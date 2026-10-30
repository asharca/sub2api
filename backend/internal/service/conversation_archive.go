package service

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/google/uuid"
)

const settingKeyConversationArchives = "conversation_archives"

// ConversationArchiveRepository is separate from the interactive list API,
// which deliberately returns payload previews rather than complete rows.
type ConversationArchiveRepository interface {
	StreamConversationArchive(context.Context, ConversationLogFilters, func(int64, json.RawMessage) error) error
	DeleteConversationArchiveIDs(context.Context, io.Reader) (int64, error)
}

// ConversationLogExport owns a completed temporary gzip file. Callers must close
// it after sending the response, including when the client disconnects.
type ConversationLogExport struct {
	File      *os.File
	FileName  string
	SizeBytes int64
}

func (e *ConversationLogExport) Close() error {
	return errors.Join(e.File.Close(), os.Remove(e.File.Name()))
}

// ExportConversationLogs uses the complete-row snapshot repository, never the
// interactive preview API, and does not persist archive records or delete rows.
func (s *ConversationLogService) ExportConversationLogs(ctx context.Context, filters ConversationLogFilters) (*ConversationLogExport, error) {
	if s == nil {
		return nil, infraerrors.InternalServer("CONVERSATION_EXPORT_UNAVAILABLE", "conversation storage is unavailable")
	}
	repo, ok := s.repo.(ConversationArchiveRepository)
	if !ok {
		return nil, infraerrors.InternalServer("CONVERSATION_EXPORT_UNAVAILABLE", "conversation storage does not support exports")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, err := os.CreateTemp("", "conversation-export-*.jsonl.gz")
	if err != nil {
		return nil, err
	}
	result := &ConversationLogExport{File: file}
	completed := false
	defer func() {
		if !completed {
			_ = result.Close()
		}
	}()
	compressed := gzip.NewWriter(file)
	newline := []byte{'\n'}
	err = repo.StreamConversationArchive(ctx, filters, func(_ int64, raw json.RawMessage) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := compressed.Write(raw); err != nil {
			return err
		}
		_, err := compressed.Write(newline)
		return err
	})
	closeErr := compressed.Close()
	if err != nil {
		return nil, fmt.Errorf("snapshot conversations: %w", err)
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	result.FileName = fmt.Sprintf("conversations_%s_%s.jsonl.gz", time.Now().UTC().Format("20060102_150405"), uuid.NewString())
	result.SizeBytes = info.Size()
	completed = true
	return result, nil
}

type ConversationArchiveRecord struct {
	ID                string         `json:"id"`
	Status            string         `json:"status"`
	FileName          string         `json:"file_name"`
	S3Key             string         `json:"s3_key"`
	ManifestKey       string         `json:"manifest_key"`
	SizeBytes         int64          `json:"size_bytes"`
	SHA256            string         `json:"sha256"`
	RecordCount       int64          `json:"record_count"`
	DeletedCount      int64          `json:"deleted_count"`
	Filters           map[string]any `json:"filters"`
	DeleteAfterBackup bool           `json:"delete_after_backup"`
	StartedAt         string         `json:"started_at"`
	LastHeartbeatAt   string         `json:"last_heartbeat_at,omitempty"`
	FinishedAt        string         `json:"finished_at,omitempty"`
	ErrorMessage      string         `json:"error_message,omitempty"`
}

func conversationArchiveFilters(f ConversationLogFilters) map[string]any {
	return map[string]any{
		"q": f.Search, "user_id": f.UserID, "api_key_id": f.APIKeyID,
		"account_id": f.AccountID, "group_id": f.GroupID, "platform": f.Platform,
		"model": f.Model, "request_id": f.RequestID, "response_id": f.ResponseID,
		"request_type": f.RequestType, "stream": f.Stream,
		"upstream_model_mismatch": f.UpstreamModelMismatch,
		"start_time":              f.StartTime, "end_time": f.EndTime,
	}
}

func (s *BackupService) StartConversationArchive(ctx context.Context, logs *ConversationLogService, filters ConversationLogFilters, deleteAfterBackup bool) (*ConversationArchiveRecord, error) {
	if logs == nil {
		return nil, infraerrors.InternalServer("CONVERSATION_ARCHIVE_UNAVAILABLE", "conversation storage is unavailable")
	}
	repo, ok := logs.repo.(ConversationArchiveRepository)
	if !ok {
		return nil, infraerrors.InternalServer("CONVERSATION_ARCHIVE_UNAVAILABLE", "conversation storage does not support archives")
	}
	s.opMu.Lock()
	if s.shuttingDown.Load() {
		s.opMu.Unlock()
		return nil, infraerrors.ServiceUnavailable("SERVER_SHUTTING_DOWN", "server is shutting down")
	}
	if s.backingUp || s.restoring {
		s.opMu.Unlock()
		return nil, ErrBackupInProgress
	}
	s.backingUp = true
	s.wg.Add(1)
	s.opMu.Unlock()
	launched := false
	defer func() {
		if !launched {
			s.finishConversationArchive()
		}
	}()
	cfg, err := s.loadS3Config(ctx)
	if err != nil {
		return nil, err
	}
	if cfg == nil || !cfg.IsConfigured() {
		return nil, ErrBackupS3NotConfigured
	}
	store, err := s.getOrCreateStore(ctx, cfg)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	id := uuid.NewString()
	name := fmt.Sprintf("conversations_%s_%s.jsonl.gz", now.Format("20060102_150405"), id)
	record := &ConversationArchiveRecord{
		ID: id, Status: "running", FileName: name,
		S3Key:   s.buildS3Key(cfg, "conversations/"+name),
		Filters: conversationArchiveFilters(filters), DeleteAfterBackup: deleteAfterBackup,
		StartedAt:       now.Format(time.RFC3339),
		LastHeartbeatAt: now.Format(time.RFC3339),
	}
	record.ManifestKey = record.S3Key + ".metadata.json"
	if err := s.saveConversationArchive(ctx, record, true); err != nil {
		return nil, err
	}
	result := *record
	launched = true
	go func() {
		defer s.finishConversationArchive()
		s.executeConversationArchive(repo, store, filters, record)
	}()
	return &result, nil
}

func (s *BackupService) finishConversationArchive() {
	s.opMu.Lock()
	s.backingUp = false
	s.opMu.Unlock()
	s.wg.Done()
}

func (s *BackupService) executeConversationArchive(repo ConversationArchiveRepository, store BackupObjectStore, filters ConversationLogFilters, record *ConversationArchiveRecord) {
	ctx := s.bgCtx
	heartbeatCtx, stopHeartbeat := context.WithCancel(ctx)
	heartbeatDone := make(chan struct{})
	go s.runConversationArchiveHeartbeat(heartbeatCtx, record.ID, heartbeatDone)
	defer func() {
		if recovered := recover(); recovered != nil {
			if record.SHA256 != "" && record.DeleteAfterBackup {
				record.Status = "cleanup_failed"
			} else {
				record.Status = "failed"
			}
			record.ErrorMessage = fmt.Sprintf("conversation archive interrupted: %v", recovered)
		}
		stopHeartbeat()
		<-heartbeatDone
		record.FinishedAt = time.Now().UTC().Format(time.RFC3339)
		persistCtx, done := context.WithTimeout(context.Background(), 30*time.Second)
		defer done()
		if err := s.saveConversationArchive(persistCtx, record, false); err != nil {
			logger.LegacyPrintf("service.conversation_archive", "save archive %s outcome: %v", record.ID, err)
		}
	}()
	if err := s.runConversationArchive(ctx, repo, store, filters, record); err != nil {
		if record.Status != "cleanup_failed" {
			record.Status = "failed"
		}
		record.ErrorMessage = err.Error()
		return
	}
	record.Status = "completed"
}

// Heartbeats track worker liveness, not total job duration or row throughput.
// Slow snapshots/uploads must remain running even if they take many hours.
func (s *BackupService) runConversationArchiveHeartbeat(ctx context.Context, id string, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.touchConversationArchive(ctx, id); err != nil && ctx.Err() == nil {
				logger.LegacyPrintf("service.conversation_archive", "refresh archive %s heartbeat: %v", id, err)
			}
		}
	}
}

