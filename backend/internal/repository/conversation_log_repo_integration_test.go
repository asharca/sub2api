//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestConversationLogRepositoryListBuildsPreviewFromLastUserText(t *testing.T) {
	requestID := "conversation-preview-" + uuid.NewString()
	body, err := json.Marshal(map[string]any{
		"messages": []any{
			map[string]any{"role": "user", "content": strings.Repeat("old context ", 400)},
			map[string]any{"role": "assistant", "content": "reply"},
			map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": "latest user message"}}},
			map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "content": "tool output"}}},
		},
	})
	require.NoError(t, err)

	_, err = integrationDB.ExecContext(context.Background(), `
INSERT INTO conversation_logs (request_id, platform, request_body, response_body)
VALUES ($1, 'anthropic', $2, 'large response')`, requestID, string(body))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM conversation_logs WHERE request_id = $1", requestID)
	})

	repo := NewConversationLogRepository(integrationDB)
	items, _, err := repo.List(context.Background(), pagination.PaginationParams{Page: 1, PageSize: 20}, service.ConversationLogFilters{RequestID: requestID})
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, "latest user message", gjson.Get(items[0].RequestBody, "input").String())
	require.Empty(t, items[0].ResponseBody)
}
