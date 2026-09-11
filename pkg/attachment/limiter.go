// limiter.go 复刻官方 attachment-local/compression-limiter：实例级 FIFO 并发上限，
// 限制同时进行的原生图像变换数量，超出的任务排队等待空位。
package attachment

import "sync"

// Limiter 是固定并发度的 FIFO 限流器。
type Limiter struct {
	slots chan struct{}
	// FIFO 等待队列
	mu      sync.Mutex
	waiters []chan struct{}
}

// NewLimiter 创建并发上限为 concurrency 的限流器；非正值按 1 处理。
func NewLimiter(concurrency int) *Limiter {
	if concurrency < 1 {
		concurrency = 1
	}
	return &Limiter{slots: make(chan struct{}, concurrency)}
}

// Acquire 获取一个槽位（阻塞直到有空位），Release 归还。
func (l *Limiter) Acquire() { l.slots <- struct{}{} }

// TryAcquire 尝试非阻塞获取。
func (l *Limiter) TryAcquire() bool {
	select {
	case l.slots <- struct{}{}:
		return true
	default:
		return false
	}
}

// Release 归还一个槽位。
func (l *Limiter) Release() { <-l.slots }

// Active 返回当前占用槽位数。
func (l *Limiter) Active() int { return len(l.slots) }

// Run 获取槽位后执行 task，结束（无论成败）归还槽位。
func (l *Limiter) Run(task func()) {
	l.Acquire()
	defer l.Release()
	task()
}
