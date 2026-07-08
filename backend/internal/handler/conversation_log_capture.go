package handler

import (
	"context"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type conversationResponseCapture struct {
	gin.ResponseWriter
	body      []byte
	truncated bool
}

func startConversationResponseCapture(c *gin.Context, svc *service.ConversationLogService) *conversationResponseCapture {
	if c == nil || svc == nil || !svc.Enabled() || !svc.StoreResponse() {
		return nil
	}
	capture := &conversationResponseCapture{
		ResponseWriter: c.Writer,
		body:           make([]byte, 0, 4096),
	}
	c.Writer = capture
	return capture
}

func (w *conversationResponseCapture) Write(data []byte) (int, error) {
	w.capture(data)
	return w.ResponseWriter.Write(data)
}

func (w *conversationResponseCapture) WriteString(s string) (int, error) {
	w.captureString(s)
	return w.ResponseWriter.WriteString(s)
}

func (w *conversationResponseCapture) Restore(c *gin.Context) {
	if w == nil || c == nil {
		return
	}
	if c.Writer == w {
		c.Writer = w.ResponseWriter
	}
}

func (w *conversationResponseCapture) Snapshot() (string, bool) {
	if w == nil {
		return "", false
	}
	return strings.ToValidUTF8(string(w.body), "\uFFFD"), w.truncated
}

func (w *conversationResponseCapture) capture(data []byte) {
	if w == nil || len(data) == 0 {
		return
	}
	w.body = append(w.body, data...)
}

func (w *conversationResponseCapture) captureString(s string) {
	if w == nil || s == "" {
		return
	}
	w.body = append(w.body, s...)
}

type conversationLogBaseInput struct {
	Body             []byte
	Capture          *conversationResponseCapture
	APIKey           *service.APIKey
	Account          *service.Account
	InboundEndpoint  string
	UpstreamEndpoint string
	StatusCode       int
	RequestHash      string
}

func submitAnthropicConversationLog(
	parent context.Context,
	svc *service.ConversationLogService,
	base conversationLogBaseInput,
	result *service.ForwardResult,
) {
	if svc == nil || !svc.Enabled() || base.APIKey == nil || base.Account == nil || result == nil {
		return
	}
	requestBody, requestTruncated := svc.CaptureRequestBody(base.Body)
	responseBody, responseTruncated := base.Capture.Snapshot()
	durationMs := durationMsPtr(result.Duration)
	log := &service.ConversationLog{
		RequestID:         result.RequestID,
		UserID:            conversationLogUserID(base.APIKey),
		APIKeyID:          base.APIKey.ID,
		AccountID:         base.Account.ID,
		GroupID:           cloneInt64Ptr(base.APIKey.GroupID),
		Platform:          base.Account.Platform,
		InboundEndpoint:   base.InboundEndpoint,
		UpstreamEndpoint:  base.UpstreamEndpoint,
		Model:             result.Model,
		RequestedModel:    result.Model,
		UpstreamModel:     result.UpstreamModel,
		RequestType:       service.RequestTypeFromLegacy(result.Stream, false),
		Stream:            result.Stream,
		StatusCode:        base.StatusCode,
		DurationMs:        durationMs,
		FirstTokenMs:      cloneIntPtr(result.FirstTokenMs),
		InputTokens:       result.Usage.InputTokens,
		OutputTokens:      result.Usage.OutputTokens,
		CacheReadTokens:   result.Usage.CacheReadInputTokens,
		CacheCreateTokens: result.Usage.CacheCreationInputTokens,
		RequestHash:       base.RequestHash,
		RequestBody:       requestBody,
		ResponseBody:      responseBody,
		RequestTruncated:  requestTruncated,
		ResponseTruncated: responseTruncated,
	}
	svc.Submit(log)
	_ = parent
}

func submitOpenAIConversationLog(
	parent context.Context,
	svc *service.ConversationLogService,
	base conversationLogBaseInput,
	requestedModel string,
	result *service.OpenAIForwardResult,
) {
	if svc == nil || !svc.Enabled() || base.APIKey == nil || base.Account == nil || result == nil {
		return
	}
	requestBody, requestTruncated := svc.CaptureRequestBody(base.Body)
	responseBody, responseTruncated := base.Capture.Snapshot()
	durationMs := durationMsPtr(result.Duration)
	requestType := service.RequestTypeFromLegacy(result.Stream, result.OpenAIWSMode)
	log := &service.ConversationLog{
		RequestID:         result.RequestID,
		ResponseID:        result.ResponseID,
		UserID:            conversationLogUserID(base.APIKey),
		APIKeyID:          base.APIKey.ID,
		AccountID:         base.Account.ID,
		GroupID:           cloneInt64Ptr(base.APIKey.GroupID),
		Platform:          base.Account.Platform,
		InboundEndpoint:   base.InboundEndpoint,
		UpstreamEndpoint:  base.UpstreamEndpoint,
		Model:             result.Model,
		RequestedModel:    requestedModel,
		UpstreamModel:     result.UpstreamModel,
		RequestType:       requestType,
		Stream:            result.Stream,
		OpenAIWSMode:      result.OpenAIWSMode,
		StatusCode:        base.StatusCode,
		DurationMs:        durationMs,
		FirstTokenMs:      cloneIntPtr(result.FirstTokenMs),
		InputTokens:       result.Usage.InputTokens,
		OutputTokens:      result.Usage.OutputTokens,
		CacheReadTokens:   result.Usage.CacheReadInputTokens,
		CacheCreateTokens: result.Usage.CacheCreationInputTokens,
		RequestHash:       base.RequestHash,
		RequestBody:       requestBody,
		ResponseBody:      responseBody,
		RequestTruncated:  requestTruncated,
		ResponseTruncated: responseTruncated,
	}
	svc.Submit(log)
	_ = parent
}

func conversationLogStatus(c *gin.Context) int {
	if c == nil || c.Writer == nil {
		return 0
	}
	return c.Writer.Status()
}

func conversationLogUserID(apiKey *service.APIKey) int64 {
	if apiKey == nil {
		return 0
	}
	if apiKey.User != nil {
		return apiKey.User.ID
	}
	return apiKey.UserID
}

func durationMsPtr(d time.Duration) *int {
	if d <= 0 {
		return nil
	}
	v := int(d.Milliseconds())
	return &v
}

func cloneIntPtr(src *int) *int {
	if src == nil {
		return nil
	}
	v := *src
	return &v
}

func cloneInt64Ptr(src *int64) *int64 {
	if src == nil {
		return nil
	}
	v := *src
	return &v
}
