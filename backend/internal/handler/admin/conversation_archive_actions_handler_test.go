//go:build unit

package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type archiveActionHandlerStore struct {
	service.BackupObjectStore
	deleted   []string
	deleteErr error
}

func (s *archiveActionHandlerStore) Delete(_ context.Context, key string) error {
	s.deleted = append(s.deleted, key)
	return s.deleteErr
}

func newArchiveActionHandler(t *testing.T, records string, stream func(context.Context, service.ConversationLogFilters, func(int64, json.RawMessage) error) error) (*ConversationLogHandler, *archiveActionHandlerStore) {
	t.Helper()
	settings := &backupRequestSettings{data: map[string]string{
		"backup_s3_config":      `{"bucket":"test","access_key_id":"key","secret_access_key":"secret"}`,
		"conversation_archives": records,
	}}
	store := &archiveActionHandlerStore{}
	backup := service.NewBackupService(settings, &config.Config{}, backupRequestEncryptor{},
		func(context.Context, *service.BackupS3Config) (service.BackupObjectStore, error) { return store, nil }, nil)
	t.Cleanup(backup.Stop)
	logs := service.NewConversationLogService(&conversationExportHandlerRepo{stream: stream}, nil)
	return NewConversationLogHandler(logs, backup), store
}

func TestConversationArchiveRetryHandlerAcceptsFreshJobFromStoredFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	started := make(chan service.ConversationLogFilters, 1)
	h, _ := newArchiveActionHandler(t, `[{"id":"failed","status":"failed","filters":{"q":"original","user_id":42,"start_time":"2026-10-01T00:00:00Z","end_time":"2026-10-03T00:00:00Z"},"delete_after_backup":true}]`,
		func(_ context.Context, filters service.ConversationLogFilters, _ func(int64, json.RawMessage) error) error {
			started <- filters
			// This test covers HTTP acceptance, not worker completion. Snapshot
			// failure also guarantees the handler never cleans up source rows.
			return errors.New("snapshot failure in handler fixture")
		})
	router := gin.New()
	router.POST("/archives/:id/retry", h.RetryArchive)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/archives/failed/retry?q=ignored&user_id=99", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	var response struct {
		Data service.ConversationArchiveRecord `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.NotEmpty(t, response.Data.ID)
	require.NotEqual(t, "failed", response.Data.ID)
	require.Equal(t, "running", response.Data.Status)
	require.True(t, response.Data.DeleteAfterBackup)
	filters := <-started
	require.Equal(t, "original", filters.Search)
	require.EqualValues(t, 42, filters.UserID)
	require.Equal(t, "2026-10-01T00:00:00Z", filters.StartTime.Format("2006-01-02T15:04:05Z07:00"))
	require.Equal(t, "2026-10-03T00:00:00Z", filters.EndTime.Format("2006-01-02T15:04:05Z07:00"))
}

func TestConversationArchiveRetryHandlerRejectsInvalidBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, body := range []string{"", "{", "[]"} {
		t.Run(body, func(t *testing.T) {
			router := gin.New()
			router.POST("/archives/:id/retry", NewConversationLogHandler(nil, nil).RetryArchive)
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/archives/id/retry", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(w, req)
			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		})
	}
}

func TestConversationArchiveActionHandlersReturnStateAndMissingErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		method, path, records string
		status                int
	}{
		{http.MethodPost, "/archives/completed/retry", `[{"id":"completed","status":"completed"}]`, http.StatusConflict},
		{http.MethodPost, "/archives/running/retry", `[{"id":"running","status":"running"}]`, http.StatusConflict},
		{http.MethodPost, "/archives/cleanup/retry", `[{"id":"cleanup","status":"cleanup_failed"}]`, http.StatusConflict},
		{http.MethodPost, "/archives/missing/retry", `[]`, http.StatusNotFound},
		{http.MethodDelete, "/archives/running", `[{"id":"running","status":"running"}]`, http.StatusConflict},
		{http.MethodDelete, "/archives/missing", `[]`, http.StatusNotFound},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			h, store := newArchiveActionHandler(t, tc.records, nil)
			router := gin.New()
			router.POST("/archives/:id/retry", h.RetryArchive)
			router.DELETE("/archives/:id", h.DeleteArchive)
			w := httptest.NewRecorder()
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{}`))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(w, req)
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Empty(t, store.deleted)
		})
	}
}

func TestConversationArchiveDeleteHandlerRetainsRecordOnObjectFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, store := newArchiveActionHandler(t, `[{"id":"failed","status":"failed","s3_key":"data","manifest_key":"metadata"}]`, nil)
	router := gin.New()
	router.DELETE("/archives/:id", h.DeleteArchive)
	router.GET("/archives/:id", h.GetArchive)
	store.deleteErr = errors.New("storage unavailable")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/archives/failed", nil))
	require.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/archives/failed", nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	store.deleteErr = nil
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/archives/failed", nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, []string{"data", "data", "metadata"}, store.deleted)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/archives/failed", nil))
	require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
}