func (s *BackupService) touchConversationArchive(ctx context.Context, id string) error {
	ctx, release, err := s.lockBackupRecordUpdates(ctx)
	if err != nil {
		return err
	}
	defer release()
	records, err := s.loadConversationArchives(ctx)
	if err != nil {
		return err
	}
	for i := range records {
		if records[i].ID != id || records[i].Status != "running" {
			continue
		}
		records[i].LastHeartbeatAt = time.Now().UTC().Format(time.RFC3339)
		data, err := json.Marshal(records)
		if err != nil {
			return err
		}
		return s.settingRepo.Set(ctx, settingKeyConversationArchives, string(data))
	}
	return nil
}

func conversationArchiveInterrupted(record *ConversationArchiveRecord, now time.Time) bool {
	lastAlive := record.LastHeartbeatAt
	if lastAlive == "" {
		lastAlive = record.StartedAt // Records created before heartbeats were added.
	}
	return backupOperationExpired(lastAlive, now)
}

func (s *BackupService) runConversationArchive(ctx context.Context, repo ConversationArchiveRepository, store BackupObjectStore, filters ConversationLogFilters, record *ConversationArchiveRecord) error {
	file, err := os.CreateTemp("", "conversation-archive-*.jsonl.gz")
	if err != nil {
		return err
	}
	defer func() { _ = file.Close(); _ = os.Remove(file.Name()) }()
	var ids *os.File
	var idWriter *bufio.Writer
	if record.DeleteAfterBackup {
		ids, err = os.CreateTemp("", "conversation-archive-ids-*")
		if err != nil {
			return err
		}
		defer func() { _ = ids.Close(); _ = os.Remove(ids.Name()) }()
		idWriter = bufio.NewWriter(ids)
	}
	hash := sha256.New()
	compressed := gzip.NewWriter(io.MultiWriter(file, hash))
	err = repo.StreamConversationArchive(ctx, filters, func(id int64, raw json.RawMessage) error {
		if _, err := compressed.Write(raw); err != nil {
			return err
		}
		if _, err := compressed.Write([]byte{'\n'}); err != nil {
			return err
		}
		if idWriter != nil {
			var encoded [8]byte
			binary.LittleEndian.PutUint64(encoded[:], uint64(id))
			if _, err := idWriter.Write(encoded[:]); err != nil {
				return err
			}
		}
		record.RecordCount++
		return nil
	})
	closeErr := compressed.Close()
	if err != nil {
		return fmt.Errorf("snapshot conversations: %w", err)
	}
	if closeErr != nil {
		return closeErr
	}
	if idWriter != nil {
		if err := idWriter.Flush(); err != nil {
			return err
		}
		if _, err := ids.Seek(0, io.SeekStart); err != nil {
			return err
		}
	}
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	size, err := store.UploadFile(ctx, record.S3Key, file.Name(), "application/gzip")
	if err != nil {
		return fmt.Errorf("upload conversation archive: %w", err)
	}
	if size != info.Size() {
		return fmt.Errorf("conversation archive upload size mismatch: expected %d, got %d", info.Size(), size)
	}
	record.SizeBytes = size
	record.SHA256 = hex.EncodeToString(hash.Sum(nil))
	// The sidecar describes the immutable uploaded snapshot, not mutable cleanup
	// state. Both it and the catalog must be durable before deleting any row.
	manifest, err := json.Marshal(map[string]any{
		"schema_version": 1, "id": record.ID, "file_name": record.FileName,
		"s3_key": record.S3Key, "size_bytes": size, "sha256": record.SHA256,
		"record_count": record.RecordCount, "filters": record.Filters,
		"delete_after_backup": record.DeleteAfterBackup, "started_at": record.StartedAt,
	})
	if err != nil {
		return err
	}
	if _, err := store.Upload(ctx, record.ManifestKey, bytes.NewReader(manifest), "application/json"); err != nil {
		return fmt.Errorf("upload conversation archive metadata: %w", err)
	}
	if err := s.saveConversationArchive(ctx, record, false); err != nil {
		return fmt.Errorf("persist uploaded conversation archive: %w", err)
	}
	if ids != nil {
		record.DeletedCount, err = repo.DeleteConversationArchiveIDs(ctx, bufio.NewReader(ids))
		if err != nil {
			record.Status = "cleanup_failed"
			return fmt.Errorf("archive retained; source cleanup failed: %w", err)
		}
	}
	return nil
}

