package service

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"golang.org/x/mod/semver"
)

var (
	ErrNoUpdateAvailable         = infraerrors.Conflict("ALREADY_UP_TO_DATE", "no update available; current version is latest")
	ErrRollbackVersionNotAllowed = infraerrors.BadRequest("ROLLBACK_VERSION_NOT_ALLOWED", "version is not in the allowed rollback list")
)

const (
	githubRepo          = "asharca/sub2api"
	githubReleaseBranch = "release/standalone"
	updateCacheTTL      = 1200 // 20 minutes
	noStandaloneReleaseWarning = "no installable standalone stable release for " + runtime.GOOS + "/" + runtime.GOARCH

	// Security: allowed download domains for updates
	allowedDownloadHost = "github.com"
	allowedAssetHost    = "objects.githubusercontent.com"

	// Security: max download size (500MB)
	maxDownloadSize = 500 * 1024 * 1024

	// Rollback: expose at most the 3 most recent versions older than current
	maxRollbackVersions = 3
	// Fetch a few extra releases so filtering (current/newer/prerelease) still leaves enough candidates
	rollbackFetchPageSize = 15
)

// UpdateCache defines cache operations for update service
type UpdateCache interface {
	GetUpdateInfo(ctx context.Context) (string, error)
	SetUpdateInfo(ctx context.Context, data string, ttl time.Duration) error
}

// GitHubReleaseClient 获取 GitHub release 信息的接口
type GitHubReleaseClient interface {
	FetchLatestRelease(ctx context.Context, repo, branch string) (*GitHubRelease, error)
	FetchRecentReleases(ctx context.Context, repo, branch string, perPage int) ([]*GitHubRelease, error)
	DownloadFile(ctx context.Context, url, dest string, maxSize int64) error
	FetchChecksumFile(ctx context.Context, url string) ([]byte, error)
}

// UpdateService handles software updates
type UpdateService struct {
	cache          UpdateCache
	githubClient   GitHubReleaseClient
	currentVersion string
	buildType      string // "source" for manual builds, "release" for CI builds
}

// NewUpdateService creates a new UpdateService
func NewUpdateService(cache UpdateCache, githubClient GitHubReleaseClient, version, buildType string) *UpdateService {
	return &UpdateService{
		cache:          cache,
		githubClient:   githubClient,
		currentVersion: version,
		buildType:      buildType,
	}
}

// UpdateInfo contains update information
type UpdateInfo struct {
	CurrentVersion        string       `json:"current_version"`
	CurrentDisplayVersion string       `json:"current_display_version,omitempty"`
	LatestVersion         string       `json:"latest_version"`
	LatestDisplayVersion  string       `json:"latest_display_version,omitempty"`
	HasUpdate             bool         `json:"has_update"`
	ReleaseInfo           *ReleaseInfo `json:"release_info,omitempty"`
	Cached                bool         `json:"cached"`
	Warning               string       `json:"warning,omitempty"`
	BuildType             string       `json:"build_type"` // "source" or "release"
}

// ReleaseInfo contains GitHub release details
type ReleaseInfo struct {
	Name        string  `json:"name"`
	Body        string  `json:"body"`
	PublishedAt string  `json:"published_at"`
	HTMLURL     string  `json:"html_url"`
	Assets      []Asset `json:"assets,omitempty"`
}

// Asset represents a release asset
type Asset struct {
	Name        string `json:"name"`
	DownloadURL string `json:"download_url"`
	Size        int64  `json:"size"`
}

// GitHubRelease represents GitHub API response
type GitHubRelease struct {
	TagName         string        `json:"tag_name"`
	Name            string        `json:"name"`
	Body            string        `json:"body"`
	PublishedAt     string        `json:"published_at"`
	HTMLURL         string        `json:"html_url"`
	TargetCommitish string        `json:"target_commitish"`
	Draft           bool          `json:"draft"`
	Prerelease      bool          `json:"prerelease"`
	Assets          []GitHubAsset `json:"assets"`
}

// RollbackVersion describes a release version the system can roll back to
type RollbackVersion struct {
	Version     string `json:"version"` // without "v" prefix, e.g. "0.1.146"
	PublishedAt string `json:"published_at"`
	HTMLURL     string `json:"html_url"`
}

type GitHubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// CheckUpdate checks for available updates
func (s *UpdateService) CheckUpdate(ctx context.Context, force bool) (*UpdateInfo, error) {
	// Try cache first
	if !force {
		if cached, err := s.getFromCache(ctx); err == nil && cached != nil {
			return cached, nil
		}
	}

	// Fetch from GitHub
	info, err := s.refreshUpdate(ctx)
	if err != nil {
		// Return cached on error
		if cached, cacheErr := s.getFromCache(ctx); cacheErr == nil && cached != nil {
			cached.Warning = "Using cached data: " + err.Error()
			return cached, nil
		}
		return s.noReleaseInfo(err.Error()), nil
	}

	return info, nil
}

func (s *UpdateService) noReleaseInfo(warning string) *UpdateInfo {
	return &UpdateInfo{
		CurrentVersion: s.currentVersion, CurrentDisplayVersion: s.currentDisplayVersion(),
		LatestVersion: s.currentVersion, LatestDisplayVersion: s.currentDisplayVersion(),
		BuildType: s.buildType, Warning: warning,
	}
}

// PerformUpdate downloads and applies the update
// Uses atomic file replacement pattern for safe in-place updates
func (s *UpdateService) PerformUpdate(ctx context.Context) error {
	// CheckUpdate may fall back to a cached release that has since been withdrawn.
	info, err := s.refreshUpdate(ctx)
	if err != nil {
		return err
	}

	if !info.HasUpdate {
		return ErrNoUpdateAvailable
	}

	return s.applyReleaseAssets(ctx, info.LatestVersion, info.ReleaseInfo)
}

// applyReleaseAssets downloads the platform archive from the given release assets,
// verifies its checksum, and atomically swaps the running binary.
// Shared by PerformUpdate (latest) and RollbackToVersion (specific older version).
func (s *UpdateService) applyReleaseAssets(ctx context.Context, version string, release *ReleaseInfo) error {
	downloadURL, checksumURL, err := standaloneDownloadURLs(version, release.assetURL)
	if err != nil {
		return err
	}

	// Get current executable path
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return fmt.Errorf("failed to resolve symlinks: %w", err)
	}

	exeDir := filepath.Dir(exePath)

	// Create temp directory in the SAME directory as executable
	// This ensures os.Rename is atomic (same filesystem)
	tempDir, err := os.MkdirTemp(exeDir, ".sub2api-update-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Download archive
	archivePath := filepath.Join(tempDir, standaloneArchiveName(version))
	if err := s.downloadFile(ctx, downloadURL, archivePath); err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	if err := s.verifyChecksum(ctx, archivePath, checksumURL); err != nil {
		return fmt.Errorf("checksum verification failed: %w", err)
	}

	// Extract binary from archive
	newBinaryPath := filepath.Join(tempDir, "sub2api")
	if err := s.extractBinary(archivePath, newBinaryPath); err != nil {
		return fmt.Errorf("extraction failed: %w", err)
	}

	// Set executable permission before replacement
	if err := os.Chmod(newBinaryPath, 0755); err != nil {
		return fmt.Errorf("chmod failed: %w", err)
	}

	// Atomic replacement using rename pattern:
	// 1. Rename current -> backup (atomic on Unix)
	// 2. Rename new -> current (atomic on Unix, same filesystem)
	// If step 2 fails, restore backup
	backupPath := exePath + ".backup"

	// Remove old backup if exists
	_ = os.Remove(backupPath)

	// Step 1: Move current binary to backup
	if err := os.Rename(exePath, backupPath); err != nil {
		return fmt.Errorf("backup failed: %w", err)
	}

	// Step 2: Move new binary to target location (atomic, same filesystem)
	if err := os.Rename(newBinaryPath, exePath); err != nil {
		// Restore backup on failure
		if restoreErr := os.Rename(backupPath, exePath); restoreErr != nil {
			return fmt.Errorf("replace failed and restore failed: %w (restore error: %v)", err, restoreErr)
		}
		return fmt.Errorf("replace failed (restored backup): %w", err)
	}

	// Success - backup file is kept for rollback capability
	// It will be cleaned up on next successful update
	return nil
}

// Rollback restores the previous version
func (s *UpdateService) Rollback() error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return fmt.Errorf("failed to resolve symlinks: %w", err)
	}

	backupFile := exePath + ".backup"
	if _, err := os.Stat(backupFile); os.IsNotExist(err) {
		return fmt.Errorf("no backup found")
	}

	// Replace current with backup
	if err := os.Rename(backupFile, exePath); err != nil {
		return fmt.Errorf("rollback failed: %w", err)
	}

	return nil
}

