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

func TestConversationArchiveRetryRebuildsExactPersistedSnapshot(t *testing.T) {
	s, _, store := newConversationArchiveTestService(t)
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(48 * time.Hour)
	requestType, stream, mismatch := int16(2), false, true
	filters := ConversationLogFilters{
		Search: "search", UserID: 9007199254740993, APIKeyID: 9, AccountID: 7, GroupID: 4,
		Platform: "openai", Model: "model", RequestID: "request", ResponseID: "response",
		RequestType: &requestType, Stream: &stream, UpstreamModelMismatch: &mismatch,
		StartTime: &start, EndTime: &end,
	}
	original := &ConversationArchiveRecord{
		ID: "original", Status: "failed", Filters: conversationArchiveFilters(filters),
		DeleteAfterBackup: true, RecordCount: 99, ErrorMessage: "EntityTooLarge: configured maximum object size",
	}
	require.NoError(t, s.saveConversationArchive(context.Background(), original, true))
	before, err := s.GetConversationArchive(context.Background(), original.ID)
	require.NoError(t, err)
	repo := &blockingArchiveRepository{
		archiveRepoStub: archiveRepoStub{ids: []int64{101, 102}, rows: []json.RawMessage{json.RawMessage(`{"id":101}`), json.RawMessage(`{"id":102}`)}},
		started:         make(chan ConversationLogFilters, 1), release: make(chan struct{}),
	}
	retried, err := s.RetryConversationArchive(context.Background(), &ConversationLogService{repo: repo}, original.ID)
	require.NoError(t, err)
	released := false
	defer func() {
		if !released {
			close(repo.release)
		}
		s.wg.Wait()
	}()
	require.NotEqual(t, original.ID, retried.ID)
	require.Equal(t, "running", retried.Status)
	require.True(t, retried.DeleteAfterBackup)
	require.Zero(t, retried.RecordCount)
	select {
	case got := <-repo.started:
		require.Equal(t, filters, got)
	case <-time.After(5 * time.Second):
		t.Fatal("retry worker did not start")
	}
	// A peer cannot delete/retry the newly running job using the catalog alone.
	peer := newTestBackupService(s.settingRepo.(*mockSettingRepo), nil, store)
	require.ErrorIs(t, peer.DeleteConversationArchive(context.Background(), retried.ID), ErrBackupInProgress)
	_, err = peer.RetryConversationArchive(context.Background(), &ConversationLogService{repo: repo}, retried.ID)
	require.Error(t, err)
	after, err := s.GetConversationArchive(context.Background(), original.ID)
	require.NoError(t, err)
	require.Equal(t, before, after)
	// Complete the fresh snapshot before inspecting its exact-ID cleanup.
	close(repo.release)
	released = true
	s.wg.Wait()
	completed, err := s.GetConversationArchive(context.Background(), retried.ID)
	require.NoError(t, err)
	require.Equal(t, "completed", completed.Status)
	require.EqualValues(t, 2, completed.RecordCount)
	require.Equal(t, []int64{101, 102}, repo.deleted)
}

func TestConversationArchiveRetryRejectsNonFailedMissingAndInvalidFilters(t *testing.T) {
	for _, status := range []string{"completed", "running", "cleanup_failed"} {
		t.Run(status, func(t *testing.T) {
			s, _, store := newConversationArchiveTestService(t)
			record := &ConversationArchiveRecord{ID: "existing", Status: status, StartedAt: time.Now().Format(time.RFC3339)}
			require.NoError(t, s.saveConversationArchive(context.Background(), record, true))
			_, err := s.RetryConversationArchive(context.Background(), &ConversationLogService{repo: &archiveRepoStub{}}, record.ID)
			require.Error(t, err)
			records, err := s.loadConversationArchives(context.Background())
			require.NoError(t, err)
			require.Len(t, records, 1)
			require.Empty(t, store.objects)
		})
	}
	s, _, _ := newConversationArchiveTestService(t)
	logs := &ConversationLogService{repo: &archiveRepoStub{}}
	_, err := s.RetryConversationArchive(context.Background(), logs, "missing")
	require.ErrorIs(t, err, ErrBackupNotFound)
	for _, filters := range []map[string]any{nil, {"start_time": "invalid"}, {"user_id": "invalid"}} {
		record := &ConversationArchiveRecord{ID: "invalid", Status: "failed", Filters: filters}
		require.NoError(t, s.saveConversationArchive(context.Background(), record, true))
		_, err := s.RetryConversationArchive(context.Background(), logs, record.ID)
		require.Error(t, err)
	}
}

