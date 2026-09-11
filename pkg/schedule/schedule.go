// Package schedule 复刻官方 schedule 的进程内调度核心：after（延迟一次性）、
// at（绝对时刻一次性）、every（固定周期，不低于 5 分钟）三类提醒的注册、触发与
// 取消。提醒投递不离开拥有者会话（session-local）。
package schedule

import (
	"errors"
	"sync"
	"time"
)

// MinEveryInterval 是固定周期规则允许的最小间隔（5 分钟）。
const MinEveryInterval = 5 * time.Minute

// Kind 是调度规则类型。
type Kind string

const (
	KindAfter Kind = "after" // 相对延迟一次性
	KindAt    Kind = "at"    // 绝对时刻一次性
	KindEvery Kind = "every" // 固定周期
)

// ErrEmptyPrompt 提醒内容为空。
var ErrEmptyPrompt = errors.New("schedule: empty prompt")

// ErrNotFuture 目标时刻不在未来。
var ErrNotFuture = errors.New("schedule: target is not in the future")

// ErrIntervalTooShort 周期小于最小值。
var ErrIntervalTooShort = errors.New("schedule: every interval below minimum")

// Entry 是一条活动提醒。
type Entry struct {
	ID       string
	Kind     Kind
	Prompt   string
	NextAt   time.Time
	Interval time.Duration // 仅 every
}

// Dispatch 是一次到点回调。
type Dispatch struct {
	ID     string
	Prompt string
}

// Scheduler 是并发安全的进程内调度器。
type Scheduler struct {
	mu      sync.Mutex
	entries map[string]*Entry
	timers  map[string]*time.Timer
	out     chan Dispatch
	now     func() time.Time
}

// New 创建调度器，dispatch 送到返回的通道。
func New() *Scheduler {
	return &Scheduler{
		entries: map[string]*Entry{},
		timers:  map[string]*time.Timer{},
		out:     make(chan Dispatch, 16),
		now:     time.Now,
	}
}

// SetNow 注入时钟（测试用）。
func (s *Scheduler) SetNow(f func() time.Time) {
	s.mu.Lock()
	s.now = f
	s.mu.Unlock()
}

// Out 返回到点通知通道。
func (s *Scheduler) Out() <-chan Dispatch { return s.out }

func (s *Scheduler) arm(id string, at time.Time) {
	d := time.Until(at)
	t := time.AfterFunc(d, func() { s.fire(id) })
	s.timers[id] = t
}

// After 注册相对延迟一次性提醒。
func (s *Scheduler) After(id, prompt string, delay time.Duration) error {
	if prompt == "" {
		return ErrEmptyPrompt
	}
	if delay <= 0 {
		return ErrNotFuture
	}
	return s.add(&Entry{ID: id, Kind: KindAfter, Prompt: prompt, NextAt: s.now().Add(delay)})
}

// At 注册绝对时刻一次性提醒。
func (s *Scheduler) At(id, prompt string, at time.Time) error {
	if prompt == "" {
		return ErrEmptyPrompt
	}
	if !at.After(s.now()) {
		return ErrNotFuture
	}
	return s.add(&Entry{ID: id, Kind: KindAt, Prompt: prompt, NextAt: at})
}

// Every 注册固定周期提醒。
func (s *Scheduler) Every(id, prompt string, interval time.Duration) error {
	if prompt == "" {
		return ErrEmptyPrompt
	}
	if interval < MinEveryInterval {
		return ErrIntervalTooShort
	}
	e := &Entry{ID: id, Kind: KindEvery, Prompt: prompt, NextAt: s.now().Add(interval), Interval: interval}
	return s.add(e)
}

func (s *Scheduler) add(e *Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[e.ID] = e
	s.arm(e.ID, e.NextAt)
	return nil
}

// fire 到点：发出通知；一次性移除，周期性重排。
func (s *Scheduler) fire(id string) {
	s.mu.Lock()
	e, ok := s.entries[id]
	if !ok {
		s.mu.Unlock()
		return
	}
	s.out <- Dispatch{ID: id, Prompt: e.Prompt}
	if e.Kind == KindEvery {
		e.NextAt = s.now().Add(e.Interval)
		s.arm(id, e.NextAt)
	} else {
		delete(s.entries, id)
		delete(s.timers, id)
	}
	s.mu.Unlock()
}

// Cancel 取消一条提醒；不存在返回 false。
func (s *Scheduler) Cancel(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.timers[id]
	if ok {
		t.Stop()
	}
	_, exists := s.entries[id]
	delete(s.entries, id)
	delete(s.timers, id)
	return exists
}

// List 列出活动提醒快照。
func (s *Scheduler) List() []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Entry, 0, len(s.entries))
	for _, e := range s.entries {
		out = append(out, *e)
	}
	return out
}

// Shutdown 停止全部计时器。
func (s *Scheduler) Shutdown() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.timers {
		t.Stop()
	}
	s.entries = map[string]*Entry{}
	s.timers = map[string]*time.Timer{}
}
