// 本文件对应任务 H06：Tool Pipeline 对象池（ExecContext / Meta map sync.Pool 回收）。
//
// 验证目标：
//   1. 功能等价：SetPooled(true) 后 Run 与默认 Run 在 pre-deny / execute / post-block /
//      result 各分支行为一致；
//   2. 复用安全：多次调用后返回的 *ToolCallResult 相互独立（调用方持有不互相干扰）；
//   3. 池化命中：开启后应复用（可观测，用reflect或直接通过行为验证）；
//   4. Benchmark：pooled VS 非 pooled 的 allocs/op 对比（pooled 分配显著减少）。
package tests

import (
	"context"
	"strings"
	"testing"

	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/tools"
	"github.com/JopenChen/dsh-go/pkg/waterfall"
)

// ============================================================================
// 1. 功能等价：pooled Run 与默认 Run 各分支行为一致
// ============================================================================

// TestH06PooledPreDeny 验证 pooled 模式下 pre-deny 短路正确。
func TestH06PooledPreDeny(t *testing.T) {
	p := tools.NewPipeline().SetPooled(true).
		UsePre(func(ec *tools.ExecContext, next waterfall.NextFunc) error {
			if ec.Request.Input["danger"] == true {
				ec.Denied = true
				return nil
			}
			return next()
		}).
		WithTool(makeEchoTool())

	req := &tools.ToolCallRequest{
		CallID: brand.NewToolCallID("hp1"),
		Tool:   "echo",
		Input:  map[string]any{"msg": "hi", "danger": true},
	}
	res := p.Run(context.Background(), req, nil)
	if !res.IsError {
		t.Fatal("pooled pre deny 应标记 isError")
	}
	if !strings.Contains(res.Error, "denied") {
		t.Fatalf("pooled deny 错误应含 denied: %q", res.Error)
	}
	if res.Value != nil {
		t.Fatal("pooled deny 后不应有值")
	}
	// 池化状态查询
	if !p.PooledState() {
		t.Fatal("PooledState 应为 true")
	}
}

// TestH06PooledExecuteResult 验证 pooled 模式下正常执行返回正确值。
func TestH06PooledExecuteResult(t *testing.T) {
	p := tools.NewPipeline().SetPooled(true).WithTool(makeEchoTool())
	req := &tools.ToolCallRequest{
		CallID: brand.NewToolCallID("hp2"),
		Tool:   "echo",
		Input:  map[string]any{"msg": "world"},
	}
	res := p.Run(context.Background(), req, nil)
	if res.IsError {
		t.Fatalf("pooled 正常执行不应出错: %s", res.Error)
	}
	v, ok := res.Value.(map[string]any)
	if !ok {
		t.Fatalf("res.Value 类型异常: %T", res.Value)
	}
	if v["echo"] != "world" {
		t.Fatalf("echo 值异常: %v", v)
	}
}

// TestH06PooledResultIndependent 验证多次调用返回的 Result 相互独立（调用方持有的
// 上一次 result 不会被下一次 Run 改写）。
func TestH06PooledResultIndependent(t *testing.T) {
	p := tools.NewPipeline().SetPooled(true).WithTool(makeEchoTool())
	ctx := context.Background()

	// 生成一批互不相同的请求，手动调用独立 Result 指针。
	results := make([]*tools.ToolCallResult, 0, 5)
	for i := 0; i < 5; i++ {
		req := &tools.ToolCallRequest{
			CallID: brand.NewToolCallID("ind-" + itoa(i)),
			Tool:   "echo",
			Input:  map[string]any{"msg": "m" + itoa(i)},
		}
		results = append(results, p.Run(ctx, req, nil))
	}
	// 每个结果都应保留其各自的 CallID（若结果被池化改写，这些会互相污染）。
	for i, r := range results {
		if r.CallID.Raw() != "ind-"+itoa(i) {
			t.Fatalf("第 %d 个结果 CallID 被污染: %s", i, r.CallID.Raw())
		}
		v, ok := r.Value.(map[string]any)
		if !ok || v["echo"] != "m"+itoa(i) {
			t.Fatalf("第 %d 个结果值被污染: %+v", i, r.Value)
		}
	}
}

// TestH06PooledMetaIsolation 验证 pooled 复用 ExecContext 后 Meta map 被正确清空（不串味）。
func TestH06PooledMetaIsolation(t *testing.T) {
	// pre 级断言：每次进入本轮的 Meta 应为空（上一轮 post 污染必须已被 clearMeta 清掉）。
	p := tools.NewPipeline().SetPooled(true).
		UseExecute(func(ec *tools.ExecContext, next waterfall.NextFunc) error {
			if len(ec.Meta) != 0 {
				t.Fatalf("复用 ExecContext 时 Meta 残留: %+v", ec.Meta)
			}
			return next()
		}).
		UsePost(func(ec *tools.ExecContext, next waterfall.NextFunc) error {
			// 每轮往 Meta 里塞一个 tag。
			ec.Meta["tag"] = ec.Request.Input["tag"]
			return next()
		}).
		WithTool(makeEchoTool())
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		req := &tools.ToolCallRequest{
			CallID: brand.NewToolCallID("meta-" + itoa(i)),
			Tool:   "echo",
			Input:  map[string]any{"msg": "x", "tag": "t" + itoa(i)},
		}
		p.Run(ctx, req, nil)
	}
}

// ============================================================================
// 2. Benchmark：pooled VS 非 pooled —— allocs / op 对比
// ============================================================================

// BenchmarkH06RunUnPooled 默认路径基线。
func BenchmarkH06RunUnPooled(b *testing.B) {
	p := tools.NewPipeline().WithTool(makeEchoTool())
	ctx := context.Background()
	req := &tools.ToolCallRequest{
		CallID: brand.NewToolCallID("bench"), Tool: "echo",
		Input: map[string]any{"msg": "x"},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = p.Run(ctx, req, nil)
	}
}

// BenchmarkH06RunPooled H06 池化路径。
func BenchmarkH06RunPooled(b *testing.B) {
	p := tools.NewPipeline().SetPooled(true).WithTool(makeEchoTool())
	ctx := context.Background()
	req := &tools.ToolCallRequest{
		CallID: brand.NewToolCallID("bench"), Tool: "echo",
		Input: map[string]any{"msg": "x"},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = p.Run(ctx, req, nil)
	}
}