// ListRollbackVersions returns up to maxRollbackVersions release versions that are
// strictly older than the current version (the current version itself is excluded),
// newest first. Draft and prerelease entries are skipped.
func (s *UpdateService) ListRollbackVersions(ctx context.Context) ([]RollbackVersion, error) {
	releases, err := s.fetchRollbackCandidates(ctx)
	if err != nil {
		return nil, err
	}

	versions := make([]RollbackVersion, 0, len(releases))
	for _, r := range releases {
		versions = append(versions, RollbackVersion{
			Version:     strings.TrimPrefix(r.TagName, "v"),
			PublishedAt: r.PublishedAt,
			HTMLURL:     r.HTMLURL,
		})
	}
	return versions, nil
}

// RollbackToVersion downloads and installs a specific older version.
// The target must be one of the versions returned by ListRollbackVersions;
// anything else (including the current version) is rejected.
func (s *UpdateService) RollbackToVersion(ctx context.Context, version string) error {
	target := strings.TrimPrefix(strings.TrimSpace(version), "v")
	if target == "" {
		return ErrRollbackVersionNotAllowed
	}

	releases, err := s.fetchRollbackCandidates(ctx)
	if err != nil {
		return err
	}

	var match *GitHubRelease
	for _, r := range releases {
		if strings.TrimPrefix(r.TagName, "v") == target {
			match = r
			break
		}
	}
	if match == nil {
		return ErrRollbackVersionNotAllowed
	}

	return s.applyReleaseAssets(ctx, target, match.releaseInfo())
}

// fetchRollbackCandidates fetches recent releases and keeps the newest
// maxRollbackVersions entries strictly older than the current version.
func (s *UpdateService) fetchRollbackCandidates(ctx context.Context) ([]*GitHubRelease, error) {
	releases, err := s.githubClient.FetchRecentReleases(ctx, githubRepo, githubReleaseBranch, rollbackFetchPageSize)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool, len(releases))
	candidates := make([]*GitHubRelease, 0, maxRollbackVersions)
	for _, r := range releases {
		if !r.IsInstallableStandaloneRelease() {
			continue
		}
		v := strings.TrimPrefix(r.TagName, "v")
		if v == "" || seen[v] {
			continue
		}
		// Only versions strictly older than current (also excludes current itself)
		if compareVersions(v, s.currentVersion) >= 0 {
			continue
		}
		seen[v] = true
		candidates = append(candidates, r)
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		return compareVersions(
			strings.TrimPrefix(candidates[i].TagName, "v"),
			strings.TrimPrefix(candidates[j].TagName, "v"),
		) > 0
	})

	if len(candidates) > maxRollbackVersions {
		candidates = candidates[:maxRollbackVersions]
	}
	return candidates, nil
}

func (s *UpdateService) refreshUpdate(ctx context.Context) (*UpdateInfo, error) {
	release, err := s.githubClient.FetchLatestRelease(ctx, githubRepo, githubReleaseBranch)
	if err != nil {
		return nil, err
	}
	var info *UpdateInfo
	if release.IsInstallableStandaloneRelease() {
		latestVersion := strings.TrimPrefix(release.TagName, "v")
		info = &UpdateInfo{
			CurrentVersion:        s.currentVersion,
			CurrentDisplayVersion: s.currentDisplayVersion(),
			LatestVersion:         latestVersion,
			LatestDisplayVersion:  latestVersion,
			HasUpdate:             compareVersions(s.currentVersion, latestVersion) < 0,
			ReleaseInfo:           release.releaseInfo(),
			BuildType:             s.buildType,
		}
	} else {
		info = s.noReleaseInfo(noStandaloneReleaseWarning)
	}
	s.saveToCache(ctx, info)
	return info, nil
}

func (s *UpdateService) downloadFile(ctx context.Context, downloadURL, dest string) error {
	return s.githubClient.DownloadFile(ctx, downloadURL, dest, maxDownloadSize)
}

func standaloneArchiveName(version string) string {
	return "sub2api_" + version + "_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
}

// The standalone channel starts at v1.0.0 and publishes only full vMAJOR.MINOR.PATCH tags.
func isStandaloneTag(tag string) bool {
	return semver.Canonical(tag) == tag && semver.Prerelease(tag) == "" && semver.Compare(tag, "v1.0.0") >= 0
}

