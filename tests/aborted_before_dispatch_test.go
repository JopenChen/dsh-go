// Package tests 的派发前取消验收测试。
package tests

import (
	"context"
	"testing"

	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/tools"
)

func TestAbortedBeforeDispatch(t *testing.T) {
	executed := false
	tool := &tools.Tool{
		Name: "noop",
		Execute: func(_ context.Context, _ map[string]any) (any, error) {
			executed = true
			return nil, nil
		},
	}
	p := tools.NewPipeline().WithTool(tool)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 派发前已取消。

	req := &tools.ToolCallRequest{CallID: brand.NewToolCallID("c1"), Tool: "noop"}
	res := p.Run(ctx, req, tool)

	if executed {
		t.Fatal("tool body must not run")
	}
	if !res.IsError || res.ErrorCode != tools.AbortedBeforeDispatch {
		t.Fatalf("result = %+v", res)
	}
}