func TestConversationArchiveDeleteNonRunningRemovesOnlyArchiveObjects(t *testing.T) {
	for _, status := range []string{"completed", "failed", "cleanup_failed"} {
		t.Run(status, func(t *testing.T) {
			s, _, store := newConversationArchiveTestService(t)
			record := &ConversationArchiveRecord{ID: "delete", Status: status, S3Key: "data", ManifestKey: "metadata", SHA256: "checksum"}
			require.NoError(t, s.saveConversationArchive(context.Background(), record, true))
			require.NoError(t, s.saveConversationArchive(context.Background(), &ConversationArchiveRecord{ID: "keep", Status: "completed"}, true))
			store.objects[record.S3Key], store.objects[record.ManifestKey] = []byte("gzip"), []byte("manifest")
			store.objects["unrelated"] = []byte("keep")
			require.NoError(t, s.DeleteConversationArchive(context.Background(), record.ID))
			require.Equal(t, []string{record.S3Key, record.ManifestKey}, store.deletedKeys)
			require.Equal(t, map[string][]byte{"unrelated": []byte("keep")}, store.objects)
			records, err := s.ListConversationArchives(context.Background())
			require.NoError(t, err)
			require.Len(t, records, 1)
			require.Equal(t, "keep", records[0].ID)
			require.ErrorIs(t, s.DeleteConversationArchive(context.Background(), record.ID), ErrBackupNotFound)
		})
	}
}

func TestConversationArchiveDeleteFailureRetainsCatalogForRetry(t *testing.T) {
	for _, key := range []string{"data", "metadata"} {
		t.Run(key, func(t *testing.T) {
			s, settings, store := newConversationArchiveTestService(t)
			record := &ConversationArchiveRecord{ID: "retained", Status: "failed", S3Key: "data", ManifestKey: "metadata"}
			require.NoError(t, s.saveConversationArchive(context.Background(), record, true))
			before, err := settings.GetValue(context.Background(), settingKeyConversationArchives)
			require.NoError(t, err)
			store.failDeleteKeys[key] = errors.New("object delete unavailable")
			require.ErrorContains(t, s.DeleteConversationArchive(context.Background(), record.ID), "object delete unavailable")
			after, err := settings.GetValue(context.Background(), settingKeyConversationArchives)
			require.NoError(t, err)
			require.Equal(t, before, after)
			delete(store.failDeleteKeys, key)
			require.NoError(t, s.DeleteConversationArchive(context.Background(), record.ID))
		})
	}
}

func TestConversationArchiveDeleteWithoutS3ProtectsKnownUploads(t *testing.T) {
	for _, tc := range []struct {
		name    string
		record  ConversationArchiveRecord
		allowed bool
	}{
		{name: "historical failed upload", record: ConversationArchiveRecord{Status: "failed", ErrorMessage: "EntityTooLarge"}, allowed: true},
		{name: "checksum", record: ConversationArchiveRecord{Status: "failed", SHA256: "uploaded"}},
		{name: "size", record: ConversationArchiveRecord{Status: "failed", SizeBytes: 10}},
		{name: "legacy size mismatch", record: ConversationArchiveRecord{Status: "failed", ErrorMessage: "conversation archive upload size mismatch: expected 10, got 9"}},
		{name: "completed", record: ConversationArchiveRecord{Status: "completed"}},
		{name: "cleanup failed", record: ConversationArchiveRecord{Status: "cleanup_failed"}},
		{name: "running", record: ConversationArchiveRecord{Status: "running"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestBackupService(newMockSettingRepo(), nil, newMockObjectStore())
			tc.record.ID, tc.record.S3Key, tc.record.ManifestKey = "id", "data", "metadata"
			require.NoError(t, s.saveConversationArchive(context.Background(), &tc.record, true))
			err := s.DeleteConversationArchive(context.Background(), tc.record.ID)
			if tc.allowed {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				records, err := s.loadConversationArchives(context.Background())
				require.NoError(t, err)
				require.Len(t, records, 1)
			}
		})
	}
}

type archiveUploadErrorStore struct {
	*mockObjectStore
	err error
}

func (s *archiveUploadErrorStore) UploadFile(context.Context, string, string, string) (int64, error) {
	return 0, s.err
}

func TestConversationArchiveEntityTooLargePreservesOriginalErrorAndSource(t *testing.T) {
	s, _, store := newConversationArchiveTestService(t)
	cause := errors.New("EntityTooLarge: configured maximum object size")
	repo := &archiveRepoStub{ids: []int64{1}, rows: []json.RawMessage{json.RawMessage(`{"id":1}`)}}
	record := &ConversationArchiveRecord{ID: "large", Status: "running", S3Key: "data", ManifestKey: "metadata", DeleteAfterBackup: true}
	require.NoError(t, s.saveConversationArchive(context.Background(), record, true))
	err := s.runConversationArchive(context.Background(), repo, &archiveUploadErrorStore{store, cause}, ConversationLogFilters{}, record)
	require.ErrorIs(t, err, cause)
	require.ErrorContains(t, err, "EntityTooLarge")
	require.Empty(t, repo.deleted)
	s.executeConversationArchive(repo, &archiveUploadErrorStore{store, cause}, ConversationLogFilters{}, record)
	require.Equal(t, "failed", record.Status)
	require.Contains(t, record.ErrorMessage, cause.Error())
	require.Empty(t, repo.deleted)
}

