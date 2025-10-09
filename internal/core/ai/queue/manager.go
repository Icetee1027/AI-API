package queue

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"recipe-generator/internal/infrastructure/config"
	"recipe-generator/internal/pkg/common"

	"go.uber.org/zap"
)

// Request 隊列請求（已棄用，改用直接調用）
type Request struct {
	Context context.Context
	Request interface{} // 改為 interface{} 以保持兼容性
	Result  chan Result
}

// Result 處理結果
type Result struct {
	Response interface{} // 改為 interface{} 以保持兼容性
	Error    error
}

// Status 隊列狀態
type Status struct {
	QueueLength    int `json:"queue_length"`
	ProcessedCount int `json:"processed_count"`
	MaxQueueSize   int `json:"max_queue_size"`
	Workers        int `json:"workers"`
}

// Manager 隊列管理器
type Manager struct {
	config    *config.Config
	queue     chan *Request
	done      chan struct{}
	processed int64
	mu        sync.RWMutex
}

// NewManager 創建新的隊列管理器
func NewManager(cfg *config.Config) *Manager {
	return &Manager{
		config:    cfg,
		queue:     make(chan *Request, cfg.Queue.MaxSize),
		done:      make(chan struct{}),
		processed: 0,
	}
}

// GetQueue 獲取請求隊列
func (m *Manager) GetQueue() <-chan *Request {
	return m.queue
}

// Enqueue 將請求加入隊列（已棄用）
func (m *Manager) Enqueue(ctx context.Context, req interface{}) (chan Result, error) {
	// 檢查隊列容量
	if len(m.queue) >= m.config.Queue.MaxSize {
		return nil, fmt.Errorf("queue is full")
	}

	// 創建隊列請求
	queueReq := Request{
		Context: ctx,
		Request: req,
		Result:  make(chan Result, 1),
	}

	// 加入隊列
	select {
	case m.queue <- &queueReq:
		common.LogInfo("Request enqueued",
			zap.Int("queue_length", len(m.queue)),
			zap.Int("max_queue_size", m.config.Queue.MaxSize),
		)
		return queueReq.Result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-m.done:
		return nil, fmt.Errorf("queue manager is closed")
	}
}

// GetQueueStatus 獲取隊列狀態
func (m *Manager) GetQueueStatus() *Status {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return &Status{
		QueueLength:    len(m.queue),
		ProcessedCount: int(m.processed),
		MaxQueueSize:   m.config.Queue.MaxSize,
		Workers:        m.config.Queue.Workers,
	}
}

// IncrementProcessed 增加處理計數
func (m *Manager) IncrementProcessed() {
	atomic.AddInt64(&m.processed, 1)
}

// Done 關閉隊列管理器
func (m *Manager) Done() {
	close(m.done)
}

// Close 關閉隊列管理器
func (m *Manager) Close() {
	close(m.done)
	close(m.queue)
}
