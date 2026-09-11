// pool.go 复刻官方 agent-loop 的有界滚动并行池：同一 step 内标记为 parallel 的
// 工具调用最多以 DefaultMaxParallelToolCalls 的并发同时在途，结果严格按模型给出
// 的顺序归位（并发执行、有序提交）。这里提供与具体工具无关的泛型执行器。
package tools

import (
	"context"
	"sync"
)

// DefaultMaxParallelToolCalls 是单个 agent step 内并行工具调用的默认在途上限。
const DefaultMaxParallelToolCalls = 10

// RunParallel 以最多 maxConcurrent 的并发执行 tasks，返回值严格按 tasks 的输入
// 顺序排列。任一任务返回错误，立即取消其余尚未开始/仍在运行的任务并返回该错误。
func RunParallel[T any](ctx context.Context, tasks []func(context.Context) (T, error), maxConcurrent int) ([]T, error) {
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	results := make([]T, len(tasks))
	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup
	var errOnce sync.Once
	var firstErr error

	for i, task := range tasks {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			break
		}
		if ctx.Err() != nil {
			<-sem
			break
		}
		wg.Add(1)
		go func(idx int, fn func(context.Context) (T, error)) {
			defer wg.Done()
			defer func() { <-sem }()
			v, err := fn(ctx)
			if err != nil {
				errOnce.Do(func() {
					firstErr = err
					cancel()
				})
				return
			}
			results[idx] = v // 按索引归位，天然保持模型顺序
		}(i, task)
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	return results, nil
}
