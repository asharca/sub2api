//go:build integration

package repository

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestConversationArchiveSnapshotAndExactIDDeletion(t *testing.T) {
	ctx := context.Background()
	requestID := "conversation-archive-" + uuid.NewString()
	body := strings.Repeat("full body, not a preview; ", 300)
	for range 35 {
		_, err := integrationDB.ExecContext(ctx, `INSERT INTO conversation_logs(request_id, request_body, response_body) VALUES ($1, $2, $3)`, requestID, body, "complete response")
		require.NoError(t, err)
	}
	_, err := integrationDB.ExecContext(ctx, `INSERT INTO conversation_logs(request_id) VALUES ($1)`, requestID+"-nonmatch")
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM conversation_logs WHERE request_id = $1 OR request_id = $2`, requestID, requestID+"-nonmatch")
	})
	repo := NewConversationLogRepository(integrationDB).(service.ConversationArchiveRepository)
	var ids bytes.Buffer
	count := 0
	err = repo.StreamConversationArchive(ctx, service.ConversationLogFilters{RequestID: requestID}, func(id int64, raw json.RawMessage) error {
		if count == 0 {
			// This row matches the same filter but must not enter a later cursor
			// fetch or be deleted after the original snapshot is uploaded.
			_, err := integrationDB.ExecContext(ctx, `INSERT INTO conversation_logs(request_id, request_body) VALUES ($1, 'new concurrent row')`, requestID)
			if err != nil {
				return err
			}
		}
		var row map[string]any
		if err := json.Unmarshal(raw, &row); err != nil {
			return err
		}
		require.Equal(t, body, row["request_body"])
		require.Equal(t, "complete response", row["response_body"])
		count++
		return binary.Write(&ids, binary.LittleEndian, id)
	})
	require.NoError(t, err)
	require.Equal(t, 35, count)
	deleted, err := repo.DeleteConversationArchiveIDs(ctx, &ids)
	require.NoError(t, err)
	require.EqualValues(t, 35, deleted)
	var remaining int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT count(*) FROM conversation_logs WHERE request_id = $1`, requestID).Scan(&remaining))
	require.Equal(t, 1, remaining)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT count(*) FROM conversation_logs WHERE request_id = $1`, requestID+"-nonmatch").Scan(&remaining))
	require.Equal(t, 1, remaining)
}

func TestConversationArchiveCleanupRollsBackMalformedIDStream(t *testing.T) {
	ctx := context.Background()
	requestID := "conversation-archive-rollback-" + uuid.NewString()
	var ids bytes.Buffer
	for range 257 {
		var id int64
		require.NoError(t, integrationDB.QueryRowContext(ctx, `INSERT INTO conversation_logs(request_id) VALUES ($1) RETURNING id`, requestID).Scan(&id))
		require.NoError(t, binary.Write(&ids, binary.LittleEndian, id))
	}
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM conversation_logs WHERE request_id = $1`, requestID)
	})
	ids.WriteByte(1)
	repo := NewConversationLogRepository(integrationDB).(service.ConversationArchiveRepository)
	deleted, err := repo.DeleteConversationArchiveIDs(ctx, &ids)
	require.Error(t, err)
	require.Zero(t, deleted)
	var remaining int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT count(*) FROM conversation_logs WHERE request_id = $1`, requestID).Scan(&remaining))
	require.Equal(t, 257, remaining)
}
