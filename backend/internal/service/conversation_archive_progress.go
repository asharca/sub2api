package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync/atomic"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

var errConversationArchiveCancelled = errors.New("conversation archive cancelled")

func conversationArchiveActive(status string) bool {
	return status == "running" || status == "cancelling"
}

type conversationArchiveProgress struct {
	rows     atomic.Int64
	bytes    atomic.Int64
	uploaded atomic.Int64
}

func (p *conversationArchiveProgress) apply(r *ConversationArchiveRecord) {
	r.ProgressRows = p.rows.Load()
	r.ProgressBytes = p.bytes.Load()
	r.UploadedBytes = p.uploaded.Load()
}

type conversationArchiveProgressWriter struct {
	io.Writer
	progress *conversationArchiveProgress
}

func (w conversationArchiveProgressWriter) Write(b []byte) (int, error) {
	n, err := w.Writer.Write(b)
	w.progress.bytes.Add(int64(n))
	return n, err
}

type conversationArchiveProgressUploader interface {
	UploadFileWithProgress(ctx context.Context, key, filePath, contentType string, progress func(int64)) (int64, error)
}

func (s *BackupService) CancelConversationArchive(ctx context.Context, id string) (*ConversationArchiveRecord, error) {
	ctx, release, err := s.lockBackupRecordUpdates(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	records, err := s.loadConversationArchives(ctx)
	if err != nil {
		return nil, err
	}
	for i := range records {
		r := &records[i]
		if r.ID != id {
			continue
		}
		if r.Status == "cancelling" || r.Status == "cancelled" {
			return r, nil
		}
		if r.Status != "running" || !r.CanCancel || conversationArchiveInterrupted(r, time.Now()) {
			return nil, infraerrors.Conflict("CONVERSATION_ARCHIVE_NOT_CANCELLABLE", "archive cannot be cancelled in its current phase")
		}
		r.Status = "cancelling"
		r.CanCancel = false
		if err := s.persistConversationArchives(ctx, records); err != nil {
			return nil, err
		}
		return r, nil
	}
	return nil, ErrBackupNotFound
}

// Phase transitions and cancellation are serialized by the same catalog lock.
// If cancellation wins, the worker cannot enter finalization or delete source rows.
func (s *BackupService) advanceConversationArchive(ctx context.Context, record *ConversationArchiveRecord, progress *conversationArchiveProgress, phase string) error {
	previousPhase, previousCancel := record.Phase, record.CanCancel
	record.Phase = phase
	record.CanCancel = phase == "snapshot" || phase == "upload"
	progress.apply(record)
	if err := s.saveConversationArchive(ctx, record, false); err != nil {
		record.Phase, record.CanCancel = previousPhase, previousCancel
		return err
	}
	return nil
}

func (s *BackupService) monitorConversationArchive(ctx context.Context, id string, progress *conversationArchiveProgress, cancel context.CancelCauseFunc, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			requested, err := s.refreshConversationArchive(ctx, id, progress)
			if err != nil && ctx.Err() == nil {
				logger.LegacyPrintf("service.conversation_archive", "refresh archive %s progress: %v", id, err)
			}
			if requested {
				cancel(errConversationArchiveCancelled)
				return
			}
		}
	}
}

func (s *BackupService) refreshConversationArchive(ctx context.Context, id string, progress *conversationArchiveProgress) (bool, error) {
	ctx, release, err := s.lockBackupRecordUpdates(ctx)
	if err != nil {
		return false, err
	}
	defer release()
	records, err := s.loadConversationArchives(ctx)
	if err != nil {
		return false, err
	}
	for i := range records {
		r := &records[i]
		if r.ID != id {
			continue
		}
		if r.Status == "cancelling" || r.Status == "cancelled" {
			return true, nil
		}
		if r.Status != "running" {
			return false, nil
		}
		rows, bytes, uploaded := r.ProgressRows, r.ProgressBytes, r.UploadedBytes
		progress.apply(r)
		now := time.Now().UTC()
		last, _ := time.Parse(time.RFC3339, r.LastHeartbeatAt)
		if rows == r.ProgressRows && bytes == r.ProgressBytes && uploaded == r.UploadedBytes && now.Sub(last) < time.Minute {
			return false, nil
		}
		r.LastHeartbeatAt = now.Format(time.RFC3339)
		return false, s.persistConversationArchives(ctx, records)
	}
	return false, nil
}

func (s *BackupService) persistConversationArchives(ctx context.Context, records []ConversationArchiveRecord) error {
	data, err := json.Marshal(records)
	if err != nil {
		return err
	}
	return s.settingRepo.Set(ctx, settingKeyConversationArchives, string(data))
}
