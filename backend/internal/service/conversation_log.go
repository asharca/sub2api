package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/alitto/pond/v2"
	"go.uber.org/zap"
)

const (
	defaultConversationLogWorkerCount        = 4
	defaultConversationLogQueueSize          = 4096
	defaultConversationLogTaskTimeoutSeconds = 5
	defaultConversationLogMaxRequestBytes    = 0
	defaultConversationLogMaxResponseBytes   = 0
	defaultConversationLogOverflowPolicy     = config.UsageRecordOverflowPolicySync
)

var ErrConversationLogNotFound = infraerrors.NotFound("CONVERSATION_LOG_NOT_FOUND", "conversation log not found")

type ConversationLogRepository interface {
	Create(ctx context.Context, log *ConversationLog) error
	List(ctx context.Context, params pagination.PaginationParams, filters ConversationLogFilters) ([]ConversationLog, *pagination.PaginationResult, error)
	GetByID(ctx context.Context, id int64) (*ConversationLog, error)
}

type ConversationLog struct {
	ID                int64
	RequestID         string
	ResponseID        string
	UserID            int64
	APIKeyID          int64
	AccountID         int64
	GroupID           *int64
	Platform          string
	InboundEndpoint   string
	UpstreamEndpoint  string
	Model             string
	RequestedModel    string
	UpstreamModel     string
	RequestType       RequestType
	Stream            bool
	OpenAIWSMode      bool
	StatusCode        int
	DurationMs        *int
	FirstTokenMs      *int
	InputTokens       int
	OutputTokens      int
	CacheReadTokens   int
	CacheCreateTokens int
	RequestHash       string
	RequestBody       string
	ResponseBody      string
	RequestTruncated  bool
	ResponseTruncated bool
	QueueDelayMs      *int
	CreatedAt         time.Time

	UserEmail   string
	APIKeyName  string
	AccountName string
	GroupName   string
}

type ConversationLogFilters struct {
	Search      string
	UserID      int64
	APIKeyID    int64
	AccountID   int64
	GroupID     int64
	Platform    string
	Model       string
	RequestID   string
	ResponseID  string
	RequestType *int16
	Stream      *bool
	StartTime   *time.Time
	EndTime     *time.Time
}

type ConversationLogStats struct {
	SubmittedTasks   uint64
	CompletedTasks   uint64
	FailedTasks      uint64
	DroppedTasks     uint64
	DroppedQueueFull uint64
	SyncFallback     uint64
	WaitingTasks     uint64
	RunningWorkers   int64
}

type ConversationLogService struct {
	repo             ConversationLogRepository
	mu               sync.RWMutex
	enabled          bool
	workerCount      int
	queueSize        int
	storeRequest     bool
	storeResponse    bool
	maxRequestBytes  int
	maxResponseBytes int
	taskTimeout      time.Duration
	overflowPolicy   string
	pool             pond.Pool
	submittedTasks   atomic.Uint64
	completedTasks   atomic.Uint64
	failedTasks      atomic.Uint64
	droppedTasks     atomic.Uint64
	droppedQueueFull atomic.Uint64
	syncFallback     atomic.Uint64
	lastDropLogNanos atomic.Int64
}

func NewConversationLogService(repo ConversationLogRepository, cfg *config.Config) *ConversationLogService {
	s := &ConversationLogService{repo: repo}
	s.applyOptions(conversationLogOptionsFromConfig(cfg))
	return s
}

func ProvideConversationLogService(repo ConversationLogRepository, cfg *config.Config, settingService *SettingService) *ConversationLogService {
	s := NewConversationLogService(repo, cfg)
	if settingService == nil {
		return s
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	settings, err := settingService.GetConversationLogSettings(ctx)
	if err != nil {
		logger.L().With(zap.String("component", "service.conversation_log")).Warn("conversation_log.settings_load_failed", zap.Error(err))
		settings = DefaultConversationLogSettings()
	}
	s.ApplySettings(settings)
	return s
}

func (s *ConversationLogService) Enabled() bool {
	if s == nil || s.repo == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.enabled
}

func (s *ConversationLogService) StoreRequest() bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.storeRequest
}

func (s *ConversationLogService) StoreResponse() bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.storeResponse
}

func (s *ConversationLogService) MaxResponseBytes() int {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.maxResponseBytes <= 0 {
		return 0
	}
	return s.maxResponseBytes
}

func (s *ConversationLogService) CaptureRequestBody(body []byte) (string, bool) {
	if s == nil {
		return "", false
	}
	s.mu.RLock()
	storeRequest := s.storeRequest
	maxRequestBytes := s.maxRequestBytes
	s.mu.RUnlock()
	if !storeRequest {
		return "", false
	}
	return captureConversationPayload(body, maxRequestBytes)
}

