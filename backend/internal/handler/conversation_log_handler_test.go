package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type userConversationLogRepoStub struct {
	listFilters service.ConversationLogFilters
	getLog      *service.ConversationLog
}

func (s *userConversationLogRepoStub) Create(context.Context, *service.ConversationLog) error {
	return nil
}

func (s *userConversationLogRepoStub) List(_ context.Context, params pagination.PaginationParams, filters service.ConversationLogFilters) ([]service.ConversationLog, *pagination.PaginationResult, error) {
	s.listFilters = filters
	return []service.ConversationLog{
			{
				ID:              101,
				UserID:          filters.UserID,
				APIKeyID:        9,
				APIKeyName:      "default",
				Platform:        "openai",
				InboundEndpoint: "/v1/responses",
				Model:           "gpt-test",
				RequestType:     service.RequestTypeSync,
				StatusCode:      http.StatusOK,
				RequestBody:     `{"input":"hello"}`,
				ResponseBody:    `{"output":"ok"}`,
				CreatedAt:       time.Unix(10, 0),
			},
		},
		&pagination.PaginationResult{Total: 1, Page: params.Page, PageSize: params.Limit(), Pages: 1}, nil
}

func (s *userConversationLogRepoStub) GetByID(context.Context, int64) (*service.ConversationLog, error) {
	if s.getLog == nil {
		return nil, service.ErrConversationLogNotFound
	}
	return s.getLog, nil
}

func TestConversationLogHandlerListMineForcesCurrentUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &userConversationLogRepoStub{}
	svc := service.NewConversationLogService(repo, &config.Config{})
	h := NewConversationLogHandler(svc)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/conversation-logs?user_id=999&api_key_id=9", nil)
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 42})

	h.ListMine(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
	if repo.listFilters.UserID != 42 {
		t.Fatalf("expected filters.UserID forced to 42, got %d", repo.listFilters.UserID)
	}
	if repo.listFilters.APIKeyID != 9 {
		t.Fatalf("expected api_key_id filter preserved, got %d", repo.listFilters.APIKeyID)
	}

	var body struct {
		Data struct {
			Items []userConversationLogResponse `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Data.Items) != 1 || body.Data.Items[0].AccountName != "" || body.Data.Items[0].UpstreamEndpoint != "" {
		t.Fatalf("user response should not expose internal account fields: %+v", body.Data.Items)
	}
}

func TestConversationLogHandlerGetMineRejectsOtherUsersLog(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &userConversationLogRepoStub{
		getLog: &service.ConversationLog{
			ID:        101,
			UserID:    7,
			CreatedAt: time.Unix(10, 0),
		},
	}
	svc := service.NewConversationLogService(repo, &config.Config{})
	h := NewConversationLogHandler(svc)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "id", Value: "101"}}
	c.Request = httptest.NewRequest(http.MethodGet, "/conversation-logs/101", nil)
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 42})

	h.GetMineByID(c)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d: %s", w.Code, w.Body.String())
	}
}
