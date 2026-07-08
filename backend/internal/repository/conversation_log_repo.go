package repository

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type conversationLogRepository struct {
	db *sql.DB
}

func NewConversationLogRepository(db *sql.DB) service.ConversationLogRepository {
	return &conversationLogRepository{db: db}
}

func (r *conversationLogRepository) Create(ctx context.Context, log *service.ConversationLog) error {
	if r == nil || r.db == nil || log == nil {
		return nil
	}
	var groupID any
	if log.GroupID != nil {
		groupID = *log.GroupID
	}
	_, err := r.db.ExecContext(ctx, `
INSERT INTO conversation_logs (
    request_id, response_id, user_id, api_key_id, account_id, group_id,
    platform, inbound_endpoint, upstream_endpoint, model, requested_model, upstream_model,
    request_type, stream, openai_ws_mode, status_code, duration_ms, first_token_ms,
    input_tokens, output_tokens, cache_read_tokens, cache_create_tokens,
    request_hash, request_body, response_body, request_truncated, response_truncated, queue_delay_ms
) VALUES (
    $1, $2, $3, $4, $5, $6,
    $7, $8, $9, $10, $11, $12,
    $13, $14, $15, $16, $17, $18,
    $19, $20, $21, $22,
    $23, $24, $25, $26, $27, $28
)`,
		log.RequestID, log.ResponseID, log.UserID, log.APIKeyID, log.AccountID, groupID,
		log.Platform, log.InboundEndpoint, log.UpstreamEndpoint, log.Model, log.RequestedModel, log.UpstreamModel,
		int16(log.RequestType.Normalize()), log.Stream, log.OpenAIWSMode, log.StatusCode, nullableIntPtr(log.DurationMs), nullableIntPtr(log.FirstTokenMs),
		log.InputTokens, log.OutputTokens, log.CacheReadTokens, log.CacheCreateTokens,
		log.RequestHash, log.RequestBody, log.ResponseBody, log.RequestTruncated, log.ResponseTruncated, nullableIntPtr(log.QueueDelayMs),
	)
	if err != nil {
		return fmt.Errorf("insert conversation log: %w", err)
	}
	return nil
}

