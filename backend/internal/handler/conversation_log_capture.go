package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

type conversationLogLiveContextKey struct{}

type conversationResponseCapture struct {
	gin.ResponseWriter
	body        []byte
	truncated   bool
	context     *gin.Context
	service     *service.ConversationLogService
	requestBody []byte
	liveOnce    sync.Once
}

func startConversationResponseCapture(c *gin.Context, svc *service.ConversationLogService, requestBody ...[]byte) *conversationResponseCapture {
	if c == nil || svc == nil || !svc.Enabled() || !svc.StoreResponse() {
		return nil
	}
	body := []byte(nil)
	if len(requestBody) > 0 {
		body = append(body, requestBody[0]...)
	}
	capture := &conversationResponseCapture{
		ResponseWriter: c.Writer,
		body:           make([]byte, 0, 4096),
		context:        c,
		service:        svc,
		requestBody:    body,
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
	w.publishLiveStart()
	w.body = append(w.body, data...)
}

func (w *conversationResponseCapture) captureString(s string) {
	if w == nil || s == "" {
		return
	}
	w.publishLiveStart()
	w.body = append(w.body, s...)
}

func (w *conversationResponseCapture) publishLiveStart() {
	if w == nil || w.context == nil || w.service == nil || len(w.requestBody) == 0 {
		return
	}
	w.liveOnce.Do(func() {
		if status := w.Status(); status < http.StatusOK || status >= http.StatusMultipleChoices {
			return
		}
		apiKey, ok := middleware2.GetAPIKeyFromContext(w.context)
		if !ok || apiKey == nil {
			return
		}
		liveID := fmt.Sprintf("live-%d", time.Now().UnixNano())
		w.context.Request = w.context.Request.WithContext(context.WithValue(w.context.Request.Context(), conversationLogLiveContextKey{}, liveID))
		requestBody, requestTruncated := w.service.CaptureRequestBody(w.requestBody)
		model := strings.TrimSpace(gjson.GetBytes(w.requestBody, "model").String())
		stream := gjson.GetBytes(w.requestBody, "stream").Bool()
		log := &service.ConversationLog{
			LiveID:           liveID,
			UserID:           conversationLogUserID(apiKey),
			APIKeyID:         apiKey.ID,
			GroupID:          cloneInt64Ptr(apiKey.GroupID),
			InboundEndpoint:  GetInboundEndpoint(w.context),
			Model:            model,
			RequestedModel:   model,
			RequestType:      service.RequestTypeFromLegacy(stream, false),
			Stream:           stream,
			StatusCode:       http.StatusProcessing,
			RequestHash:      service.HashUsageRequestPayload(w.requestBody),
			RequestBody:      requestBody,
			RequestTruncated: requestTruncated,
			CreatedAt:        time.Now(),
		}
		populateConversationLogDisplayFields(log, apiKey, nil)
		w.service.PublishLive(log)
	})
}

type conversationLogBaseInput struct {
	Body                      []byte
	Capture                   *conversationResponseCapture
	APIKey                    *service.APIKey
	Account                   *service.Account
	InboundEndpoint           string
	UpstreamEndpoint          string
	StatusCode                int
	RequestHash               string
	ResponseBodyOverride      *string
	ResponseTruncatedOverride bool
}

func conversationLogLiveID(ctx context.Context) string {
	liveID, _ := ctx.Value(conversationLogLiveContextKey{}).(string)
	return liveID
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
		LiveID:            conversationLogLiveID(parent),
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
	populateConversationLogDisplayFields(log, base.APIKey, base.Account)
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
	if base.ResponseBodyOverride != nil {
		responseBody = *base.ResponseBodyOverride
		responseTruncated = base.ResponseTruncatedOverride
	}
	durationMs := durationMsPtr(result.Duration)
	requestType := service.RequestTypeFromLegacy(result.Stream, result.OpenAIWSMode)
	log := &service.ConversationLog{
		LiveID:            conversationLogLiveID(parent),
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
	populateConversationLogDisplayFields(log, base.APIKey, base.Account)
	svc.Submit(log)
	_ = parent
}

func populateConversationLogDisplayFields(log *service.ConversationLog, apiKey *service.APIKey, account *service.Account) {
	if log == nil {
		return
	}
	if apiKey != nil {
		log.APIKeyName = apiKey.Name
		if apiKey.User != nil {
			log.UserEmail = apiKey.User.Email
		}
		if apiKey.Group != nil {
			log.GroupName = apiKey.Group.Name
		}
	}
	if account != nil {
		log.AccountName = account.Name
	}
}

type conversationWSTurnCapture struct {
	requestBody    []byte
	requestedModel string
	responseFrames [][]byte
}

type conversationWSLogCapture struct {
	svc   *service.ConversationLogService
	mu    sync.Mutex
	turns map[int]*conversationWSTurnCapture
}

func openAIWSConversationLogRequestedModel(turnRequestedModel, capturedModel string, mapping service.ChannelMappingResult) string {
	capturedModel = strings.TrimSpace(capturedModel)
	if capturedModel != "" && (!mapping.Mapped || capturedModel != strings.TrimSpace(mapping.MappedModel)) {
		return capturedModel
	}
	return strings.TrimSpace(turnRequestedModel)
}

func newConversationWSLogCapture(svc *service.ConversationLogService) *conversationWSLogCapture {
	if svc == nil || !svc.Enabled() || (!svc.StoreRequest() && !svc.StoreResponse()) {
		return nil
	}
	return &conversationWSLogCapture{
		svc:   svc,
		turns: make(map[int]*conversationWSTurnCapture),
	}
}

func (c *conversationWSLogCapture) CaptureRequest(turn int, payload []byte, requestedModel string) {
	if c == nil || c.svc == nil || len(payload) == 0 {
		return
	}
	if turn <= 0 {
		turn = 1
	}
	storeRequest := c.svc.StoreRequest()
	c.mu.Lock()
	defer c.mu.Unlock()
	capture := c.getOrCreateLocked(turn)
	if storeRequest {
		capture.requestBody = append([]byte(nil), payload...)
	}
	capture.requestedModel = strings.TrimSpace(requestedModel)
	capture.responseFrames = nil
}

func (c *conversationWSLogCapture) CaptureResponse(turn int, payload []byte) {
	if c == nil || c.svc == nil || !c.svc.StoreResponse() || len(payload) == 0 {
		return
	}
	if turn <= 0 {
		turn = 1
	}
	frame := append([]byte(nil), payload...)
	c.mu.Lock()
	defer c.mu.Unlock()
	capture := c.getOrCreateLocked(turn)
	capture.responseFrames = append(capture.responseFrames, frame)
}

func (c *conversationWSLogCapture) Snapshot(turn int) ([]byte, string, string, bool) {
	if c == nil {
		return nil, "", "", false
	}
	if turn <= 0 {
		turn = 1
	}
	c.mu.Lock()
	capture := c.turns[turn]
	if capture == nil {
		c.mu.Unlock()
		return nil, "", "", false
	}
	requestBody := append([]byte(nil), capture.requestBody...)
	requestedModel := strings.TrimSpace(capture.requestedModel)
	frames := cloneConversationWSFrames(capture.responseFrames)
	c.mu.Unlock()

	return requestBody, requestedModel, conversationWSFramesJSON(frames), false
}

func (c *conversationWSLogCapture) SnapshotSession() ([]byte, string) {
	if c == nil {
		return nil, ""
	}
	c.mu.Lock()
	turns := make([]int, 0, len(c.turns))
	for turn := range c.turns {
		turns = append(turns, turn)
	}
	sort.Ints(turns)
	type sessionTurn struct {
		Turn     int             `json:"turn"`
		Request  json.RawMessage `json:"request,omitempty"`
		Response json.RawMessage `json:"response,omitempty"`
	}
	requestTurns := make([]sessionTurn, 0, len(turns))
	responseTurns := make([]sessionTurn, 0, len(turns))
	for _, turn := range turns {
		capture := c.turns[turn]
		if capture == nil {
			continue
		}
		if request := conversationWSJSONValue(capture.requestBody); request != nil {
			requestTurns = append(requestTurns, sessionTurn{Turn: turn, Request: request})
		}
		if response := conversationWSJSONValue([]byte(conversationWSFramesJSON(capture.responseFrames))); response != nil {
			responseTurns = append(responseTurns, sessionTurn{Turn: turn, Response: response})
		}
	}
	c.mu.Unlock()

	requestBody, _ := json.Marshal(struct {
		ConversationTurns []sessionTurn `json:"conversation_turns"`
	}{ConversationTurns: requestTurns})
	if !c.svc.StoreResponse() {
		return requestBody, ""
	}
	responseBody, _ := json.Marshal(struct {
		ConversationTurns []sessionTurn `json:"conversation_turns"`
	}{ConversationTurns: responseTurns})
	return requestBody, string(responseBody)
}

func (c *conversationWSLogCapture) Forget(turn int) {
	if c == nil {
		return
	}
	if turn <= 0 {
		turn = 1
	}
	c.mu.Lock()
	delete(c.turns, turn)
	c.mu.Unlock()
}

func (c *conversationWSLogCapture) getOrCreateLocked(turn int) *conversationWSTurnCapture {
	if c.turns == nil {
		c.turns = make(map[int]*conversationWSTurnCapture)
	}
	capture := c.turns[turn]
	if capture == nil {
		capture = &conversationWSTurnCapture{}
		c.turns[turn] = capture
	}
	return capture
}

func cloneConversationWSFrames(frames [][]byte) [][]byte {
	if len(frames) == 0 {
		return nil
	}
	cloned := make([][]byte, 0, len(frames))
	for _, frame := range frames {
		cloned = append(cloned, append([]byte(nil), frame...))
	}
	return cloned
}

func conversationWSFramesJSON(frames [][]byte) string {
	if len(frames) == 0 {
		return ""
	}
	out := make([]byte, 0, len(frames)*64)
	out = append(out, '[')
	for i, frame := range frames {
		if i > 0 {
			out = append(out, ',')
		}
		trimmed := bytes.TrimSpace(frame)
		if json.Valid(trimmed) {
			out = append(out, trimmed...)
			continue
		}
		encoded, err := json.Marshal(strings.ToValidUTF8(string(frame), "\uFFFD"))
		if err != nil {
			encoded = []byte(`""`)
		}
		out = append(out, encoded...)
	}
	out = append(out, ']')
	return string(out)
}

func conversationWSJSONValue(payload []byte) json.RawMessage {
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 {
		return nil
	}
	if json.Valid(trimmed) {
		return append(json.RawMessage(nil), trimmed...)
	}
	encoded, err := json.Marshal(strings.ToValidUTF8(string(trimmed), "\uFFFD"))
	if err != nil {
		return nil
	}
	return encoded
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
