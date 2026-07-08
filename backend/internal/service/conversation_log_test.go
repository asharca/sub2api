package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type conversationLogRepoStub struct {
	mu   sync.Mutex
	logs []*ConversationLog
	ch   chan *ConversationLog
	err  error
}

func (r *conversationLogRepoStub) Create(ctx context.Context, log *ConversationLog) error {
	if r.err != nil {
		return r.err
	}
	cp := *log
	r.mu.Lock()
	r.logs = append(r.logs, &cp)
	r.mu.Unlock()
	if r.ch != nil {
		select {
		case r.ch <- &cp:
		default:
		}
	}
	return nil
}

func (r *conversationLogRepoStub) List(ctx context.Context, params pagination.PaginationParams, filters ConversationLogFilters) ([]ConversationLog, *pagination.PaginationResult, error) {
	return nil, &pagination.PaginationResult{Total: 0, Page: params.Page, PageSize: params.Limit(), Pages: 1}, nil
}

func (r *conversationLogRepoStub) GetByID(ctx context.Context, id int64) (*ConversationLog, error) {
	return nil, ErrConversationLogNotFound
}

func TestConversationLogService_SubmitEnqueued(t *testing.T) {
	repo := &conversationLogRepoStub{ch: make(chan *ConversationLog, 1)}
	svc := NewConversationLogService(repo, &config.Config{
		Gateway: config.GatewayConfig{
			ConversationLog: config.GatewayConversationLogConfig{
				Enabled:            true,
				WorkerCount:        1,
				QueueSize:          8,
				TaskTimeoutSeconds: 1,
				OverflowPolicy:     config.UsageRecordOverflowPolicySync,
				StoreRequest:       true,
				StoreResponse:      true,
				MaxRequestBytes:    8,
				MaxResponseBytes:   8,
			},
		},
	})
	t.Cleanup(svc.Stop)

	svc.Submit(&ConversationLog{RequestID: "req_1", UserID: 1})

	select {
	case got := <-repo.ch:
		require.Equal(t, "req_1", got.RequestID)
		require.NotNil(t, got.QueueDelayMs)
	case <-time.After(time.Second):
		t.Fatal("conversation log task not executed")
	}

	require.Eventually(t, func() bool {
		stats := svc.Stats()
		return stats.SubmittedTasks == 1 && stats.CompletedTasks == 1
	}, time.Second, 10*time.Millisecond)
}

func TestConversationLogService_CaptureRequestBody(t *testing.T) {
	svc := NewConversationLogService(nil, &config.Config{
		Gateway: config.GatewayConfig{
			ConversationLog: config.GatewayConversationLogConfig{
				StoreRequest:    true,
				MaxRequestBytes: 5,
			},
		},
	})

	body, truncated := svc.CaptureRequestBody([]byte("hello world"))
	require.Equal(t, "hello world", body)
	require.False(t, truncated)

	svc = NewConversationLogService(nil, &config.Config{
		Gateway: config.GatewayConfig{
			ConversationLog: config.GatewayConversationLogConfig{
				StoreRequest:    false,
				MaxRequestBytes: 5,
			},
		},
	})
	body, truncated = svc.CaptureRequestBody([]byte("hello world"))
	require.Empty(t, body)
	require.False(t, truncated)
}

func TestConversationLogService_ApplySettingsEnablesRuntime(t *testing.T) {
	repo := &conversationLogRepoStub{ch: make(chan *ConversationLog, 1)}
	svc := NewConversationLogService(repo, &config.Config{})
	t.Cleanup(svc.Stop)
	require.False(t, svc.Enabled())

	svc.ApplySettings(&ConversationLogSettings{
		Enabled:            true,
		WorkerCount:        1,
		QueueSize:          8,
		TaskTimeoutSeconds: 1,
		OverflowPolicy:     config.UsageRecordOverflowPolicySync,
		StoreRequest:       true,
		StoreResponse:      true,
		MaxRequestBytes:    8,
		MaxResponseBytes:   16,
	})

	require.True(t, svc.Enabled())
	require.True(t, svc.StoreRequest())
	require.True(t, svc.StoreResponse())
	require.Equal(t, 0, svc.MaxResponseBytes())

	body, truncated := svc.CaptureRequestBody([]byte("hello world"))
	require.Equal(t, "hello world", body)
	require.False(t, truncated)

	svc.Submit(&ConversationLog{RequestID: "req_runtime", UserID: 2})
	select {
	case got := <-repo.ch:
		require.Equal(t, "req_runtime", got.RequestID)
	case <-time.After(time.Second):
		t.Fatal("conversation log task not executed after runtime enable")
	}

	before := svc.Stats().SubmittedTasks
	svc.ApplySettings(DefaultConversationLogSettings())
	require.False(t, svc.Enabled())
	svc.Submit(&ConversationLog{RequestID: "req_disabled", UserID: 2})
	require.Equal(t, before, svc.Stats().SubmittedTasks)
}