func (r *conversationLogRepository) List(ctx context.Context, params pagination.PaginationParams, filters service.ConversationLogFilters) ([]service.ConversationLog, *pagination.PaginationResult, error) {
	if r == nil || r.db == nil {
		return []service.ConversationLog{}, emptyConversationLogPagination(params), nil
	}
	where, args := buildConversationLogWhere(filters)
	var total int64
	countSQL := `
SELECT COUNT(*)
FROM conversation_logs cl
LEFT JOIN users u ON u.id = cl.user_id
LEFT JOIN api_keys ak ON ak.id = cl.api_key_id
LEFT JOIN accounts a ON a.id = cl.account_id
LEFT JOIN groups g ON g.id = cl.group_id
` + where
	if err := r.db.QueryRowContext(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, nil, fmt.Errorf("count conversation logs: %w", err)
	}

	page := params.Page
	if page <= 0 {
		page = 1
	}
	pageSize := params.Limit()
	sortBy := normalizeConversationLogSortBy(params.SortBy)
	sortOrder := params.NormalizedSortOrder(pagination.SortOrderDesc)
	listArgs := append([]any{}, args...)
	listArgs = append(listArgs, pageSize, (page-1)*pageSize)
	query := fmt.Sprintf(`
SELECT
    cl.id, cl.request_id, cl.response_id, cl.user_id, cl.api_key_id, cl.account_id, cl.group_id,
    cl.platform, cl.inbound_endpoint, cl.upstream_endpoint, cl.model, cl.requested_model, cl.upstream_model,
    cl.request_type, cl.stream, cl.openai_ws_mode, cl.status_code, cl.duration_ms, cl.first_token_ms,
    cl.input_tokens, cl.output_tokens, cl.cache_read_tokens, cl.cache_create_tokens,
    cl.request_hash, LEFT(cl.request_body, 2048), LEFT(cl.response_body, 2048),
    cl.request_truncated, cl.response_truncated, cl.queue_delay_ms, cl.created_at,
    COALESCE(u.email, ''), COALESCE(ak.name, ''), COALESCE(a.name, ''), COALESCE(g.name, '')
FROM conversation_logs cl
LEFT JOIN users u ON u.id = cl.user_id
LEFT JOIN api_keys ak ON ak.id = cl.api_key_id
LEFT JOIN accounts a ON a.id = cl.account_id
LEFT JOIN groups g ON g.id = cl.group_id
%s
ORDER BY %s %s, cl.id DESC
LIMIT $%d OFFSET $%d`, where, sortBy, sortOrder, len(listArgs)-1, len(listArgs))
	rows, err := r.db.QueryContext(ctx, query, listArgs...)
	if err != nil {
		return nil, nil, fmt.Errorf("list conversation logs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]service.ConversationLog, 0, pageSize)
	for rows.Next() {
		item, err := scanConversationLog(rows)
		if err != nil {
			return nil, nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate conversation logs: %w", err)
	}
	pages := int(math.Ceil(float64(total) / float64(pageSize)))
	if pages < 1 {
		pages = 1
	}
	return items, &pagination.PaginationResult{Total: total, Page: page, PageSize: pageSize, Pages: pages}, nil
}

func (r *conversationLogRepository) GetByID(ctx context.Context, id int64) (*service.ConversationLog, error) {
	if r == nil || r.db == nil || id <= 0 {
		return nil, service.ErrConversationLogNotFound
	}
	rows, err := r.db.QueryContext(ctx, `
SELECT
    cl.id, cl.request_id, cl.response_id, cl.user_id, cl.api_key_id, cl.account_id, cl.group_id,
    cl.platform, cl.inbound_endpoint, cl.upstream_endpoint, cl.model, cl.requested_model, cl.upstream_model,
    cl.request_type, cl.stream, cl.openai_ws_mode, cl.status_code, cl.duration_ms, cl.first_token_ms,
    cl.input_tokens, cl.output_tokens, cl.cache_read_tokens, cl.cache_create_tokens,
    cl.request_hash, cl.request_body, cl.response_body,
    cl.request_truncated, cl.response_truncated, cl.queue_delay_ms, cl.created_at,
    COALESCE(u.email, ''), COALESCE(ak.name, ''), COALESCE(a.name, ''), COALESCE(g.name, '')
FROM conversation_logs cl
LEFT JOIN users u ON u.id = cl.user_id
LEFT JOIN api_keys ak ON ak.id = cl.api_key_id
LEFT JOIN accounts a ON a.id = cl.account_id
LEFT JOIN groups g ON g.id = cl.group_id
WHERE cl.id = $1`, id)
	if err != nil {
		return nil, fmt.Errorf("get conversation log: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return nil, service.ErrConversationLogNotFound
	}
	item, err := scanConversationLog(rows)
	if err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate conversation log: %w", err)
	}
	return &item, nil
}

type conversationLogScanner interface {
	Scan(dest ...any) error
}

func scanConversationLog(row conversationLogScanner) (service.ConversationLog, error) {
	var item service.ConversationLog
	var userID, apiKeyID, accountID, groupID sql.NullInt64
	var durationMs, firstTokenMs, queueDelayMs sql.NullInt64
	var requestType int16
	if err := row.Scan(
		&item.ID, &item.RequestID, &item.ResponseID, &userID, &apiKeyID, &accountID, &groupID,
		&item.Platform, &item.InboundEndpoint, &item.UpstreamEndpoint, &item.Model, &item.RequestedModel, &item.UpstreamModel,
		&requestType, &item.Stream, &item.OpenAIWSMode, &item.StatusCode, &durationMs, &firstTokenMs,
		&item.InputTokens, &item.OutputTokens, &item.CacheReadTokens, &item.CacheCreateTokens,
		&item.RequestHash, &item.RequestBody, &item.ResponseBody,
		&item.RequestTruncated, &item.ResponseTruncated, &queueDelayMs, &item.CreatedAt,
		&item.UserEmail, &item.APIKeyName, &item.AccountName, &item.GroupName,
	); err != nil {
		return item, fmt.Errorf("scan conversation log: %w", err)
	}
	item.UserID = nullInt64Value(userID)
	item.APIKeyID = nullInt64Value(apiKeyID)
	item.AccountID = nullInt64Value(accountID)
	if groupID.Valid {
		v := groupID.Int64
		item.GroupID = &v
	}
	item.RequestType = service.RequestTypeFromInt16(requestType)
	item.DurationMs = nullIntPtr(durationMs)
	item.FirstTokenMs = nullIntPtr(firstTokenMs)
	item.QueueDelayMs = nullIntPtr(queueDelayMs)
	return item, nil
}

func buildConversationLogWhere(filters service.ConversationLogFilters) (string, []any) {
	var clauses []string
	var args []any
	add := func(clause string, value any) {
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf(clause, len(args)))
	}
	addRepeated := func(clause string, value any, count int) {
		args = append(args, value)
		idx := len(args)
		positions := make([]any, count)
		for i := range positions {
			positions[i] = idx
		}
		clauses = append(clauses, fmt.Sprintf(clause, positions...))
	}
	if filters.UserID > 0 {
		add("cl.user_id = $%d", filters.UserID)
	}
	if filters.APIKeyID > 0 {
		add("cl.api_key_id = $%d", filters.APIKeyID)
	}
	if filters.AccountID > 0 {
		add("cl.account_id = $%d", filters.AccountID)
	}
	if filters.GroupID > 0 {
		add("cl.group_id = $%d", filters.GroupID)
	}
	if strings.TrimSpace(filters.Platform) != "" {
		add("cl.platform = $%d", strings.TrimSpace(filters.Platform))
	}
	if strings.TrimSpace(filters.Model) != "" {
		addRepeated("(cl.model ILIKE $%d OR cl.requested_model ILIKE $%d OR cl.upstream_model ILIKE $%d)", like(filters.Model), 3)
	}
	if strings.TrimSpace(filters.RequestID) != "" {
		add("cl.request_id = $%d", strings.TrimSpace(filters.RequestID))
	}
	if strings.TrimSpace(filters.ResponseID) != "" {
		add("cl.response_id = $%d", strings.TrimSpace(filters.ResponseID))
	}
	if filters.RequestType != nil {
		add("cl.request_type = $%d", *filters.RequestType)
	}
	if filters.Stream != nil {
		add("cl.stream = $%d", *filters.Stream)
	}
	if filters.StartTime != nil {
		add("cl.created_at >= $%d", *filters.StartTime)
	}
	if filters.EndTime != nil {
		add("cl.created_at < $%d", *filters.EndTime)
	}
	if strings.TrimSpace(filters.Search) != "" {
		addRepeated(`(
cl.request_id ILIKE $%d OR cl.response_id ILIKE $%d OR cl.model ILIKE $%d OR
cl.requested_model ILIKE $%d OR cl.upstream_model ILIKE $%d OR cl.request_hash ILIKE $%d OR
u.email ILIKE $%d OR ak.name ILIKE $%d OR a.name ILIKE $%d
)`, like(filters.Search), 9)
	}
	if len(clauses) == 0 {
		return "", args
	}
	return "WHERE " + strings.Join(clauses, " AND "), args
}

func normalizeConversationLogSortBy(sortBy string) string {
	switch strings.ToLower(strings.TrimSpace(sortBy)) {
	case "duration_ms":
		return "cl.duration_ms"
	case "status_code":
		return "cl.status_code"
	case "input_tokens":
		return "cl.input_tokens"
	case "output_tokens":
		return "cl.output_tokens"
	case "queue_delay_ms":
		return "cl.queue_delay_ms"
	default:
		return "cl.created_at"
	}
}

func emptyConversationLogPagination(params pagination.PaginationParams) *pagination.PaginationResult {
	page := params.Page
	if page <= 0 {
		page = 1
	}
	return &pagination.PaginationResult{Total: 0, Page: page, PageSize: params.Limit(), Pages: 1}
}

func like(value string) string {
	return "%" + strings.TrimSpace(value) + "%"
}

func nullInt64Value(v sql.NullInt64) int64 {
	if !v.Valid {
		return 0
	}
	return v.Int64
}

func nullIntPtr(v sql.NullInt64) *int {
	if !v.Valid {
		return nil
	}
	iv := int(v.Int64)
	return &iv
}
