package handler

import (
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type ConversationLogHandler struct {
	conversationLogService *service.ConversationLogService
}

func NewConversationLogHandler(conversationLogService *service.ConversationLogService) *ConversationLogHandler {
	return &ConversationLogHandler{conversationLogService: conversationLogService}
}

type userConversationLogResponse struct {
	ID                int64  `json:"id"`
	RequestID         string `json:"request_id"`
	ResponseID        string `json:"response_id"`
	UserID            int64  `json:"user_id"`
	UserEmail         string `json:"user_email"`
	APIKeyID          int64  `json:"api_key_id"`
	APIKeyName        string `json:"api_key_name"`
	AccountID         int64  `json:"account_id"`
	AccountName       string `json:"account_name"`
	GroupID           *int64 `json:"group_id"`
	GroupName         string `json:"group_name"`
	Platform          string `json:"platform"`
	InboundEndpoint   string `json:"inbound_endpoint"`
	UpstreamEndpoint  string `json:"upstream_endpoint"`
	Model             string `json:"model"`
	RequestedModel    string `json:"requested_model"`
	UpstreamModel     string `json:"upstream_model"`
	RequestType       string `json:"request_type"`
	Stream            bool   `json:"stream"`
	OpenAIWSMode      bool   `json:"openai_ws_mode"`
	StatusCode        int    `json:"status_code"`
	DurationMs        *int   `json:"duration_ms"`
	FirstTokenMs      *int   `json:"first_token_ms"`
	InputTokens       int    `json:"input_tokens"`
	OutputTokens      int    `json:"output_tokens"`
	CacheReadTokens   int    `json:"cache_read_tokens"`
	CacheCreateTokens int    `json:"cache_create_tokens"`
	RequestHash       string `json:"request_hash"`
	RequestBody       string `json:"request_body"`
	ResponseBody      string `json:"response_body"`
	RequestTruncated  bool   `json:"request_truncated"`
	ResponseTruncated bool   `json:"response_truncated"`
	QueueDelayMs      *int   `json:"queue_delay_ms"`
	CreatedAt         string `json:"created_at"`
	TotalTokens       int    `json:"total_tokens"`
}

func (h *ConversationLogHandler) ListMine(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	page, pageSize := response.ParsePagination(c)
	filters, ok := parseUserConversationLogFilters(c)
	if !ok {
		return
	}
	filters.UserID = subject.UserID

	params := pagination.PaginationParams{
		Page:      page,
		PageSize:  pageSize,
		SortBy:    c.DefaultQuery("sort_by", "created_at"),
		SortOrder: c.DefaultQuery("sort_order", "desc"),
	}
	items, result, err := h.conversationLogService.List(c.Request.Context(), params, filters)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	out := make([]userConversationLogResponse, 0, len(items))
	for i := range items {
		out = append(out, conversationLogToUserResponse(&items[i]))
	}
	response.Paginated(c, out, result.Total, result.Page, result.PageSize)
}

func (h *ConversationLogHandler) GetMineByID(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid conversation log id")
		return
	}
	item, err := h.conversationLogService.GetByID(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if item.UserID != subject.UserID {
		response.Forbidden(c, "Not authorized to access this conversation log")
		return
	}
	response.Success(c, conversationLogToUserResponse(item))
}

func parseUserConversationLogFilters(c *gin.Context) (service.ConversationLogFilters, bool) {
	var filters service.ConversationLogFilters
	filters.Search = strings.TrimSpace(c.Query("q"))
	filters.Platform = strings.TrimSpace(c.Query("platform"))
	filters.Model = strings.TrimSpace(c.Query("model"))
	filters.RequestID = strings.TrimSpace(c.Query("request_id"))
	filters.ResponseID = strings.TrimSpace(c.Query("response_id"))

	var ok bool
	if filters.APIKeyID, ok = parseOptionalConversationLogInt64Query(c, "api_key_id"); !ok {
		return filters, false
	}
	if filters.GroupID, ok = parseOptionalConversationLogInt64Query(c, "group_id"); !ok {
		return filters, false
	}
	if requestTypeStr := strings.TrimSpace(c.Query("request_type")); requestTypeStr != "" {
		parsed, err := service.ParseUsageRequestType(requestTypeStr)
		if err != nil {
			response.BadRequest(c, err.Error())
			return filters, false
		}
		value := int16(parsed)
		filters.RequestType = &value
	}
	if streamStr := strings.TrimSpace(c.Query("stream")); streamStr != "" {
		parsed, err := strconv.ParseBool(streamStr)
		if err != nil {
			response.BadRequest(c, "Invalid stream value, use true or false")
			return filters, false
		}
		filters.Stream = &parsed
	}
	userTZ := c.Query("timezone")
	if startDateStr := strings.TrimSpace(c.Query("start_date")); startDateStr != "" {
		t, err := timezone.ParseInUserLocation("2006-01-02", startDateStr, userTZ)
		if err != nil {
			response.BadRequest(c, "Invalid start_date format, use YYYY-MM-DD")
			return filters, false
		}
		filters.StartTime = &t
	}
	if endDateStr := strings.TrimSpace(c.Query("end_date")); endDateStr != "" {
		t, err := timezone.ParseInUserLocation("2006-01-02", endDateStr, userTZ)
		if err != nil {
			response.BadRequest(c, "Invalid end_date format, use YYYY-MM-DD")
			return filters, false
		}
		t = t.AddDate(0, 0, 1)
		filters.EndTime = &t
	}
	return filters, true
}

func parseOptionalConversationLogInt64Query(c *gin.Context, name string) (int64, bool) {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return 0, true
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		response.BadRequest(c, "Invalid "+name)
		return 0, false
	}
	return value, true
}

