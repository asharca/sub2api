//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type updateServiceCacheStub struct {
	data string
}

func (s *updateServiceCacheStub) GetUpdateInfo(context.Context) (string, error) {
	if s.data == "" {
		return "", errors.New("cache miss")
	}
	return s.data, nil
}

func (s *updateServiceCacheStub) SetUpdateInfo(_ context.Context, data string, _ time.Duration) error {
	s.data = data
	return nil
}

type updateServiceGitHubClientStub struct {
	release        *GitHubRelease
	recentReleases []*GitHubRelease
	recentErr      error
	latestErr      error
	repo, branch   string
	downloadURL    string
}

func (s *updateServiceGitHubClientStub) FetchLatestRelease(_ context.Context, repo, branch string) (*GitHubRelease, error) {
	s.repo, s.branch = repo, branch
	return s.release, s.latestErr
}

func (s *updateServiceGitHubClientStub) FetchRecentReleases(_ context.Context, repo, branch string, _ int) ([]*GitHubRelease, error) {
	s.repo, s.branch = repo, branch
	return s.recentReleases, s.recentErr
}

func (s *updateServiceGitHubClientStub) DownloadFile(_ context.Context, url, _ string, _ int64) error {
	s.downloadURL = url
	return errors.New("download interrupted")
}

func (s *updateServiceGitHubClientStub) FetchChecksumFile(context.Context, string) ([]byte, error) {
	panic("FetchChecksumFile should not be called when no update is available")
}

func standaloneTestRelease(tag string) *GitHubRelease {
	archive := fmt.Sprintf("sub2api_%s_%s_%s.tar.gz", strings.TrimPrefix(tag, "v"), runtime.GOOS, runtime.GOARCH)
	base := "https://github.com/asharca/sub2api/releases/download/" + tag + "/"
	return &GitHubRelease{TagName: tag, Name: tag, TargetCommitish: "release/standalone", Assets: []GitHubAsset{
		{Name: archive, BrowserDownloadURL: base + archive, Size: 2048},
		{Name: "checksums.txt", BrowserDownloadURL: base + "checksums.txt", Size: 100},
	}}
}

func TestUpdateServiceCheckUpdateStandalone(t *testing.T) {
	for _, tt := range []struct {
		current, tag string
		wantUpdate   bool
	}{
		{"0.1.144-asharca.2", "v1.0.0", true},
		{"v1.0.0-rc.1", "v1.0.0", true},
		{"1.0.9", "v1.0.10", true},
		{"v1.0.0", "v1.0.0", false},
		{"2.0.0", "v1.9.9", false},
	} {
		t.Run(tt.current+"_to_"+tt.tag, func(t *testing.T) {
			client := &updateServiceGitHubClientStub{release: standaloneTestRelease(tt.tag)}
			svc := NewUpdateService(&updateServiceCacheStub{}, client, tt.current, "release")
			for _, force := range []bool{true, false} {
				info, err := svc.CheckUpdate(context.Background(), force)
				require.NoError(t, err)
				require.Empty(t, info.Warning)
				require.Equal(t, !force, info.Cached)
				require.Equal(t, tt.current, info.CurrentDisplayVersion)
				require.Equal(t, strings.TrimPrefix(tt.tag, "v"), info.LatestVersion)
				require.Equal(t, info.LatestVersion, info.LatestDisplayVersion)
				require.Equal(t, tt.wantUpdate, info.HasUpdate)
			}
			require.Equal(t, "asharca/sub2api", client.repo)
			require.Equal(t, "release/standalone", client.branch)
			if !tt.wantUpdate {
				require.ErrorIs(t, svc.PerformUpdate(context.Background()), ErrNoUpdateAvailable)
				require.Empty(t, client.downloadURL)
			} else {
				require.ErrorContains(t, svc.PerformUpdate(context.Background()), "download interrupted")
				require.Equal(t, client.release.Assets[0].BrowserDownloadURL, client.downloadURL)
			}
		})
	}
}

