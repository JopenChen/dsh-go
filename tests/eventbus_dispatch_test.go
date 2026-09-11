// Package tests 的事件总线（eventbus）验收测试。
//
// 覆盖 Cordis 四种同步分发模式在 Go 侧的对应契约：
//   - Emit 广播：所有监听器执行，返回值忽略
//   - Bail 保释：首个命中的监听器短路
//   - Serial 串行：按注册顺序聚合返回值
//   - On 返回的 dispose 摘除监听（对应插件卸载自动清理）
package tests

import (
	"errors"
	"testing"

	"github.com/JopenChen/dsh-go/pkg/eventbus"
)

func TestEventBusEmitRunsAll(t *testing.T) {
	bus := eventbus.New[string, struct{}]()
	var got []string
	bus.On(func(s string) struct{} { got = append(got, "a:"+s); return struct{}{} })
	bus.On(func(s string) struct{} { got = append(got, "b:"+s); return struct{}{} })

	bus.Emit("x")

	if len(got) != 2 || got[0] != "a:x" || got[1] != "b:x" {
		t.Fatalf("emit should run all listeners in order, got %v", got)
	}
}

func TestEventBusBailShortCircuits(t *testing.T) {
	bus := eventbus.New[int, error]()
	var reached int
	bus.On(func(n int) error { reached = 1; return nil }) // 未命中
	hit := errors.New("boom")
	bus.On(func(n int) error { reached = 2; return hit }) // 命中并短路
	bus.On(func(n int) error { reached = 3; return nil }) // 不应到达

	r, ok := bus.Bail(7, func(e error) bool { return e != nil })

	if !ok || !errors.Is(r, hit) {
		t.Fatalf("bail should return first truthy result, got %v ok=%v", r, ok)
	}
	if reached != 2 {
		t.Fatalf("bail should short-circuit before third listener, reached=%d", reached)
	}
}

func TestEventBusBailNoHitReturnsFalse(t *testing.T) {
	bus := eventbus.New[int, error]()
	bus.On(func(n int) error { return nil })
	_, ok := bus.Bail(1, func(e error) bool { return e != nil })
	if ok {
		t.Fatal("bail without a hit should report false")
	}
}

func TestEventBusSerialAggregates(t *testing.T) {
	bus := eventbus.New[int, int]()
	bus.On(func(n int) int { return n + 1 })
	bus.On(func(n int) int { return n + 2 })

	out := bus.Serial(10)

	if len(out) != 2 || out[0] != 11 || out[1] != 12 {
		t.Fatalf("serial should aggregate in registration order, got %v", out)
	}
}

func TestEventBusDisposeRemovesListener(t *testing.T) {
	bus := eventbus.New[string, struct{}]()
	var count int
	dispose := bus.On(func(s string) struct{} { count++; return struct{}{} })

	bus.Emit("x")
	dispose()
	bus.Emit("x")

	if count != 1 {
		t.Fatalf("dispose should remove listener, got count=%d", count)
	}
}

func TestEventBusDisposeIdempotent(t *testing.T) {
	bus := eventbus.New[string, struct{}]()
	dispose := bus.On(func(s string) struct{} { return struct{}{} })
	dispose()
	dispose() // 不应 panic
	if bus.Len() != 0 {
		t.Fatalf("double dispose should keep len 0, got %d", bus.Len())
	}
}

func TestSignalEmit(t *testing.T) {
	sig := eventbus.NewSignal[int]()
	var sum int
	sig.On(func(n int) { sum += n })
	sig.On(func(n int) { sum += n })
	sig.Emit(3)
	if sum != 6 {
		t.Fatalf("signal should notify all, sum=%d", sum)
	}
}
