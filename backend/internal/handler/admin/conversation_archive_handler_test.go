package admin

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type conversationExportHandlerRepo struct {
	service.ConversationLogRepository
	stream func(context.Context, service.ConversationLogFilters, func(int64, json.RawMessage) error) error
}

func (r *conversationExportHandlerRepo) StreamConversationArchive(ctx context.Context, filters service.ConversationLogFilters, emit func(int64, json.RawMessage) error) error {
	return r.stream(ctx, filters, emit)
}

func (r *conversationExportHandlerRepo) DeleteConversationArchiveIDs(context.Context, io.Reader) (int64, error) {
	panic("browser export must never delete conversations")
}

func TestConversationLogExportHandlerAttachmentAndFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tempDir := t.TempDir()
	t.Setenv("TMPDIR", tempDir)
	raw := json.RawMessage(`{"id":17,"request_body":"` + strings.Repeat("full request", 1000) + `","response_body":"` + strings.Repeat("full response", 1000) + `","extra_snapshot_field":true}`)
	repo := &conversationExportHandlerRepo{}
	repo.stream = func(ctx context.Context, filters service.ConversationLogFilters, emit func(int64, json.RawMessage) error) error {
		require.Equal(t, "search", filters.Search)
		require.EqualValues(t, 42, filters.UserID)
		require.EqualValues(t, 9, filters.APIKeyID)
		require.EqualValues(t, 7, filters.AccountID)
		require.EqualValues(t, 4, filters.GroupID)
		require.Equal(t, "openai", filters.Platform)
		require.Equal(t, "model", filters.Model)
		require.Equal(t, "request", filters.RequestID)
		require.Equal(t, "response", filters.ResponseID)
		require.NotNil(t, filters.RequestType)
		require.NotNil(t, filters.Stream)
		require.False(t, *filters.Stream)
		require.NotNil(t, filters.UpstreamModelMismatch)
		require.True(t, *filters.UpstreamModelMismatch)
		require.Equal(t, "2026-10-01T00:00:00+08:00", filters.StartTime.Format(time.RFC3339))
		require.Equal(t, "2026-10-03T00:00:00+08:00", filters.EndTime.Format(time.RFC3339))
		_, deadline := ctx.Deadline()
		require.False(t, deadline)
		return emit(17, raw)
	}
	h := NewConversationLogHandler(service.NewConversationLogService(repo, nil), nil)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/conversation-logs/export?q=search&user_id=42&api_key_id=9&account_id=7&group_id=4&platform=openai&model=model&request_id=request&response_id=response&request_type=stream&stream=false&upstream_model_mismatch=true&start_date=2026-10-01&end_date=2026-10-02&timezone=Asia%2FShanghai", strings.NewReader(`{}`))
	c.Request.Header.Set("Content-Type", "application/json")
	h.Export(c)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "application/gzip", recorder.Header().Get("Content-Type"))
	require.Regexp(t, `^attachment; filename="conversations_\d{8}_\d{6}_[0-9a-f-]{36}\.jsonl\.gz"$`, recorder.Header().Get("Content-Disposition"))
	require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
	require.Empty(t, recorder.Header().Get("Content-Encoding"))
	require.Equal(t, strconv.Itoa(recorder.Body.Len()), recorder.Header().Get("Content-Length"))
	reader, err := gzip.NewReader(recorder.Body)
	require.NoError(t, err)
	decoded, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	require.Equal(t, string(raw)+"\n", string(decoded))
	entries, err := os.ReadDir(tempDir)
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestConversationLogExportHandlerRejectsInvalidInput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct{ query, body string }{
		{"?start_date=2026-10-04&end_date=2026-10-02", `{}`},
		{"?start_date=invalid", `{}`},
		{"?end_date=invalid", `{}`},
		{"?user_id=-1", `{}`},
		{"?api_key_id=invalid", `{}`},
		{"?stream=invalid", `{}`},
		{"?request_type=invalid", `{}`},
		{"?upstream_model_mismatch=invalid", `{}`},
		{"", ""},
		{"", "{"},
		{"", "[]"},
	} {
		t.Run(tc.query+tc.body, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/conversation-logs/export"+tc.query, strings.NewReader(tc.body))
			NewConversationLogHandler(nil, nil).Export(c)
			require.Equal(t, http.StatusBadRequest, recorder.Code)
			require.Contains(t, recorder.Header().Get("Content-Type"), "application/json")
			require.Empty(t, recorder.Header().Get("Content-Disposition"))
		})
	}
}

func TestConversationLogExportHandlerSnapshotFailureReturnsJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tempDir := t.TempDir()
	t.Setenv("TMPDIR", tempDir)
	repo := &conversationExportHandlerRepo{stream: func(_ context.Context, _ service.ConversationLogFilters, emit func(int64, json.RawMessage) error) error {
		if err := emit(1, json.RawMessage(`{"id":1}`)); err != nil {
			return err
		}
		return errors.New("snapshot failed after a row")
	}}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/conversation-logs/export", strings.NewReader(`{}`))
	NewConversationLogHandler(service.NewConversationLogService(repo, nil), nil).Export(c)
	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	require.Contains(t, recorder.Header().Get("Content-Type"), "application/json")
	require.Empty(t, recorder.Header().Get("Content-Disposition"))
	require.True(t, json.Valid(recorder.Body.Bytes()))
	entries, err := os.ReadDir(tempDir)
	require.NoError(t, err)
	require.Empty(t, entries)
}

type disconnectedConversationExportWriter struct {
	*httptest.ResponseRecorder
	cancel context.CancelFunc
}

func (w *disconnectedConversationExportWriter) Write([]byte) (int, error) {
	w.cancel()
	return 0, errors.New("client disconnected")
}

func TestConversationLogExportHandlerDisconnectCleansTemporaryFile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tempDir := t.TempDir()
	t.Setenv("TMPDIR", tempDir)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	repo := &conversationExportHandlerRepo{stream: func(_ context.Context, _ service.ConversationLogFilters, emit func(int64, json.RawMessage) error) error {
		return emit(1, json.RawMessage(`{"id":1,"response_body":"full"}`))
	}}
	writer := &disconnectedConversationExportWriter{ResponseRecorder: httptest.NewRecorder(), cancel: cancel}
	c, _ := gin.CreateTestContext(writer)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/conversation-logs/export", strings.NewReader(`{}`)).WithContext(ctx)
	NewConversationLogHandler(service.NewConversationLogService(repo, nil), nil).Export(c)
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	require.NotEmpty(t, c.Errors)
	entries, err := os.ReadDir(tempDir)
	require.NoError(t, err)
	require.Empty(t, entries)
}