func TestUpdateServiceSkipsIneligibleReleases(t *testing.T) {
	draft, prerelease := standaloneTestRelease("v2.0.0"), standaloneTestRelease("v2.0.0")
	draft.Draft, prerelease.Prerelease = true, true
	wrongPlatform, badURL := standaloneTestRelease("v2.0.0"), standaloneTestRelease("v2.0.0")
	wrongPlatform.Assets[0].Name += ".sig"
	badURL.Assets[0].BrowserDownloadURL = "https://example.com/file.tar.gz"
	noArchive, noChecksum := standaloneTestRelease("v2.0.0"), standaloneTestRelease("v2.0.0")
	noArchive.Assets = noArchive.Assets[1:]
	noChecksum.Assets = noChecksum.Assets[:1]
	empty, oversized := standaloneTestRelease("v2.0.0"), standaloneTestRelease("v2.0.0")
	empty.Assets[0].Size, oversized.Assets[0].Size = 0, maxDownloadSize+1
	for name, r := range map[string]*GitHubRelease{
		"nil": nil, "legacy": standaloneTestRelease("v0.1.144-asharca.2"), "before baseline": standaloneTestRelease("v0.99.99"),
		"unflagged rc": standaloneTestRelease("v1.2.0-rc.1"), "short tag": standaloneTestRelease("v2.0"),
		"metadata": standaloneTestRelease("v2.0.0+test"), "no prefix": standaloneTestRelease("2.0.0"),
		"draft": draft, "prerelease": prerelease, "unusable archive": wrongPlatform,
		"untrusted URL": badURL, "missing archive": noArchive, "missing checksum": noChecksum,
		"empty archive": empty, "oversized archive": oversized,
	} {
		t.Run(name, func(t *testing.T) {
			client := &updateServiceGitHubClientStub{release: r, recentReleases: []*GitHubRelease{r}}
			svc := NewUpdateService(&updateServiceCacheStub{}, client, "0.1.1", "release")
			info, err := svc.CheckUpdate(context.Background(), true)
			require.NoError(t, err)
			require.False(t, info.HasUpdate)
			require.NotEmpty(t, info.Warning)
			require.Nil(t, info.ReleaseInfo)
			require.ErrorIs(t, svc.PerformUpdate(context.Background()), ErrNoUpdateAvailable)
			// Also ineligible for rollback even when strictly older than current.
			svc.currentVersion = "3.0.0"
			versions, err := svc.ListRollbackVersions(context.Background())
			require.NoError(t, err)
			require.Empty(t, versions)
			require.Empty(t, client.downloadURL)
		})
	}
}

func TestUpdateServiceDoesNotInstallWithdrawnCachedRelease(t *testing.T) {
	client := &updateServiceGitHubClientStub{release: standaloneTestRelease("v1.1.0")}
	svc := NewUpdateService(&updateServiceCacheStub{}, client, "1.0.0", "release")
	info, err := svc.CheckUpdate(context.Background(), true)
	require.NoError(t, err)
	require.True(t, info.HasUpdate)
	client.release = nil
	info, err = svc.CheckUpdate(context.Background(), true)
	require.NoError(t, err)
	require.False(t, info.Cached)
	require.False(t, info.HasUpdate)
	cached, err := svc.CheckUpdate(context.Background(), false)
	require.NoError(t, err)
	require.True(t, cached.Cached)
	require.False(t, cached.HasUpdate)
	require.NotEmpty(t, info.Warning)
	require.ErrorIs(t, svc.PerformUpdate(context.Background()), ErrNoUpdateAvailable)
	require.Empty(t, client.downloadURL)
}

func TestUpdateServiceCheckUpdateAPIFailure(t *testing.T) {
	client := &updateServiceGitHubClientStub{release: standaloneTestRelease("v1.1.0")}
	svc := NewUpdateService(&updateServiceCacheStub{}, client, "1.0.0", "release")
	ctx := context.Background()
	_, err := svc.CheckUpdate(ctx, true)
	require.NoError(t, err)
	client.latestErr = errors.New("GitHub unavailable")
	info, err := svc.CheckUpdate(ctx, true)
	require.NoError(t, err)
	require.True(t, info.Cached)
	require.Contains(t, info.Warning, client.latestErr.Error())
	require.ErrorIs(t, svc.PerformUpdate(ctx), client.latestErr)
	require.Empty(t, client.downloadURL)
}

func TestUpdateServiceRollbackStandalone(t *testing.T) {
	client := &updateServiceGitHubClientStub{}
	for _, tag := range []string{"v1.0.1", "v1.0.11", "v1.0.9", "v1.0.2", "v1.0.9", "v1.0.12", "v1.0.10"} {
		client.recentReleases = append(client.recentReleases, standaloneTestRelease(tag))
	}
	svc := NewUpdateService(&updateServiceCacheStub{}, client, "1.0.11", "release")
	ctx := context.Background()
	versions, err := svc.ListRollbackVersions(ctx)
	require.NoError(t, err)
	require.Equal(t, "release/standalone", client.branch)
	require.Equal(t, []RollbackVersion{{Version: "1.0.10"}, {Version: "1.0.9"}, {Version: "1.0.2"}}, versions)
	for _, target := range []string{"", "v1.0.11", "1.0.12", "1.0.1", "9.9.9", "0.1.144-asharca.2"} {
		require.ErrorIs(t, svc.RollbackToVersion(ctx, target), ErrRollbackVersionNotAllowed)
	}
	require.Empty(t, client.downloadURL)
	require.ErrorContains(t, svc.RollbackToVersion(ctx, "v1.0.9"), "download interrupted")
	require.Equal(t, standaloneTestRelease("v1.0.9").Assets[0].BrowserDownloadURL, client.downloadURL)
	client.recentErr = errors.New("github unavailable")
	_, err = svc.ListRollbackVersions(ctx)
	require.ErrorIs(t, err, client.recentErr)
}
