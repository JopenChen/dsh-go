// Package tests 的工具协作式超时策略验收测试。
package tests

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/JopenChen/dsh-go/pkg/tools"
)

func TestWrapTimeoutReturnsValue(t *testing.T) {
	fn := tools.WrapTimeout(func(ctx context.Context, in map[string]any) (any, error) {
		return "ok", nil
	}, 1000)
	v, err := fn(context.Background(), nil)
	if err != nil || v != "ok" {
		t.Fatalf("fast tool should return value, got %v err=%v", v, err)
	}
}

func TestWrapTimeoutFires(t *testing.T) {
	fn := tools.WrapTimeout(func(ctx context.Context, in map[string]any) (any, error) {
		select {
		case <-time.After(500 * time.Millisecond):
			return "late", nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}, 50)
	_, err := fn(context.Background(), nil)
	var te *tools.TimeoutError
	if !errors.As(err, &te) || te.Code != tools.ToolTimeoutCode {
		t.Fatalf("expected TOOL_TIMEOUT, got %v", err)
	}
}

func TestWrapTimeoutNoBudgetPassthrough(t *testing.T) {
	called := false
	fn := tools.WrapTimeout(func(ctx context.Context, in map[string]any) (any, error) {
		called = true
		return 1, nil
	}, 0)
	_, _ = fn(context.Background(), nil)
	if !called {
		t.Fatal("zero budget should pass through unchanged")
	}
}

func TestWrapTimeoutRespectsParentCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	fn := tools.WrapTimeout(func(ctx context.Context, in map[string]any) (any, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}, 5000)
	cancel() // 父先取消
	_, err := fn(ctx, nil)
	var te *tools.TimeoutError
	if errors.As(err, &te) {
		t.Fatal("parent cancel must not map to this layer's TOOL_TIMEOUT")
	}
}
