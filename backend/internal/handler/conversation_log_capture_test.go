package handler

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type handlerConversationLogRepoStub struct{}

func (handlerConversationLogRepoStub) Create(ctx context.Context, log *service.ConversationLog) error {
	return nil
}

func (handlerConversationLogRepoStub) List(ctx context.Context, params pagination.PaginationParams, filters service.ConversationLogFilters) ([]service.ConversationLog, *pagination.PaginationResult, error) {
	return nil, &pagination.PaginationResult{Total: 0, Page: params.Page, PageSize: params.Limit(), Pages: 1}, nil
}

func (handlerConversationLogRepoStub) GetByID(ctx context.Context, id int64) (*service.ConversationLog, error) {
	return nil, service.ErrConversationLogNotFound
}

func TestConversationResponseCapture_DoesNotAlterResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	svc := service.NewConversationLogService(handlerConversationLogRepoStub{}, &config.Config{
		Gateway: config.GatewayConfig{
			ConversationLog: config.GatewayConversationLogConfig{
				Enabled:            true,
				WorkerCount:        1,
				QueueSize:          1,
				TaskTimeoutSeconds: 1,
				OverflowPolicy:     config.UsageRecordOverflowPolicySync,
				StoreRequest:       true,
				StoreResponse:      true,
				MaxRequestBytes:    5,
				MaxResponseBytes:   5,
			},
		},
	})
	t.Cleanup(svc.Stop)

	capture := startConversationResponseCapture(c, svc)
	require.NotNil(t, capture)

	_, err := c.Writer.WriteString("hello")
	require.NoError(t, err)
	_, err = c.Writer.Write([]byte(" world"))
	require.NoError(t, err)

	body, truncated := capture.Snapshot()
	require.Equal(t, "hello world", body)
	require.False(t, truncated)
	require.Equal(t, "hello world", rec.Body.String())
}

func TestConversationResponseCapture_RestoresNestedWriterBeforeOpsRelease(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	originalWriter := c.Writer
	opsWriter := acquireOpsCaptureWriter(originalWriter)
	c.Writer = opsWriter
	svc := service.NewConversationLogService(handlerConversationLogRepoStub{}, &config.Config{
		Gateway: config.GatewayConfig{
			ConversationLog: config.GatewayConversationLogConfig{
				Enabled:            true,
				WorkerCount:        1,
				QueueSize:          1,
				TaskTimeoutSeconds: 1,
				OverflowPolicy:     config.UsageRecordOverflowPolicySync,
				StoreResponse:      true,
				MaxResponseBytes:   5,
			},
		},
	})
	t.Cleanup(svc.Stop)

	capture := startConversationResponseCapture(c, svc)
	require.NotNil(t, capture)
	require.Same(t, capture, c.Writer)

	capture.Restore(c)
	require.Same(t, opsWriter, c.Writer)
	if c.Writer == opsWriter {
		c.Writer = originalWriter
	}
	releaseOpsCaptureWriter(opsWriter)

	require.NotPanics(t, func() {
		_ = c.Writer.Status()
		_ = c.Writer.Written()
	})
}

func TestConversationWSLogCapture_SnapshotsTurnAsJSONFrameArray(t *testing.T) {
	svc := service.NewConversationLogService(handlerConversationLogRepoStub{}, &config.Config{
		Gateway: config.GatewayConfig{
			ConversationLog: config.GatewayConversationLogConfig{
				Enabled:            true,
				WorkerCount:        1,
				QueueSize:          1,
				TaskTimeoutSeconds: 1,
				OverflowPolicy:     config.UsageRecordOverflowPolicySync,
				StoreRequest:       true,
				StoreResponse:      true,
			},
		},
	})
	t.Cleanup(svc.Stop)

	capture := newConversationWSLogCapture(svc)
	require.NotNil(t, capture)

	request := []byte(`{"type":"response.create","model":"gpt-4.1","input":"hello"}`)
	capture.CaptureRequest(2, request, "gpt-4.1")
	capture.CaptureResponse(2, []byte(`{"type":"response.created","response":{"id":"resp_1"}}`))
	capture.CaptureResponse(2, []byte(`not-json`))

	requestBody, requestedModel, responseBody, truncated := capture.Snapshot(2)
	require.Equal(t, string(request), string(requestBody))
	require.Equal(t, "gpt-4.1", requestedModel)
	require.False(t, truncated)

	var frames []any
	require.NoError(t, json.Unmarshal([]byte(responseBody), &frames))
	require.Len(t, frames, 2)
	first, ok := frames[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "response.created", first["type"])
	require.Equal(t, "not-json", frames[1])
}

func TestConversationWSLogCapture_RequestCaptureResetsRetriedTurnFrames(t *testing.T) {
	svc := service.NewConversationLogService(handlerConversationLogRepoStub{}, &config.Config{
		Gateway: config.GatewayConfig{
			ConversationLog: config.GatewayConversationLogConfig{
				Enabled:            true,
				WorkerCount:        1,
				QueueSize:          1,
				TaskTimeoutSeconds: 1,
				OverflowPolicy:     config.UsageRecordOverflowPolicySync,
				StoreRequest:       true,
				StoreResponse:      true,
			},
		},
	})
	t.Cleanup(svc.Stop)

	capture := newConversationWSLogCapture(svc)
	capture.CaptureRequest(1, []byte(`{"type":"response.create","model":"gpt-4.1","input":"first"}`), "gpt-4.1")
	capture.CaptureResponse(1, []byte(`{"type":"response.created","response":{"id":"stale"}}`))
	capture.CaptureRequest(1, []byte(`{"type":"response.create","model":"gpt-4.1","input":"retry"}`), "gpt-4.1")
	capture.CaptureResponse(1, []byte(`{"type":"response.created","response":{"id":"fresh"}}`))

	_, _, responseBody, _ := capture.Snapshot(1)
	var frames []map[string]any
	require.NoError(t, json.Unmarshal([]byte(responseBody), &frames))
	require.Len(t, frames, 1)
	response, ok := frames[0]["response"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "fresh", response["id"])
}
