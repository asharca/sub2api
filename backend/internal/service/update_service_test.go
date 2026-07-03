//go:build unit

package service

import (
	"context"
	"errors"
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
	release *GitHubRelease
}

func (s *updateServiceGitHubClientStub) FetchLatestRelease(context.Context, string, string) (*GitHubRelease, error) {
	return s.release, nil
}

func (s *updateServiceGitHubClientStub) DownloadFile(context.Context, string, string, int64) error {
	panic("DownloadFile should not be called when no update is available")
}

func (s *updateServiceGitHubClientStub) FetchChecksumFile(context.Context, string) ([]byte, error) {
	panic("FetchChecksumFile should not be called when no update is available")
}

func TestUpdateServicePerformUpdateNoUpdateReturnsSentinel(t *testing.T) {
	svc := NewUpdateService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{
			release: &GitHubRelease{
				TagName: "v0.1.132",
				Name:    "v0.1.132",
			},
		},
		"0.1.132",
		"release",
	)

	err := svc.PerformUpdate(context.Background())

	require.Error(t, err)
	require.True(t, errors.Is(err, ErrNoUpdateAvailable))
	require.ErrorIs(t, err, ErrNoUpdateAvailable)
}

func TestUpdateServiceCheckUpdateUsesAsharcaTagRelease(t *testing.T) {
	cache := &updateServiceCacheStub{}
	svc := NewUpdateService(
		cache,
		&updateServiceGitHubClientStub{
			release: &GitHubRelease{
				TagName: "v0.1.144-asharca.1",
				Name:    "v0.1.144-asharca.1",
			},
		},
		"0.1.144",
		"release",
	)

	info, err := svc.CheckUpdate(context.Background(), true)

	require.NoError(t, err)
	require.Equal(t, "0.1.144", info.CurrentVersion)
	require.Equal(t, "0.1.144", info.CurrentDisplayVersion)
	require.Equal(t, "0.1.144-asharca.1", info.LatestVersion)
	require.Equal(t, "0.1.144-asharca.1", info.LatestDisplayVersion)
	require.True(t, info.HasUpdate)
}

func TestUpdateServiceCheckUpdateTreatsSameAsharcaTagAsCurrent(t *testing.T) {
	svc := NewUpdateService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{
			release: &GitHubRelease{
				TagName: "v0.1.144-asharca.1",
				Name:    "v0.1.144-asharca.1",
			},
		},
		"0.1.144-asharca.1",
		"release",
	)

	info, err := svc.CheckUpdate(context.Background(), true)

	require.NoError(t, err)
	require.False(t, info.HasUpdate)
}

func TestUpdateServiceCheckUpdateDetectsNextAsharcaRevision(t *testing.T) {
	cache := &updateServiceCacheStub{}
	svc := NewUpdateService(
		cache,
		&updateServiceGitHubClientStub{
			release: &GitHubRelease{
				TagName: "v0.1.144-asharca.2",
				Name:    "v0.1.144-asharca.2",
			},
		},
		"0.1.144-asharca.1",
		"release",
	)

	_, err := svc.CheckUpdate(context.Background(), true)
	require.NoError(t, err)

	cached, err := svc.CheckUpdate(context.Background(), false)

	require.NoError(t, err)
	require.True(t, cached.Cached)
	require.True(t, cached.HasUpdate)
	require.Equal(t, "0.1.144-asharca.2", cached.LatestDisplayVersion)
}
