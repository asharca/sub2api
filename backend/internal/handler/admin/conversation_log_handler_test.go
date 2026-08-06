package admin

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestConversationLogToListResponseOmitsPayloadBodies(t *testing.T) {
	response := conversationLogToListResponse(&service.ConversationLog{
		RequestBody:  `{"messages":[{"role":"user","content":"private request"}]}`,
		ResponseBody: `{"choices":[{"message":{"content":"private response"}}]}`,
	})

	if response.RequestBody != "" || response.ResponseBody != "" {
		t.Fatalf("list response must omit payload bodies: %+v", response)
	}
}