type blockingArchiveDeleteStore struct {
	*mockObjectStore
	started chan struct{}
	release chan struct{}
}

func (s *blockingArchiveDeleteStore) Delete(ctx context.Context, key string) error {
	if key == "data" {
		close(s.started)
		<-s.release
	}
	return s.mockObjectStore.Delete(ctx, key)
}

func TestConversationArchiveDeleteSerializesConcurrentCatalogWrites(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, _, store := newConversationArchiveTestService(t)
		s.storeFactory = func(context.Context, *BackupS3Config) (BackupObjectStore, error) {
			return &blockingArchiveDeleteStore{store, make(chan struct{}), make(chan struct{})}, nil
		}
		cfg, err := s.loadS3Config(context.Background())
		require.NoError(t, err)
		objectStore, err := s.getOrCreateStore(context.Background(), cfg)
		require.NoError(t, err)
		blocking := objectStore.(*blockingArchiveDeleteStore)
		record := &ConversationArchiveRecord{ID: "delete", Status: "completed", S3Key: "data", ManifestKey: "metadata"}
		require.NoError(t, s.saveConversationArchive(context.Background(), record, true))
		deleted, saved := make(chan error, 1), make(chan error, 1)
		go func() { deleted <- s.DeleteConversationArchive(context.Background(), record.ID) }()
		<-blocking.started
		go func() {
			saved <- s.saveConversationArchive(context.Background(), &ConversationArchiveRecord{ID: "concurrent", Status: "completed"}, true)
		}()
		close(blocking.release)
		require.NoError(t, <-deleted)
		require.NoError(t, <-saved)
		records, err := s.ListConversationArchives(context.Background())
		require.NoError(t, err)
		require.Len(t, records, 1)
		require.Equal(t, "concurrent", records[0].ID)
	})
}

func TestConversationArchiveDeleteCatalogFailureRetainsRetryableRecord(t *testing.T) {
	s, settings, store := newConversationArchiveTestService(t)
	record := &ConversationArchiveRecord{ID: "retry-delete", Status: "completed", S3Key: "data", ManifestKey: "metadata"}
	require.NoError(t, s.saveConversationArchive(context.Background(), record, true))
	s.settingRepo = &archiveCatalogFailureRepo{SettingRepository: settings, failNext: true}
	require.ErrorContains(t, s.DeleteConversationArchive(context.Background(), record.ID), "catalog persistence unavailable")
	retained, err := s.GetConversationArchive(context.Background(), record.ID)
	require.NoError(t, err)
	require.Equal(t, record.ID, retained.ID)
	require.Equal(t, []string{"data", "metadata"}, store.deletedKeys)
	require.NoError(t, s.DeleteConversationArchive(context.Background(), record.ID))
}

type archiveSizeMismatchStore struct{ *mockObjectStore }

func (s *archiveSizeMismatchStore) UploadFile(ctx context.Context, key, path, contentType string) (int64, error) {
	size, err := s.mockObjectStore.UploadFile(ctx, key, path, contentType)
	return size + 1, err
}

func TestConversationArchiveSizeMismatchRetainsUploadedObjectEvidence(t *testing.T) {
	s, settings, store := newConversationArchiveTestService(t)
	repo := &archiveRepoStub{ids: []int64{1}, rows: []json.RawMessage{json.RawMessage(`{"id":1}`)}}
	record := &ConversationArchiveRecord{ID: "mismatch", Status: "running", S3Key: "data", ManifestKey: "metadata", DeleteAfterBackup: true}
	require.NoError(t, s.saveConversationArchive(context.Background(), record, true))
	s.executeConversationArchive(repo, &archiveSizeMismatchStore{store}, ConversationLogFilters{}, record)
	require.Equal(t, "failed", record.Status)
	require.Positive(t, record.SizeBytes)
	require.Empty(t, record.SHA256)
	require.NotEmpty(t, store.objects[record.S3Key])
	require.Empty(t, repo.deleted)
	require.NoError(t, settings.Set(context.Background(), settingKeyBackupS3Config, ""))
	require.ErrorIs(t, s.DeleteConversationArchive(context.Background(), record.ID), ErrBackupS3NotConfigured)
}
