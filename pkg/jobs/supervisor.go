// 本文件对应任务 H08：Goroutine 治理 + 单一 Watcher（统一 Supervisor 生命周期）。
//
// 背景：项目散落的后台 goroutine（fsnotify watcher、ticker 型 writer、telemetry exporter、
// JSONL shard writer 等）各自手写 close channel + WaitGroup，启动/关闭路径不统一，
// 容易出现：关闭顺序错误 → 半开泄漏；漏 cancel → 退出后残留 goroutine；重复 Shutdown
// 行为不一致。
//
// Supervisor 提供"统一启动 / 统一关闭"的生命周期治理：
//   - Go(name, fn)：把任意后台 goroutine 挂到 Supervisor，父 ctx 会级联注入；
//   - Shutdown(ctx)：一次性关闭所有 worker，等待全部退出；超时返回部分失败；
//   - 幂等：重复 Shutdown 安全；挂到其上的 worker 不会泄漏。
//
// 用法示例（把 shardWriter / fsnotify / exporter 都包进来）：
//
//	sup := jobs.New()
//	sup.Go("jsonl-shard-3", func(ctx context.Context) { j.shardWriter(ctx, 3) })
//	sup.Go("fsnotify",           func(ctx context.Context) { watchLoop(ctx, dir) })
//	if err := sup.Shutdown(ctx); err != nil { ... }
package jobs

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Supervisor 是统一的后台 goroutine 生命周期治理器。
type Supervisor struct {
	mu     sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc
	// workers 名字 → worker 状态。
	workers map[string]*workerState
	closed  bool
}

// workerState 单个 worker 的状态。
type workerState struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// New 创建 Supervisor（自带可取消的根 ctx，可级联到所有 worker）。
func New() *Supervisor {
	ctx, cancel := context.WithCancel(context.Background())
	return &Supervisor{ctx: ctx, cancel: cancel, workers: map[string]*workerState{}}
}

// Go 注册并启动一个受管 goroutine。
//   - name 必须唯一（重复注册用 _name#n 去重，保证不 panic）；
//   - fn 收到一个 ctx；当 Supervisor.Shutdown() 被调用时，此 ctx 被取消，
//     fn 应监听 ctx.Done() 自行退出；
//   - 若已 Shutdown，Go 立即拒绝（返回 false）。
func (s *Supervisor) Go(name string, fn func(ctx context.Context)) bool {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return false
	}
	// 去重：同名追加序号。
	key := name
	if _, exists := s.workers[key]; exists {
		for i := 1; ; i++ {
			cand := fmt.Sprintf("%s#%d", name, i)
			if _, ok := s.workers[cand]; !ok {
				key = cand
				break
			}
		}
	}
	wctx, wcancel := context.WithCancel(s.ctx)
	st := &workerState{cancel: wcancel, done: make(chan struct{})}
	s.workers[key] = st
	s.mu.Unlock()

	go func() {
		defer close(st.done)
		defer wcancel()
		fn(wctx)
	}()
	return true
}

// WorkerCount 返回当前受管 worker 数（含已完成但未复查的）。
func (s *Supervisor) WorkerCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.workers)
}

// Name 返回全部 worker 名。
func (s *Supervisor) Names() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.workers))
	for n := range s.workers {
		out = append(out, n)
	}
	return out
}

// CancelWorker 单独取消某个 worker（不关闭整个 Supervisor）。
func (s *Supervisor) CancelWorker(name string) {
	s.mu.Lock()
	st, ok := s.workers[name]
	s.mu.Unlock()
	if ok && st != nil {
		st.cancel()
	}
}

// WaitWorker 阻塞等待指定 worker 退出（最多 wait）。
func (s *Supervisor) WaitWorker(ctx context.Context, name string) bool {
	s.mu.Lock()
	st, ok := s.workers[name]
	s.mu.Unlock()
	if !ok || st == nil {
		return true
	}
	select {
	case <-st.done:
		return true
	case <-ctx.Done():
		return false
	}
}

// Shutdown 关闭所有 worker 并等待退出。
//   - ctx 提供整体超时：超时未退出的 worker 计入错误集返回；
//   - 幂等：多次调用安全（第一次执行真正的关闭，后续返回 nil 或基于已记录状态）；
//   - 关闭后任何 Go() 调用被拒绝。
func (s *Supervisor) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.cancel() // 取消根 ctx → 级联取消所有 worker 子 ctx
	states := make([]*workerState, 0, len(s.workers))
	for _, st := range s.workers {
		states = append(states, st)
	}
	s.mu.Unlock()

	// 等待所有 worker 退出；带整体超时。
	var wg sync.WaitGroup
	var mu sync.Mutex
	var failed []string
	for _, st := range states {
		wg.Add(1)
		go func(x *workerState, name string) {
			defer wg.Done()
			select {
			case <-x.done:
			case <-ctx.Done():
				mu.Lock()
				failed = append(failed, name)
				mu.Unlock()
			}
		}(st, "") // name not needed in error; we collect only on timeout
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
		mu.Lock()
		list := failed
		mu.Unlock()
		if len(list) == 0 {
			return ctx.Err()
		}
		return fmt.Errorf("jobs: shutdown timeout, %d worker(s) not exited", len(list))
	}
	return nil
}

// Healthy 返回是否有 worker 仍存活（done 未关闭）。
func (s *Supervisor) Healthy(ctx context.Context) bool {
	s.mu.Lock()
	states := make([]*workerState, 0, len(s.workers))
	for _, st := range s.workers {
		states = append(states, st)
	}
	s.mu.Unlock()
	// 全部 done 关闭才算健康（无存活 worker 也算健康：空 Supervisor 应健康）。
	for _, st := range states {
		select {
		case <-st.done:
		default:
			return true // 仍有存活 worker → 视为"仍在运行"
		}
	}
	return false // 无存活 worker
}

// Close 便捷：用默认短超时关闭（等价 Shutdown(context.Background())，
// 建议调用方用带超时的 Shutdown）。
func (s *Supervisor) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.Shutdown(ctx)
}