// IsInstallableStandaloneRelease reports whether this stable release can be verified and installed locally.
func (r *GitHubRelease) IsInstallableStandaloneRelease() bool {
	if r == nil || r.Draft || r.Prerelease || !isStandaloneTag(r.TagName) {
		return false
	}
	_, _, err := standaloneDownloadURLs(strings.TrimPrefix(r.TagName, "v"), r.assetURL)
	return err == nil
}

func (r *GitHubRelease) assetURL(name string) string {
	for i := range r.Assets {
		a := &r.Assets[i]
		if a.Name == name && a.Size > 0 && a.Size <= maxDownloadSize {
			return a.BrowserDownloadURL
		}
	}
	return ""
}

func (r *ReleaseInfo) assetURL(name string) string {
	for i := range r.Assets {
		a := &r.Assets[i]
		if a.Name == name && a.Size > 0 && a.Size <= maxDownloadSize {
			return a.DownloadURL
		}
	}
	return ""
}

func standaloneDownloadURLs(version string, assetURL func(string) string) (string, string, error) {
	archive, checksum := assetURL(standaloneArchiveName(version)), assetURL("checksums.txt")
	if archive == "" || checksum == "" {
		return "", "", fmt.Errorf("missing %s archive or checksums.txt for %s/%s", version, runtime.GOOS, runtime.GOARCH)
	}
	if err := validateDownloadURL(archive); err != nil {
		return "", "", fmt.Errorf("invalid download URL: %w", err)
	}
	if err := validateDownloadURL(checksum); err != nil {
		return "", "", fmt.Errorf("invalid checksum URL: %w", err)
	}
	return archive, checksum, nil
}

func (r *GitHubRelease) releaseInfo() *ReleaseInfo {
	assets := make([]Asset, len(r.Assets))
	for i, a := range r.Assets {
		assets[i] = Asset{Name: a.Name, DownloadURL: a.BrowserDownloadURL, Size: a.Size}
	}
	return &ReleaseInfo{Name: r.Name, Body: r.Body, PublishedAt: r.PublishedAt, HTMLURL: r.HTMLURL, Assets: assets}
}

// validateDownloadURL checks if the URL is from an allowed domain
// SECURITY: This prevents SSRF and ensures downloads only come from trusted GitHub domains
func validateDownloadURL(rawURL string) error {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Must be HTTPS
	if parsedURL.Scheme != "https" {
		return fmt.Errorf("only HTTPS URLs are allowed")
	}

	// Check against allowed hosts
	host := parsedURL.Host
	// GitHub release URLs can be from github.com or objects.githubusercontent.com
	if host != allowedDownloadHost &&
		!strings.HasSuffix(host, "."+allowedDownloadHost) &&
		host != allowedAssetHost &&
		!strings.HasSuffix(host, "."+allowedAssetHost) {
		return fmt.Errorf("download from untrusted host: %s", host)
	}

	return nil
}

func (s *UpdateService) verifyChecksum(ctx context.Context, filePath, checksumURL string) error {
	// Download checksums file
	checksumData, err := s.githubClient.FetchChecksumFile(ctx, checksumURL)
	if err != nil {
		return fmt.Errorf("failed to download checksums: %w", err)
	}

	// Calculate file hash
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	actualHash := hex.EncodeToString(h.Sum(nil))

	// Find expected hash in checksums file
	fileName := filepath.Base(filePath)
	scanner := bufio.NewScanner(strings.NewReader(string(checksumData)))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) == 2 && parts[1] == fileName {
			if parts[0] == actualHash {
				return nil
			}
			return fmt.Errorf("checksum mismatch: expected %s, got %s", parts[0], actualHash)
		}
	}

	return fmt.Errorf("checksum not found for %s", fileName)
}

