//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

type cancellableArchiveUpload struct {
	*mockObjectStore
	started chan struct{}
}

func (s *cancellableArchiveUpload) UploadFileWithProgress(ctx context.Context, _, _, _ string, progress func(int64)) (int64, error) {
	progress(12)
	close(s.started)
	<-ctx.Done()
	return 0, ctx.Err()
}

func TestConversationArchiveProgressAndPeerCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		s, settings, store := newConversationArchiveTestService(t)
		upload := &cancellableArchiveUpload{mockObjectStore: store, started: make(chan struct{})}
		s.storeFactory = func(context.Context, *BackupS3Config) (BackupObjectStore, error) { return upload, nil }
		repo := &archiveRepoStub{ids: []int64{7}, rows: []json.RawMessage{json.RawMessage(`{"id":7}`)}}
		record, err := s.StartConversationArchive(ctx, &ConversationLogService{repo: repo}, ConversationLogFilters{}, true)
		require.NoError(t, err)
		defer func() { s.bgCancel(); s.wg.Wait() }()
		select {
		case <-upload.started:
		case <-time.After(time.Second):
			t.Fatal("archive did not start upload")
		}
		time.Sleep(6 * time.Second)
		synctest.Wait()

		peer := newTestBackupService(settings, nil, store)
		active, err := peer.GetConversationArchive(ctx, record.ID)
		require.NoError(t, err)
		require.Equal(t, "upload", active.Phase)
		require.True(t, active.CanCancel)
		require.EqualValues(t, 1, active.ProgressRows)
		require.EqualValues(t, 12, active.UploadedBytes)
		require.Greater(t, active.UploadTotalBytes, active.UploadedBytes)
		require.Zero(t, active.SizeBytes)

		cancelled, err := peer.CancelConversationArchive(ctx, record.ID)
		require.NoError(t, err)
		require.Equal(t, "cancelling", cancelled.Status)
		require.ErrorIs(t, peer.DeleteConversationArchive(ctx, record.ID), ErrBackupInProgress)
		s.wg.Wait()
		cancelled, err = peer.GetConversationArchive(ctx, record.ID)
		require.NoError(t, err)
		require.Equal(t, "cancelled", cancelled.Status)
		require.False(t, cancelled.CanCancel)
		require.Empty(t, repo.deleted)
		require.Empty(t, store.objects)
	})
}