func (s *ConversationLogService) Submit(log *ConversationLog) {
	if s == nil || s.repo == nil || log == nil {
		return
	}
	s.mu.RLock()
	enabled := s.enabled
	pool := s.pool
	overflowPolicy := s.overflowPolicy
	s.mu.RUnlock()
	if !enabled {
		return
	}
	enqueuedAt := time.Now()
	task := func() {
		s.execute(enqueuedAt, log)
	}
	if pool == nil || pool.Stopped() {
		s.drop("stopped")
		return
	}
	s.submittedTasks.Add(1)
	if _, ok := pool.TrySubmit(task); ok {
		return
	}
	if pool.Stopped() {
		s.drop("stopped")
		return
	}
	if overflowPolicy == config.UsageRecordOverflowPolicySync {
		s.syncFallback.Add(1)
		task()
		return
	}
	s.droppedQueueFull.Add(1)
	s.drop("full")
}

func (s *ConversationLogService) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	pool := s.pool
	s.pool = nil
	s.enabled = false
	s.mu.Unlock()
	if pool == nil {
		return
	}
	pool.StopAndWait()
}

func (s *ConversationLogService) List(ctx context.Context, params pagination.PaginationParams, filters ConversationLogFilters) ([]ConversationLog, *pagination.PaginationResult, error) {
	if s == nil || s.repo == nil {
		return []ConversationLog{}, &pagination.PaginationResult{Total: 0, Page: params.Page, PageSize: params.Limit(), Pages: 1}, nil
	}
	return s.repo.List(ctx, params, filters)
}

func (s *ConversationLogService) GetByID(ctx context.Context, id int64) (*ConversationLog, error) {
	if s == nil || s.repo == nil {
		return nil, ErrConversationLogNotFound
	}
	return s.repo.GetByID(ctx, id)
}

func (s *ConversationLogService) Stats() ConversationLogStats {
	if s == nil {
		return ConversationLogStats{}
	}
	stats := ConversationLogStats{
		SubmittedTasks:   s.submittedTasks.Load(),
		CompletedTasks:   s.completedTasks.Load(),
		FailedTasks:      s.failedTasks.Load(),
		DroppedTasks:     s.droppedTasks.Load(),
		DroppedQueueFull: s.droppedQueueFull.Load(),
		SyncFallback:     s.syncFallback.Load(),
	}
	s.mu.RLock()
	pool := s.pool
	s.mu.RUnlock()
	if pool != nil {
		stats.WaitingTasks = pool.WaitingTasks()
		stats.RunningWorkers = pool.RunningWorkers()
	}
	return stats
}

func (s *ConversationLogService) execute(enqueuedAt time.Time, log *ConversationLog) {
	defer func() {
		if r := recover(); r != nil {
			s.failedTasks.Add(1)
			logger.L().With(
				zap.String("component", "service.conversation_log"),
				zap.Any("panic", r),
			).Error("conversation_log.task_panic")
		}
	}()
	s.mu.RLock()
	taskTimeout := s.taskTimeout
	s.mu.RUnlock()
	ctx, cancel := context.WithTimeout(context.Background(), taskTimeout)
	defer cancel()
	queueDelay := int(time.Since(enqueuedAt).Milliseconds())
	log.QueueDelayMs = &queueDelay
	if err := s.repo.Create(ctx, log); err != nil {
		s.failedTasks.Add(1)
		logger.L().With(
			zap.String("component", "service.conversation_log"),
			zap.String("request_id", log.RequestID),
			zap.Int64("user_id", log.UserID),
		).Warn("conversation_log.create_failed", zap.Error(err))
		return
	}
	s.completedTasks.Add(1)
}

func (s *ConversationLogService) drop(reason string) {
	if s == nil {
		return
	}
	s.droppedTasks.Add(1)
	now := time.Now().UnixNano()
	last := s.lastDropLogNanos.Load()
	if last != 0 && time.Duration(now-last) < 5*time.Second {
		return
	}
	if !s.lastDropLogNanos.CompareAndSwap(last, now) {
		return
	}
	logger.L().With(
		zap.String("component", "service.conversation_log"),
		zap.String("reason", reason),
		zap.Uint64("dropped", s.droppedTasks.Load()),
	).Warn("conversation_log.task_dropped")
}

func captureConversationPayload(data []byte, maxBytes int) (string, bool) {
	_ = maxBytes
	if len(data) == 0 {
		return "", false
	}
	return strings.ToValidUTF8(string(data), "\uFFFD"), false
}

func (s *ConversationLogService) ApplySettings(settings *ConversationLogSettings) {
	if s == nil {
		return
	}
	s.applyOptions(conversationLogOptionsFromSettings(settings))
}

