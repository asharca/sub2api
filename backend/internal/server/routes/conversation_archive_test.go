package routes

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestConversationArchiveCreateAndDownloadRequireStepUp(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := &handler.Handlers{Admin: &handler.AdminHandlers{ConversationLog: adminhandler.NewConversationLogHandler(nil, nil)}}
	gateCalls := 0
	gate := middleware.StepUpAuthMiddleware(func(c *gin.Context) {
		gateCalls++
		c.AbortWithStatus(http.StatusUnauthorized)
	})
	registerConversationLogRoutes(router.Group("/admin"), h, gate)
	for _, req := range []struct{ method, path, body string }{
		{http.MethodPost, "/admin/conversation-logs/export", `{}`},
		{http.MethodPost, "/admin/conversation-logs/archives", `{}`},
		{http.MethodPost, "/admin/conversation-logs/archives", `{"delete_after_backup":true}`},
		{http.MethodGet, "/admin/conversation-logs/archives/archive-id/download", ""},
	} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(req.method, req.path, strings.NewReader(req.body))
		router.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusUnauthorized, recorder.Code)
	}
	require.Equal(t, 4, gateCalls)
}