func (s *UpdateService) extractBinary(archivePath, destPath string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	var reader io.Reader = f

	// Handle gzip compression
	if strings.HasSuffix(archivePath, ".gz") || strings.HasSuffix(archivePath, ".tar.gz") || strings.HasSuffix(archivePath, ".tgz") {
		gzr, err := gzip.NewReader(f)
		if err != nil {
			return err
		}
		defer func() { _ = gzr.Close() }()
		reader = gzr
	}

	// Handle tar archive
	if strings.Contains(archivePath, ".tar") {
		tr := tar.NewReader(reader)
		for {
			hdr, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}

			// SECURITY: Prevent Zip Slip / Path Traversal attack
			// Only allow files with safe base names, no directory traversal
			baseName := filepath.Base(hdr.Name)

			// Check for path traversal attempts
			if strings.Contains(hdr.Name, "..") {
				return fmt.Errorf("path traversal attempt detected: %s", hdr.Name)
			}

			// Validate the entry is a regular file
			if hdr.Typeflag != tar.TypeReg {
				continue // Skip directories and special files
			}

			// Only extract the specific binary we need
			if baseName == "sub2api" || baseName == "sub2api.exe" {
				// Additional security: limit file size (max 500MB)
				const maxBinarySize = 500 * 1024 * 1024
				if hdr.Size > maxBinarySize {
					return fmt.Errorf("binary too large: %d bytes (max %d)", hdr.Size, maxBinarySize)
				}

				out, err := os.Create(destPath)
				if err != nil {
					return err
				}

				// Use LimitReader to prevent decompression bombs
				limited := io.LimitReader(tr, maxBinarySize)
				if _, err := io.Copy(out, limited); err != nil {
					_ = out.Close()
					return err
				}
				if err := out.Close(); err != nil {
					return err
				}
				return nil
			}
		}
		return fmt.Errorf("binary not found in archive")
	}

	// Direct copy for non-tar files (with size limit)
	const maxBinarySize = 500 * 1024 * 1024
	out, err := os.Create(destPath)
	if err != nil {
		return err
	}

	limited := io.LimitReader(reader, maxBinarySize)
	if _, err := io.Copy(out, limited); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func (s *UpdateService) getFromCache(ctx context.Context) (*UpdateInfo, error) {
	data, err := s.cache.GetUpdateInfo(ctx)
	if err != nil {
		return nil, err
	}

	var cached struct {
		Latest        string       `json:"latest"`
		LatestDisplay string       `json:"latest_display"`
		ReleaseInfo   *ReleaseInfo `json:"release_info"`
		Timestamp     int64        `json:"timestamp"`
	}
	if err := json.Unmarshal([]byte(data), &cached); err != nil {
		return nil, err
	}

	if time.Now().Unix()-cached.Timestamp > updateCacheTTL {
		return nil, fmt.Errorf("cache expired")
	}
	// A successful scan with no eligible release supersedes any previous hit.
	if cached.Latest == "" && cached.ReleaseInfo == nil {
		info := s.noReleaseInfo(noStandaloneReleaseWarning)
		info.Cached = true
		return info, nil
	}
	if !isStandaloneTag("v"+cached.Latest) || cached.ReleaseInfo == nil {
		return nil, fmt.Errorf("cached release is not a standalone stable version")
	}
	if _, _, err := standaloneDownloadURLs(cached.Latest, cached.ReleaseInfo.assetURL); err != nil {
		return nil, err
	}

	return &UpdateInfo{
		CurrentVersion:        s.currentVersion,
		CurrentDisplayVersion: s.currentDisplayVersion(),
		LatestVersion:         cached.Latest,
		LatestDisplayVersion:  firstNonEmpty(cached.LatestDisplay, cached.Latest),
		HasUpdate:             compareVersions(s.currentVersion, cached.Latest) < 0,
		ReleaseInfo:           cached.ReleaseInfo,
		Cached:                true,
		BuildType:             s.buildType,
	}, nil
}

func (s *UpdateService) saveToCache(ctx context.Context, info *UpdateInfo) {
	cacheData := struct {
		Latest        string       `json:"latest"`
		LatestDisplay string       `json:"latest_display"`
		ReleaseInfo   *ReleaseInfo `json:"release_info"`
		Timestamp     int64        `json:"timestamp"`
	}{
		Latest:        info.LatestVersion,
		LatestDisplay: info.LatestDisplayVersion,
		ReleaseInfo:   info.ReleaseInfo,
		Timestamp:     time.Now().Unix(),
	}
	if info.ReleaseInfo == nil {
		// Cache absence, not the version of whichever server performed the check.
		cacheData.Latest, cacheData.LatestDisplay = "", ""
	}

	data, _ := json.Marshal(cacheData)
	_ = s.cache.SetUpdateInfo(ctx, string(data), time.Duration(updateCacheTTL)*time.Second)
}

func (s *UpdateService) currentDisplayVersion() string {
	return s.currentVersion
}

// compareVersions compares two semantic versions
func compareVersions(current, latest string) int {
	return semver.Compare("v"+strings.TrimPrefix(current, "v"), "v"+strings.TrimPrefix(latest, "v"))
}
