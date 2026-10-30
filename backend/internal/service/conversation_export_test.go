//go:build unit

package service

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type conversationExportRepoStub struct {
	archiveRepoStub
	stream func(context.Context, ConversationLogFilters, func(int64, json.RawMessage) error) error
}

func (r *conversationExportRepoStub) StreamConversationArchive(ctx context.Context, filters ConversationLogFilters, emit func(int64, json.RawMessage) error) error {
	return r.stream(ctx, filters, emit)
}

func TestConversationLogExportCompleteSnapshotWithoutBackup(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("TMPDIR", tempDir)
	filters := ConversationLogFilters{Search: "needle", UserID: 42, Model: "model", RequestID: "request"}
	raw := json.RawMessage(`{"id":19,"request_body":"` + strings.Repeat("request", 2000) + `","response_body":"` + strings.Repeat("response", 2000) + `","unknown_column":{"preserved":true}}`)
	repo := &conversationExportRepoStub{}
	repo.stream = func(ctx context.Context, got ConversationLogFilters, emit func(int64, json.RawMessage) error) error {
		require.Equal(t, filters, got)
		_, deadline := ctx.Deadline()
		require.False(t, deadline, "export must not impose a total time limit")
		return emit(19, raw)
	}
	s := &ConversationLogService{repo: repo}
	export, err := s.ExportConversationLogs(context.Background(), filters)
	require.NoError(t, err)
	t.Cleanup(func() { _ = export.Close() })
	require.Regexp(t, `^conversations_\d{8}_\d{6}_[0-9a-f-]{36}\.jsonl\.gz$`, export.FileName)
	info, err := export.File.Stat()
	require.NoError(t, err)
	require.Equal(t, info.Size(), export.SizeBytes)
	reader, err := gzip.NewReader(export.File)
	require.NoError(t, err)
	decoded, err := io.ReadAll(reader)
	require.NoError(t, err, "gzip trailer must be complete before returning the export")
	require.NoError(t, reader.Close())
	require.Equal(t, string(raw)+"\n", string(decoded))
	require.Empty(t, repo.deleted)
	require.NoError(t, export.Close())
	entries, err := os.ReadDir(tempDir)
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestConversationLogExportEmptySnapshot(t *testing.T) {
	repo := &conversationExportRepoStub{stream: func(context.Context, ConversationLogFilters, func(int64, json.RawMessage) error) error { return nil }}
	export, err := (&ConversationLogService{repo: repo}).ExportConversationLogs(context.Background(), ConversationLogFilters{})
	require.NoError(t, err)
	defer func() { _ = export.Close() }()
	reader, err := gzip.NewReader(export.File)
	require.NoError(t, err)
	decoded, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	require.Empty(t, decoded)
}

func TestConversationLogExportFailureCleansTemporaryFiles(t *testing.T) {
	for _, failure := range []string{"snapshot", "cancelled_before", "cancelled_during", "cancelled_after_snapshot"} {
		t.Run(failure, func(t *testing.T) {
			tempDir := t.TempDir()
			t.Setenv("TMPDIR", tempDir)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if failure == "cancelled_before" {
				cancel()
			}
			repo := &conversationExportRepoStub{}
			repo.stream = func(_ context.Context, _ ConversationLogFilters, emit func(int64, json.RawMessage) error) error {
				if failure == "cancelled_during" {
					cancel()
				}
				if err := emit(1, json.RawMessage(`{"id":1}`)); err != nil {
					return err
				}
				if failure == "cancelled_after_snapshot" {
					cancel()
					return nil
				}
				return errors.New("snapshot failed")
			}
			export, err := (&ConversationLogService{repo: repo}).ExportConversationLogs(ctx, ConversationLogFilters{})
			require.Error(t, err)
			require.Nil(t, export)
			if strings.HasPrefix(failure, "cancelled") {
				require.ErrorIs(t, err, context.Canceled)
			}
			require.Empty(t, repo.deleted)
			entries, err := os.ReadDir(tempDir)
			require.NoError(t, err)
			require.Empty(t, entries)
		})
	}
}

func TestConversationLogExportUnavailable(t *testing.T) {
	for _, s := range []*ConversationLogService{nil, {}} {
		export, err := s.ExportConversationLogs(context.Background(), ConversationLogFilters{})
		require.Error(t, err)
		require.Nil(t, export)
	}
}
