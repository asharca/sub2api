//go:build unit

package admin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type backupRequestSettings struct {
	service.SettingRepository
	mu   sync.Mutex
	data map[string]string
}

func (r *backupRequestSettings) GetValue(_ context.Context, key string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.data[key], nil
}

func (r *backupRequestSettings) Set(_ context.Context, key, value string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data[key] = value
	return nil
}

type backupRequestEncryptor struct{ service.SecretEncryptor }

func (backupRequestEncryptor) Decrypt(value string) (string, error) { return value, nil }

type backupRequestDumper struct{ service.DBDumper }

func (backupRequestDumper) Dump(context.Context, bool) (io.ReadCloser, error) {
	return nil, errors.New("request test does not create a database dump")
}

func newBackupRequestHandler(t *testing.T) *BackupHandler {
	t.Helper()
	repo := &backupRequestSettings{data: map[string]string{
		"backup_s3_config": `{"bucket":"test","access_key_id":"key","secret_access_key":"secret"}`,
	}}
	svc := service.NewBackupService(repo, &config.Config{}, backupRequestEncryptor{},
		func(context.Context, *service.BackupS3Config) (service.BackupObjectStore, error) {
			return nil, nil // Dump fails before any object store operation.
		}, backupRequestDumper{})
	t.Cleanup(svc.Stop)
	return NewBackupHandler(svc, nil, nil)
}

func TestBackupHandlerManualConversationInclusion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name    string
		body    string
		include bool
	}{
		{name: "empty body", include: true},
		{name: "omitted", body: `{}`, include: true},
		{name: "null", body: `{"include_conversation_logs":null}`, include: true},
		{name: "include", body: `{"include_conversation_logs":true}`, include: true},
		{name: "exclude", body: `{"include_conversation_logs":false}`, include: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newBackupRequestHandler(t)
			router := gin.New()
			router.POST("/backups", h.CreateBackup)
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/backups", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(w, req)
			require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
			var response struct {
				Data service.BackupRecord `json:"data"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
			require.Equal(t, tc.include, response.Data.IncludeConversationLogs)
		})
	}
}

func TestBackupHandlerRejectsInvalidConversationInclusion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, body := range []string{`{"include_conversation_logs":"false"}`, `{"include_conversation_logs":0}`, `{"include_conversation_logs":`} {
		// A nil service ensures malformed JSON is rejected before starting a backup.
		h := NewBackupHandler(nil, nil, nil)
		router := gin.New()
		router.POST("/backups", h.CreateBackup)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/backups", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	}
}

func TestBackupHandlerScheduleConversationInclusion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		body    string
		include bool
	}{
		{body: `{}`, include: true},
		{body: `{"include_conversation_logs":null}`, include: true},
		{body: `{"include_conversation_logs":true}`, include: true},
		{body: `{"include_conversation_logs":false}`, include: false},
	} {
		t.Run(tc.body, func(t *testing.T) {
			h := newBackupRequestHandler(t)
			router := gin.New()
			router.PUT("/schedule", h.UpdateSchedule)
			router.GET("/schedule", h.GetSchedule)
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPut, "/schedule", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(w, req)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			for _, method := range []string{http.MethodPut, http.MethodGet} {
				if method == http.MethodGet {
					w = httptest.NewRecorder()
					router.ServeHTTP(w, httptest.NewRequest(method, "/schedule", nil))
					require.Equal(t, http.StatusOK, w.Code, w.Body.String())
				}
				var response struct {
					Data service.BackupScheduleConfig `json:"data"`
				}
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
				require.Equal(t, tc.include, response.Data.IncludeConversationLogs)
			}
		})
	}
}
