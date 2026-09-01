//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestConversationLogRepositoryListAcceptsJSONBIncompatibleRequest(t *testing.T) {
	requestID := "conversation-preview-" + uuid.NewString()
	body := `{"messages":[{"role":"user","content":"hello\u0000world"}]}`

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
	require.Equal(t, body, items[0].RequestBody)
	require.Empty(t, items[0].ResponseBody)
}
