//go:build integration

package repository

import (
	"context"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestConversationLogRepositoryListBuildsPreviewFromLongJSONBIncompatibleRequest(t *testing.T) {
	var parallelSafety string
	require.NoError(t, integrationDB.QueryRowContext(context.Background(), `
SELECT proparallel::text FROM pg_proc WHERE proname = 'try_parse_conversation_log_jsonb'`).Scan(&parallelSafety))
	require.Equal(t, "u", parallelSafety)

	requestID := "conversation-preview-" + uuid.NewString()
	body := `{"messages":[` +
		`{"role":"user","content":"` + strings.Repeat("old context ", 400) + `\u0000"},` +
		`{"role":"assistant","content":"reply"},` +
		`{"role":"user","content":[{"type":"text","text":"latest user message ` + strings.Repeat("context ", 400) + `"},{"type":"text","text":"final instruction"}]},` +
		`{"role":"user","content":[{"type":"tool_result","content":"tool output"}]}]}`
	require.Greater(t, len(body), 2048)
	require.True(t, gjson.Valid(body))

	_, err := integrationDB.ExecContext(context.Background(), `
INSERT INTO conversation_logs (request_id, platform, request_body, response_body)
VALUES ($1, 'anthropic', $2, 'large response')`, requestID, body)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM conversation_logs WHERE request_id = $1", requestID)
	})

	repo := NewConversationLogRepository(integrationDB)
	items, _, err := repo.List(context.Background(), pagination.PaginationParams{Page: 1, PageSize: 20}, service.ConversationLogFilters{RequestID: requestID})
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, "latest user message "+strings.Repeat("context ", 400)+"\nfinal instruction", gjson.Get(items[0].RequestBody, "input").String())
	require.Empty(t, items[0].ResponseBody)

	for name, incompatibleBody := range map[string]string{
		"surrogate": `{"messages":[{"role":"user","content":"\uD800"}]}`,
		"number":    `{"metadata":{"huge":1e1000000},"messages":[{"role":"user","content":"valid input"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			edgeRequestID := requestID + "-" + name
			_, err := integrationDB.ExecContext(context.Background(), `
INSERT INTO conversation_logs (request_id, platform, request_body)
VALUES ($1, 'openai', $2)`, edgeRequestID, incompatibleBody)
			require.NoError(t, err)
			t.Cleanup(func() {
				_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM conversation_logs WHERE request_id = $1", edgeRequestID)
			})

			items, _, err := repo.List(context.Background(), pagination.PaginationParams{Page: 1, PageSize: 20}, service.ConversationLogFilters{RequestID: edgeRequestID})
			require.NoError(t, err)
			require.Len(t, items, 1)
			require.Empty(t, items[0].RequestBody)
		})
	}
}