func (s *BackupService) loadConversationArchives(ctx context.Context) ([]ConversationArchiveRecord, error) {
	raw, err := s.settingRepo.GetValue(ctx, settingKeyConversationArchives)
	if err != nil && !errors.Is(err, ErrSettingNotFound) {
		return nil, err
	}
	records := make([]ConversationArchiveRecord, 0)
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &records); err != nil {
			return nil, infraerrors.InternalServer("CONVERSATION_ARCHIVES_CORRUPT", "conversation archive catalog is corrupted")
		}
	}
	return records, nil
}

func (s *BackupService) saveConversationArchive(ctx context.Context, record *ConversationArchiveRecord, create bool) error {
	ctx, release, err := s.lockBackupRecordUpdates(ctx)
	if err != nil {
		return err
	}
	defer release()
	records, err := s.loadConversationArchives(ctx)
	if err != nil {
		return err
	}
	found := false
	for i := range records {
		if create && records[i].Status == "running" && !conversationArchiveInterrupted(&records[i], time.Now()) {
			return ErrBackupInProgress
		}
		if records[i].ID == record.ID {
			if records[i].LastHeartbeatAt != "" {
				record.LastHeartbeatAt = records[i].LastHeartbeatAt
			}
			records[i] = *record
			found = true
		}
	}
	if !found {
		if !create {
			return ErrBackupNotFound
		}
		records = append([]ConversationArchiveRecord{*record}, records...)
	}
	data, err := json.Marshal(records)
	if err != nil {
		return err
	}
	return s.settingRepo.Set(ctx, settingKeyConversationArchives, string(data))
}

