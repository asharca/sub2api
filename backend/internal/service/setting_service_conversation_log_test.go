package service

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type conversationLogSettingsRepoStub struct {
	mu   sync.Mutex
	data map[string]string
}

func newConversationLogSettingsRepoStub() *conversationLogSettingsRepoStub {
	return &conversationLogSettingsRepoStub{data: make(map[string]string)}
}

func (r *conversationLogSettingsRepoStub) Get(_ context.Context, key string) (*Setting, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value, ok := r.data[key]
	if !ok {
		return nil, ErrSettingNotFound
	}
	return &Setting{Key: key, Value: value}, nil
}

func (r *conversationLogSettingsRepoStub) GetValue(_ context.Context, key string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.data[key], nil
}

func (r *conversationLogSettingsRepoStub) Set(_ context.Context, key, value string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data[key] = value
	return nil
}

func (r *conversationLogSettingsRepoStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := r.data[key]; ok {
			result[key] = value
		}
	}
	return result, nil
}

func (r *conversationLogSettingsRepoStub) SetMultiple(_ context.Context, settings map[string]string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, value := range settings {
		r.data[key] = value
	}
	return nil
}

func (r *conversationLogSettingsRepoStub) GetAll(_ context.Context) (map[string]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make(map[string]string, len(r.data))
	for key, value := range r.data {
		result[key] = value
	}
	return result, nil
}

func (r *conversationLogSettingsRepoStub) Delete(_ context.Context, key string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.data, key)
	return nil
}

func TestConversationLogSettings_DefaultsWhenMissing(t *testing.T) {
	svc := NewSettingService(newConversationLogSettingsRepoStub(), &config.Config{})

	settings, err := svc.GetConversationLogSettings(context.Background())

	require.NoError(t, err)
	require.False(t, settings.Enabled)
	require.Equal(t, 4, settings.WorkerCount)
	require.Equal(t, 4096, settings.QueueSize)
	require.Equal(t, config.UsageRecordOverflowPolicySync, settings.OverflowPolicy)
	require.True(t, settings.StoreRequest)
	require.True(t, settings.StoreResponse)
}

func TestConversationLogSettings_SetAndGet(t *testing.T) {
	repo := newConversationLogSettingsRepoStub()
	svc := NewSettingService(repo, &config.Config{})

	err := svc.SetConversationLogSettings(context.Background(), &ConversationLogSettings{
		Enabled:            true,
		WorkerCount:        2,
		QueueSize:          16,
		TaskTimeoutSeconds: 3,
		OverflowPolicy:     config.UsageRecordOverflowPolicySync,
		StoreRequest:       false,
		StoreResponse:      true,
		MaxRequestBytes:    0,
		MaxResponseBytes:   42,
	})
	require.NoError(t, err)

	var persisted ConversationLogSettings
	require.NoError(t, json.Unmarshal([]byte(repo.data[SettingKeyConversationLogSettings]), &persisted))
	require.Equal(t, config.UsageRecordOverflowPolicySync, persisted.OverflowPolicy)

	got, err := svc.GetConversationLogSettings(context.Background())
	require.NoError(t, err)
	require.True(t, got.Enabled)
	require.Equal(t, 2, got.WorkerCount)
	require.Equal(t, 16, got.QueueSize)
	require.Equal(t, 3, got.TaskTimeoutSeconds)
	require.False(t, got.StoreRequest)
	require.True(t, got.StoreResponse)
	require.Equal(t, 0, got.MaxRequestBytes)
	require.Equal(t, 0, got.MaxResponseBytes)
}

func TestConversationLogSettings_DisabledNormalizesInvalidValues(t *testing.T) {
	svc := NewSettingService(newConversationLogSettingsRepoStub(), &config.Config{})

	err := svc.SetConversationLogSettings(context.Background(), &ConversationLogSettings{
		Enabled:            false,
		WorkerCount:        0,
		QueueSize:          0,
		TaskTimeoutSeconds: 0,
		OverflowPolicy:     "bad",
		StoreRequest:       true,
		StoreResponse:      true,
		MaxRequestBytes:    -1,
		MaxResponseBytes:   -1,
	})
	require.NoError(t, err)

	got, err := svc.GetConversationLogSettings(context.Background())
	require.NoError(t, err)
	require.False(t, got.Enabled)
	require.Equal(t, defaultConversationLogWorkerCount, got.WorkerCount)
	require.Equal(t, defaultConversationLogQueueSize, got.QueueSize)
	require.Equal(t, defaultConversationLogTaskTimeoutSeconds, got.TaskTimeoutSeconds)
	require.Equal(t, defaultConversationLogOverflowPolicy, got.OverflowPolicy)
	require.Zero(t, got.MaxRequestBytes)
	require.Zero(t, got.MaxResponseBytes)
}

func TestConversationLogSettings_EnabledRejectsInvalidValues(t *testing.T) {
	svc := NewSettingService(newConversationLogSettingsRepoStub(), &config.Config{})

	err := svc.SetConversationLogSettings(context.Background(), &ConversationLogSettings{
		Enabled:            true,
		WorkerCount:        0,
		QueueSize:          1,
		TaskTimeoutSeconds: 1,
		OverflowPolicy:     config.UsageRecordOverflowPolicySync,
		StoreRequest:       true,
		StoreResponse:      true,
		MaxRequestBytes:    0,
		MaxResponseBytes:   0,
	})

	require.ErrorContains(t, err, "worker_count")
}
