// 本文件对应任务 M02：Waterfall 中间件链原语。
package tests

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/JopenChen/dsh-go/pkg/waterfall"
)

// testPayload 模拟一条被各级中间件传递与改写的载荷。
type testPayload struct {
	seq     []string // 记录各级进入/离开顺序
	msg     string   // 可被中间件改写的文本
	blocked bool     // 是否被拦截
}

// TestWaterfallOrdering 验证多级中间件严格按注册顺序调用（洋葱进入/离开）。
func TestWaterfallOrdering(t *testing.T) {
	p := &testPayload{seq: []string{}}

	chain := waterfall.New[testPayload](
		func(p *testPayload, next waterfall.NextFunc) error {
			p.seq = append(p.seq, "A-in")
			err := next()
			p.seq = append(p.seq, "A-out")
			return err
		},
		func(p *testPayload, next waterfall.NextFunc) error {
			p.seq = append(p.seq, "B-in")
			err := next()
			p.seq = append(p.seq, "B-out")
			return err
		},
		func(p *testPayload, next waterfall.NextFunc) error {
			p.seq = append(p.seq, "C-in")
			err := next()
			p.seq = append(p.seq, "C-out")
			return err
		},
	)

	if err := chain.Run(context.Background(), p); err != nil {
		t.Fatalf("Run 失败: %v", err)
	}

	want := []string{"A-in", "B-in", "C-in", "C-out", "B-out", "A-out"}
	if strings.Join(p.seq, ",") != strings.Join(want, ",") {
		t.Fatalf("调用顺序 = %v, want %v", p.seq, want)
	}
}

// TestWaterfallShortCircuit 验证中间件不调用 next() 时短路终止。
func TestWaterfallShortCircuit(t *testing.T) {
	var entered int32
	p := &testPayload{seq: []string{}}

	chain := waterfall.New[testPayload](
		func(p *testPayload, next waterfall.NextFunc) error {
			p.seq = append(p.seq, "guard")
			// 不调用 next() → 短路
			return nil
		},
		func(p *testPayload, next waterfall.NextFunc) error {
			atomic.AddInt32(&entered, 1)
			return next()
		},
	)

	if err := chain.Run(context.Background(), p); err != nil {
		t.Fatalf("短路场景 Run 应返回 nil, 实际 %v", err)
	}
	if entered != 0 {
		t.Fatalf("短路后后续中间件不应执行, entered = %d", entered)
	}
	if len(p.seq) != 1 {
		t.Fatalf("只有首个中间件应进入, seq = %v", p.seq)
	}
}

// TestWaterfallPayloadRewrite 验证 payload 可被任一中间件改写并传递到下一级。
func TestWaterfallPayloadRewrite(t *testing.T) {
	p := &testPayload{msg: "initial"}

	chain := waterfall.New[testPayload](
		func(p *testPayload, next waterfall.NextFunc) error {
			p.msg += "+m1"
			return next()
		},
		func(p *testPayload, next waterfall.NextFunc) error {
			p.msg += "+m2"
			return next()
		},
		func(p *testPayload, next waterfall.NextFunc) error {
			p.msg += "+m3"
			return next()
		},
	)

	if err := chain.Run(context.Background(), p); err != nil {
		t.Fatalf("Run 失败: %v", err)
	}
	if p.msg != "initial+m1+m2+m3" {
		t.Fatalf("改写结果 = %q, want %q", p.msg, "initial+m1+m2+m3")
	}
}

// TestWaterfallErrorPropagation 验证中间件返回 error 时链立即终止并传播错误。
func TestWaterfallErrorPropagation(t *testing.T) {
	var after int32
	boom := errors.New("boom")
	p := &testPayload{}

	chain := waterfall.New[testPayload](
		func(p *testPayload, next waterfall.NextFunc) error {
			return next()
		},
		func(p *testPayload, next waterfall.NextFunc) error {
			return boom // 第二个中间件直接报错
		},
		func(p *testPayload, next waterfall.NextFunc) error {
			atomic.AddInt32(&after, 1)
			return next()
		},
	)

	err := chain.Run(context.Background(), p)
	if !errors.Is(err, boom) {
		t.Fatalf("应传播 boom, 实际 %v", err)
	}
	if after != 0 {
		t.Fatalf("错误后后续中间件不应执行, after = %d", after)
	}
}

// TestWaterfallNextSingleCall 验证同一中间件内重复调用 next() 触发 panic 保护。
func TestWaterfallNextSingleCall(t *testing.T) {
	p := &testPayload{}

	chain := waterfall.New[testPayload](
		func(p *testPayload, next waterfall.NextFunc) error {
			_ = next()
			// 第二次调用应 panic
			_ = next()
			return nil
		},
	)

	defer func() {
		if recover() == nil {
			t.Fatal("重复调用 next() 应 panic")
		}
	}()
	_ = chain.Run(context.Background(), p)
}

// TestWaterfallDownstreamErrorToUpstream 验证中间件可捕获/改写下游错误。
func TestWaterfallDownstreamErrorToUpstream(t *testing.T) {
	p := &testPayload{}

	chain := waterfall.New[testPayload](
		func(p *testPayload, next waterfall.NextFunc) error {
			err := next()
			if err != nil {
				// 吞掉下游错误并改写
				return errors.New("wrapped: " + err.Error())
			}
			return nil
		},
		func(p *testPayload, next waterfall.NextFunc) error {
			return errors.New("inner failure")
		},
	)

	err := chain.Run(context.Background(), p)
	if err == nil || !strings.Contains(err.Error(), "wrapped:") {
		t.Fatalf("应返回改写后的错误, 实际 %v", err)
	}
}

// TestWaterfallEmptyChain 验证空链直接返回 nil。
func TestWaterfallEmptyChain(t *testing.T) {
	chain := waterfall.New[testPayload]()
	if err := chain.Run(context.Background(), &testPayload{}); err != nil {
		t.Fatalf("空链应返回 nil, 实际 %v", err)
	}
}

// TestWaterfallTypedPayloads 验证泛型可承载任意载荷类型（不同中间件链互不干扰）。
func TestWaterfallTypedPayloads(t *testing.T) {
	type request struct{ url string }
	type approval struct{ ok bool }

	reqChain := waterfall.New[request](
		func(r *request, next waterfall.NextFunc) error {
			r.url = "https://example.com"
			return next()
		},
	)
	apprChain := waterfall.New[approval](
		func(a *approval, next waterfall.NextFunc) error {
			a.ok = true
			return next()
		},
	)

	req := &request{}
	appr := &approval{}
	_ = reqChain.Run(context.Background(), req)
	_ = apprChain.Run(context.Background(), appr)

	if req.url != "https://example.com" || !appr.ok {
		t.Fatalf("两条不同类型的链应独立工作: req=%+v appr=%+v", req, appr)
	}
}