func (s *ConversationLogService) applyOptions(opts conversationLogOptions) {
	if s == nil {
		return
	}
	opts = normalizeConversationLogOptions(opts)
	var nextPool pond.Pool
	if opts.Enabled && s.repo != nil {
		nextPool = pond.NewPool(opts.WorkerCount, pond.WithQueueSize(opts.QueueSize))
	}

	s.mu.Lock()
	oldPool := s.pool
	s.enabled = opts.Enabled
	s.workerCount = opts.WorkerCount
	s.queueSize = opts.QueueSize
	s.storeRequest = opts.StoreRequest
	s.storeResponse = opts.StoreResponse
	s.maxRequestBytes = opts.MaxRequestBytes
	s.maxResponseBytes = opts.MaxResponseBytes
	s.taskTimeout = time.Duration(opts.TaskTimeoutSeconds) * time.Second
	s.overflowPolicy = normalizeConversationLogOverflowPolicy(opts.OverflowPolicy)
	s.pool = nextPool
	s.mu.Unlock()

	if oldPool != nil {
		go oldPool.StopAndWait()
	}
}

type conversationLogOptions struct {
	Enabled            bool
	WorkerCount        int
	QueueSize          int
	TaskTimeoutSeconds int
	OverflowPolicy     string
	StoreRequest       bool
	StoreResponse      bool
	MaxRequestBytes    int
	MaxResponseBytes   int
}

func conversationLogOptionsFromConfig(cfg *config.Config) conversationLogOptions {
	opts := normalizeConversationLogOptions(conversationLogOptions{
		WorkerCount:        defaultConversationLogWorkerCount,
		QueueSize:          defaultConversationLogQueueSize,
		TaskTimeoutSeconds: defaultConversationLogTaskTimeoutSeconds,
		OverflowPolicy:     defaultConversationLogOverflowPolicy,
		StoreRequest:       true,
		StoreResponse:      true,
		MaxRequestBytes:    defaultConversationLogMaxRequestBytes,
		MaxResponseBytes:   defaultConversationLogMaxResponseBytes,
	})
	if cfg == nil {
		return opts
	}
	c := cfg.Gateway.ConversationLog
	opts.Enabled = c.Enabled
	if c.WorkerCount > 0 {
		opts.WorkerCount = c.WorkerCount
	}
	if c.QueueSize > 0 {
		opts.QueueSize = c.QueueSize
	}
	if c.TaskTimeoutSeconds > 0 {
		opts.TaskTimeoutSeconds = c.TaskTimeoutSeconds
	}
	if strings.TrimSpace(c.OverflowPolicy) != "" {
		opts.OverflowPolicy = c.OverflowPolicy
	}
	opts.StoreRequest = c.StoreRequest
	opts.StoreResponse = c.StoreResponse
	return normalizeConversationLogOptions(opts)
}

func conversationLogOptionsFromSettings(settings *ConversationLogSettings) conversationLogOptions {
	defaults := DefaultConversationLogSettings()
	if settings == nil {
		settings = defaults
	}
	return normalizeConversationLogOptions(conversationLogOptions{
		Enabled:            settings.Enabled,
		WorkerCount:        settings.WorkerCount,
		QueueSize:          settings.QueueSize,
		TaskTimeoutSeconds: settings.TaskTimeoutSeconds,
		OverflowPolicy:     settings.OverflowPolicy,
		StoreRequest:       settings.StoreRequest,
		StoreResponse:      settings.StoreResponse,
		MaxRequestBytes:    settings.MaxRequestBytes,
		MaxResponseBytes:   settings.MaxResponseBytes,
	})
}

func normalizeConversationLogOptions(opts conversationLogOptions) conversationLogOptions {
	if opts.WorkerCount <= 0 {
		opts.WorkerCount = defaultConversationLogWorkerCount
	}
	if opts.QueueSize <= 0 {
		opts.QueueSize = defaultConversationLogQueueSize
	}
	if opts.TaskTimeoutSeconds <= 0 {
		opts.TaskTimeoutSeconds = defaultConversationLogTaskTimeoutSeconds
	}
	opts.OverflowPolicy = normalizeConversationLogOverflowPolicy(opts.OverflowPolicy)
	opts.MaxRequestBytes = defaultConversationLogMaxRequestBytes
	opts.MaxResponseBytes = defaultConversationLogMaxResponseBytes
	return opts
}

func normalizeConversationLogOverflowPolicy(policy string) string {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case config.UsageRecordOverflowPolicySync:
		return config.UsageRecordOverflowPolicySync
	default:
		return defaultConversationLogOverflowPolicy
	}
}

func (s ConversationLogStats) String() string {
	return fmt.Sprintf("submitted=%d completed=%d failed=%d dropped=%d waiting=%d", s.SubmittedTasks, s.CompletedTasks, s.FailedTasks, s.DroppedTasks, s.WaitingTasks)
}
