//go:build unit

package service

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

type archiveRepoStub struct {
	ConversationLogRepository
	rows         []json.RawMessage
	ids          []int64
	deleted      []int64
	deleteErr    error
	streamErr    error
	beforeDelete func()
}

func (r *archiveRepoStub) StreamConversationArchive(_ context.Context, _ ConversationLogFilters, emit func(int64, json.RawMessage) error) error {
	for i := range r.rows {
		if err := emit(r.ids[i], r.rows[i]); err != nil {
			return err
		}
	}
	return r.streamErr
}

func (r *archiveRepoStub) DeleteConversationArchiveIDs(_ context.Context, source io.Reader) (int64, error) {
	if r.beforeDelete != nil {
		r.beforeDelete()
	}
	if r.deleteErr != nil {
		return 0, r.deleteErr
	}
	for {
		var id int64
		err := binary.Read(source, binary.LittleEndian, &id)
		if err == io.EOF {
			return int64(len(r.deleted)), nil
		}
		if err != nil {
			return 0, err
		}
		r.deleted = append(r.deleted, id)
	}
}

type archiveMetadataFailureStore struct{ *mockObjectStore }

func (s *archiveMetadataFailureStore) Upload(context.Context, string, io.Reader, string) (int64, error) {
	return 0, errors.New("metadata unavailable")
}

func newConversationArchiveTestService(t *testing.T) (*BackupService, *mockSettingRepo, *mockObjectStore) {
	t.Helper()
	settings := newMockSettingRepo()
	store := newMockObjectStore()
	s := newTestBackupService(settings, nil, store)
	_, err := s.UpdateS3Config(context.Background(), BackupS3Config{Bucket: "archives", AccessKeyID: "key", SecretAccessKey: "secret"})
	require.NoError(t, err)
	return s, settings, store
}

func TestConversationArchiveDurableBeforeExactIDCleanup(t *testing.T) {
	s, settings, store := newConversationArchiveTestService(t)
	for _, cleanup := range []bool{false, true} {
		t.Run(map[bool]string{false: "archive_only", true: "archive_then_delete"}[cleanup], func(t *testing.T) {
			repo := &archiveRepoStub{
				ids:  []int64{3, 18},
				rows: []json.RawMessage{json.RawMessage(`{"id":3,"request_body":"full request","response_body":"full response"}`), json.RawMessage(`{"id":18,"request_body":"second"}`)},
			}
			record := &ConversationArchiveRecord{ID: "id", Status: "running", S3Key: "archive.jsonl.gz", ManifestKey: "archive.metadata.json", StartedAt: time.Now().Format(time.RFC3339), DeleteAfterBackup: cleanup}
			require.NoError(t, s.saveConversationArchive(context.Background(), record, true))
			repo.beforeDelete = func() {
				store.mu.Lock()
				require.NotEmpty(t, store.objects[record.S3Key])
				require.NotEmpty(t, store.objects[record.ManifestKey])
				store.mu.Unlock()
				raw, err := settings.GetValue(context.Background(), settingKeyConversationArchives)
				require.NoError(t, err)
				require.Contains(t, raw, record.SHA256)
			}
			s.executeConversationArchive(repo, store, ConversationLogFilters{}, record)
			require.Equal(t, "completed", record.Status)
			require.EqualValues(t, 2, record.RecordCount)
			if cleanup {
				require.Equal(t, []int64{3, 18}, repo.deleted)
				require.EqualValues(t, 2, record.DeletedCount)
			} else {
				require.Empty(t, repo.deleted)
			}
			store.mu.Lock()
			data := append([]byte(nil), store.objects[record.S3Key]...)
			store.mu.Unlock()
			sum := sha256.Sum256(data)
			require.Equal(t, hex.EncodeToString(sum[:]), record.SHA256)
			require.EqualValues(t, len(data), record.SizeBytes)
			reader, err := gzip.NewReader(bytes.NewReader(data))
			require.NoError(t, err)
			decoded, err := io.ReadAll(reader)
			require.NoError(t, err)
			require.NoError(t, reader.Close())
			require.Equal(t, string(repo.rows[0])+"\n"+string(repo.rows[1])+"\n", string(decoded))
		})
	}
}

func TestConversationArchiveFailuresNeverDeleteBeforeDurability(t *testing.T) {
	for _, failure := range []string{"snapshot", "upload", "metadata", "cleanup"} {
		t.Run(failure, func(t *testing.T) {
			s, _, store := newConversationArchiveTestService(t)
			repo := &archiveRepoStub{ids: []int64{5}, rows: []json.RawMessage{json.RawMessage(`{"id":5,"response_body":"retained"}`)}}
			var objectStore BackupObjectStore = store
			switch failure {
			case "snapshot":
				repo.streamErr = errors.New("snapshot unavailable")
			case "upload":
				store.failUploadFileAt = 1
			case "metadata":
				objectStore = &archiveMetadataFailureStore{store}
			case "cleanup":
				repo.deleteErr = errors.New("delete rejected")
			}
			record := &ConversationArchiveRecord{ID: "failure", Status: "running", S3Key: "archive.jsonl.gz", ManifestKey: "archive.metadata.json", StartedAt: time.Now().Format(time.RFC3339), DeleteAfterBackup: true}
			require.NoError(t, s.saveConversationArchive(context.Background(), record, true))
			s.executeConversationArchive(repo, objectStore, ConversationLogFilters{}, record)
			require.Empty(t, repo.deleted)
			require.NotEmpty(t, record.ErrorMessage)
			if failure == "cleanup" {
				require.Equal(t, "cleanup_failed", record.Status)
				require.NotEmpty(t, store.objects[record.S3Key])
				require.NotEmpty(t, store.objects[record.ManifestKey])
			} else {
				require.Equal(t, "failed", record.Status)
			}
		})
	}
}

