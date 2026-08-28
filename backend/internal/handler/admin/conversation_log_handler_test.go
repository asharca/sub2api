package admin

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestConversationLogToListResponseKeepsRequestPreview(t *testing.T) {
	requestBody := `{"messages":[{"role":"user","content":"private request"}]}`
	response := conversationLogToListResponse(&service.ConversationLog{
		RequestBody:  requestBody,
		ResponseBody: `{"choices":[{"message":{"content":"private response"}}]}`,
	})

	if response.RequestBody != requestBody || response.ResponseBody != "" {
		t.Fatalf("list response must keep the request preview and omit the response body: %+v", response)
	}
}
