package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCyberAllowlistedUserBypassesExistingBlocksAndContinuesWebSocket(t *testing.T) {
	t.Run("existing websocket block", func(t *testing.T) { runCyberAllowlistedUserWebSocket(t, true) })
	t.Run("no new websocket block", func(t *testing.T) { runCyberAllowlistedUserWebSocket(t, false) })
}

func runCyberAllowlistedUserWebSocket(t *testing.T, existingWSBlock bool) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		for _, response := range []string{
			`{"type":"response.failed","response":{"id":"resp_cyber","model":"gpt-5.1","error":{"code":"cyber_policy","message":"blocked by upstream"}}}`,
			`{"type":"response.completed","response":{"id":"resp_ok","model":"gpt-5.1","usage":{"input_tokens":1,"output_tokens":1}}}`,
		} {
			if _, _, err := conn.Read(ctx); err != nil {
				return
			}
			if err := conn.Write(ctx, coderws.MessageText, []byte(response)); err != nil {
				return
			}
		}
		// Let the downstream close so the completed frame can drain normally.
		_, _, _ = conn.Read(ctx)
	}))
	defer upstream.Close()
	harness := newOpenAIWSPassthroughHandlerHarness(t, upstream.URL, map[string]string{
		service.SettingKeyCyberPolicyUserAllowlist: "1751",
	})
	payload := []byte(`{"type":"response.create","model":"gpt-5.1","prompt_cache_key":"trusted-session","input":"test"}`)
	// Exercise HTTP bypass with either the same blocked WS identity or a
	// separate identity, leaving the WS identity free to test new-block suppression.
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/openai/v1/responses", strings.NewReader(string(payload)))
	c.Request.Header.Set("session_id", "trusted-http-session")
	if existingWSBlock {
		c.Request.Header.Set("session_id", "ws-test-session")
	}
	explicitKey := service.CyberSessionExplicitBlockKey(harness.apiKey.ID, c, payload)
	require.NotEmpty(t, explicitKey)
	store, ok := harness.gatewayCache.(service.CyberSessionBlockStore)
	require.True(t, ok)
	require.NoError(t, store.SetCyberSessionBlocked(context.Background(), "", []string{explicitKey}, time.Minute))
	for _, format := range []cyberSessionBlockFormat{cyberBlockFormatResponses, cyberBlockFormatChat, cyberBlockFormatAnthropic} {
		require.False(t, harness.handler.rejectIfCyberSessionBlocked(c, harness.apiKey, payload, "gpt-5.1", format))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, harness.clientConn.Write(ctx, coderws.MessageText, payload))
	_, event, err := harness.clientConn.Read(ctx)
	require.NoError(t, err)
	require.Equal(t, "cyber_policy", gjson.GetBytes(event, "response.error.code").String())
	require.Eventually(t, func() bool {
		logs := harness.moderationRepo.logSnapshot()
		return len(logs) == 1 && logs[0].Mode == service.ContentModerationModeCyberLogOnly
	}, 3*time.Second, 10*time.Millisecond)
	wsKeyCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	wsKeyCtx.Request = httptest.NewRequest(http.MethodPost, "/openai/v1/responses", strings.NewReader(string(payload)))
	wsKeyCtx.Request.Header.Set("session_id", "ws-test-session")
	wsBlockKey := service.CyberSessionExplicitBlockKey(harness.apiKey.ID, wsKeyCtx, payload)
	require.NotEmpty(t, wsBlockKey)
	matched, err := store.FindCyberSessionBlocked(ctx, []string{wsBlockKey})
	require.NoError(t, err)
	if existingWSBlock {
		require.Equal(t, wsBlockKey, matched, "the trusted websocket must bypass its existing block")
	} else {
		require.Empty(t, matched, "the cyber response must not write a new session block")
	}
	require.NoError(t, harness.clientConn.Write(ctx, coderws.MessageText, payload))
	_, event, err = harness.clientConn.Read(ctx)
	require.NoError(t, err)
	require.Equal(t, "response.completed", gjson.GetBytes(event, "type").String())
	_ = harness.clientConn.CloseNow()
	select {
	case <-harness.handlerDone:
	case <-ctx.Done():
		t.Fatal("handler did not exit")
	}
}