func TestConversationArchiveInterruptedUploadedCleanupRetainsDownload(t *testing.T) {
	s, _, _ := newConversationArchiveTestService(t)
	record := &ConversationArchiveRecord{ID: "interrupted", Status: "running", SHA256: "durable-checksum", SizeBytes: 42, S3Key: "archive.jsonl.gz", DeleteAfterBackup: true, StartedAt: time.Now().Add(-time.Hour).Format(time.RFC3339)}
	require.NoError(t, s.saveConversationArchive(context.Background(), record, true))
	recovered, err := s.GetConversationArchive(context.Background(), record.ID)
	require.NoError(t, err)
	require.Equal(t, "cleanup_failed", recovered.Status)
	require.Equal(t, record.SHA256, recovered.SHA256)
	url, err := s.GetConversationArchiveDownloadURL(context.Background(), record.ID)
	require.NoError(t, err)
	require.NotEmpty(t, url)
}

type archiveCatalogFailureRepo struct {
	SettingRepository
	failNext bool
}

func (r *archiveCatalogFailureRepo) Set(ctx context.Context, key, value string) error {
	if key == settingKeyConversationArchives && r.failNext {
		r.failNext = false
		return errors.New("catalog persistence unavailable")
	}
	return r.SettingRepository.Set(ctx, key, value)
}

func TestConversationArchiveCatalogFailureNeverDeletesSource(t *testing.T) {
	s, settings, store := newConversationArchiveTestService(t)
	record := &ConversationArchiveRecord{ID: "catalog-failure", Status: "running", S3Key: "archive.jsonl.gz", ManifestKey: "archive.metadata.json", StartedAt: time.Now().Format(time.RFC3339), DeleteAfterBackup: true}
	require.NoError(t, s.saveConversationArchive(context.Background(), record, true))
	s.settingRepo = &archiveCatalogFailureRepo{SettingRepository: settings, failNext: true}
	repo := &archiveRepoStub{ids: []int64{7}, rows: []json.RawMessage{json.RawMessage(`{"id":7}`)}}
	s.executeConversationArchive(repo, store, ConversationLogFilters{}, record)
	require.Equal(t, "failed", record.Status)
	require.Empty(t, repo.deleted)
	require.NotEmpty(t, store.objects[record.S3Key])
	require.Contains(t, record.ErrorMessage, "persist uploaded")
}

type blockingArchiveRepository struct {
	archiveRepoStub
	started chan ConversationLogFilters
	release chan struct{}
}

func (r *blockingArchiveRepository) StreamConversationArchive(ctx context.Context, filters ConversationLogFilters, emit func(int64, json.RawMessage) error) error {
	r.started <- filters
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-r.release:
		return r.archiveRepoStub.StreamConversationArchive(ctx, filters, emit)
	}
}

func TestConversationArchiveStartIsAsyncAndForwardsExactFilters(t *testing.T) {
	s, _, _ := newConversationArchiveTestService(t)
	repo := &blockingArchiveRepository{started: make(chan ConversationLogFilters, 1), release: make(chan struct{})}
	filters := ConversationLogFilters{UserID: 81, RequestID: "exact-request", Model: "model"}
	record, err := s.StartConversationArchive(context.Background(), &ConversationLogService{repo: repo}, filters, false)
	require.NoError(t, err)
	require.Equal(t, "running", record.Status)
	select {
	case got := <-repo.started:
		require.Equal(t, filters, got)
	case <-time.After(5 * time.Second):
		t.Fatal("archive worker did not start")
	}
	_, err = s.StartConversationArchive(context.Background(), &ConversationLogService{repo: repo}, filters, false)
	require.ErrorIs(t, err, ErrBackupInProgress)
	close(repo.release)
	s.wg.Wait()
	completed, err := s.GetConversationArchive(context.Background(), record.ID)
	require.NoError(t, err)
	require.Equal(t, "completed", completed.Status)
	require.Zero(t, completed.RecordCount)
}

func TestConversationArchiveKeepsRunningWithoutTotalTimeLimit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, settings, store := newConversationArchiveTestService(t)
		repo := &blockingArchiveRepository{
			archiveRepoStub: archiveRepoStub{ids: []int64{7}, rows: []json.RawMessage{json.RawMessage(`{"id":7,"request_body":"long-running archive"}`)}},
			started:         make(chan ConversationLogFilters, 1), release: make(chan struct{}),
		}
		requestCtx, disconnect := context.WithCancel(context.Background())
		record, err := s.StartConversationArchive(requestCtx, &ConversationLogService{repo: repo}, ConversationLogFilters{}, false)
		require.NoError(t, err)
		defer func() {
			close(repo.release)
			s.wg.Wait()
			completed, err := s.GetConversationArchive(context.Background(), record.ID)
			require.NoError(t, err)
			require.Equal(t, "completed", completed.Status)
			require.EqualValues(t, 1, completed.RecordCount)
		}()
		<-repo.started
		disconnect()
		time.Sleep(2 * time.Hour)
		synctest.Wait()

		// A different instance must also see the old but still live job as running.
		peer := newTestBackupService(settings, nil, store)
		live, err := peer.GetConversationArchive(context.Background(), record.ID)
		require.NoError(t, err)
		require.Equal(t, "running", live.Status)
		_, err = peer.StartConversationArchive(context.Background(), &ConversationLogService{repo: repo}, ConversationLogFilters{}, false)
		require.ErrorIs(t, err, ErrBackupInProgress)
	})
}
