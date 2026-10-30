package repository

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

// StreamConversationArchive uses one MVCC snapshot and a server-side cursor:
// neither concurrent inserts nor changes to filter matches affect the export.
// Each JSON object is the complete stored row, including both payload bodies.
func (r *conversationLogRepository) StreamConversationArchive(ctx context.Context, filters service.ConversationLogFilters, emit func(int64, json.RawMessage) error) error {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return fmt.Errorf("begin conversation snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	where, args := buildConversationLogWhere(filters)
	_, err = tx.ExecContext(ctx, `DECLARE conversation_archive NO SCROLL CURSOR FOR
SELECT cl.id, row_to_json(cl)::text FROM conversation_logs cl
LEFT JOIN users u ON u.id = cl.user_id
LEFT JOIN api_keys ak ON ak.id = cl.api_key_id
LEFT JOIN accounts a ON a.id = cl.account_id
LEFT JOIN groups g ON g.id = cl.group_id
`+where+` ORDER BY cl.id`, args...)
	if err != nil {
		return fmt.Errorf("declare conversation snapshot cursor: %w", err)
	}
	for {
		rows, err := tx.QueryContext(ctx, "FETCH FORWARD 32 FROM conversation_archive")
		if err != nil {
			return fmt.Errorf("fetch conversation snapshot: %w", err)
		}
		n := 0
		for rows.Next() {
			var id int64
			var body sql.RawBytes
			if err = rows.Scan(&id, &body); err != nil {
				_ = rows.Close()
				return err
			}
			if err = emit(id, json.RawMessage(body)); err != nil {
				_ = rows.Close()
				return err
			}
			n++
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return err
		}
		if n == 0 {
			break
		}
	}
	return tx.Commit()
}

// DeleteConversationArchiveIDs never re-evaluates filters: only the IDs that
// were written to the uploaded archive can be deleted. All batches commit as
// one transaction so a cleanup error leaves the source unchanged.
func (r *conversationLogRepository) DeleteConversationArchiveIDs(ctx context.Context, source io.Reader) (int64, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	var deleted int64
	ids := make([]int64, 0, 256)
	var encoded [8]byte
	for {
		ids = ids[:0]
		for len(ids) < cap(ids) {
			_, err := io.ReadFull(source, encoded[:])
			if err == io.EOF {
				break
			}
			if err != nil {
				return 0, fmt.Errorf("read archived conversation ID: %w", err)
			}
			ids = append(ids, int64(binary.LittleEndian.Uint64(encoded[:])))
		}
		if len(ids) == 0 {
			break
		}
		result, err := tx.ExecContext(ctx, "DELETE FROM conversation_logs WHERE id = ANY($1)", pq.Array(ids))
		if err != nil {
			return 0, fmt.Errorf("delete archived conversations: %w", err)
		}
		n, err := result.RowsAffected()
		if err != nil {
			return 0, err
		}
		deleted += n
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return deleted, nil
}