func (s *BackupService) ListConversationArchives(ctx context.Context) ([]ConversationArchiveRecord, error) {
	ctx, release, err := s.lockBackupRecordUpdates(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	records, err := s.loadConversationArchives(ctx)
	if err != nil {
		return nil, err
	}
	changed := false
	for i := range records {
		r := &records[i]
		if r.Status != "running" || !conversationArchiveInterrupted(r, time.Now()) {
			continue
		}
		r.Status = "failed"
		r.ErrorMessage = "archive operation interrupted before completion; source cleanup was not confirmed"
		if r.SHA256 != "" {
			r.Status = "cleanup_failed"
		}
		r.FinishedAt = time.Now().UTC().Format(time.RFC3339)
		changed = true
	}
	if changed {
		data, err := json.Marshal(records)
		if err != nil {
			return nil, err
		}
		if err := s.settingRepo.Set(ctx, settingKeyConversationArchives, string(data)); err != nil {
			return nil, err
		}
	}
	return records, nil
}

func (s *BackupService) GetConversationArchive(ctx context.Context, id string) (*ConversationArchiveRecord, error) {
	records, err := s.ListConversationArchives(ctx)
	if err != nil {
		return nil, err
	}
	for i := range records {
		if records[i].ID == id {
			return &records[i], nil
		}
	}
	return nil, ErrBackupNotFound
}

func (s *BackupService) GetConversationArchiveDownloadURL(ctx context.Context, id string) (string, error) {
	record, err := s.GetConversationArchive(ctx, id)
	if err != nil {
		return "", err
	}
	if record.SHA256 == "" || record.Status == "running" {
		return "", infraerrors.BadRequest("CONVERSATION_ARCHIVE_NOT_READY", "conversation archive is not ready for download")
	}
	cfg, err := s.loadS3Config(ctx)
	if err != nil {
		return "", err
	}
	if cfg == nil || !cfg.IsConfigured() {
		return "", ErrBackupS3NotConfigured
	}
	store, err := s.getOrCreateStore(ctx, cfg)
	if err != nil {
		return "", err
	}
	return store.PresignURL(ctx, record.S3Key, 15*time.Minute)
}