func conversationLogToUserResponse(log *service.ConversationLog) userConversationLogResponse {
	if log == nil {
		return userConversationLogResponse{}
	}
	return userConversationLogResponse{
		ID:                log.ID,
		RequestID:         log.RequestID,
		ResponseID:        log.ResponseID,
		UserID:            log.UserID,
		APIKeyID:          log.APIKeyID,
		APIKeyName:        log.APIKeyName,
		GroupID:           cloneConversationLogInt64Ptr(log.GroupID),
		GroupName:         log.GroupName,
		Platform:          log.Platform,
		InboundEndpoint:   log.InboundEndpoint,
		Model:             log.Model,
		RequestedModel:    log.RequestedModel,
		UpstreamModel:     log.UpstreamModel,
		RequestType:       log.RequestType.String(),
		Stream:            log.Stream,
		OpenAIWSMode:      log.OpenAIWSMode,
		StatusCode:        log.StatusCode,
		DurationMs:        cloneConversationLogIntPtr(log.DurationMs),
		FirstTokenMs:      cloneConversationLogIntPtr(log.FirstTokenMs),
		InputTokens:       log.InputTokens,
		OutputTokens:      log.OutputTokens,
		CacheReadTokens:   log.CacheReadTokens,
		CacheCreateTokens: log.CacheCreateTokens,
		RequestBody:       log.RequestBody,
		ResponseBody:      log.ResponseBody,
		RequestTruncated:  log.RequestTruncated,
		ResponseTruncated: log.ResponseTruncated,
		CreatedAt:         log.CreatedAt.Format(time.RFC3339),
		TotalTokens:       log.InputTokens + log.OutputTokens + log.CacheReadTokens + log.CacheCreateTokens,
	}
}

func cloneConversationLogIntPtr(src *int) *int {
	if src == nil {
		return nil
	}
	v := *src
	return &v
}

func cloneConversationLogInt64Ptr(src *int64) *int64 {
	if src == nil {
		return nil
	}
	v := *src
	return &v
}
