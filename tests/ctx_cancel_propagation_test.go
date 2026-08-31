// 本文件验证任务 H01：Agent 请求 ctx 透传（取消/超时传播）。
//
// 覆盖：
//   1. Pipeline 透传：把可取消父 ctx 传入 pipeline.Run，工具实现能收到该 ctx；父取消后
//      工具 ctx 变为 Done（取消/cancel 语义可传播进工具实现）。
//   2. Agent SetRunContext：绑定上游 ctx 后，工具/LLM 调用经由该 ctx（而非 Background）。
package tests

import (
	"context"
	"testing"
	"time"

	"github.com/JopenChen/dsh-go/pkg/agent"
	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/llm"
	"github.com/JopenChen/dsh-go/pkg/session"
	"github.com/JopenChen/dsh-go/pkg/tools"
)

// TestH01PipelinePropagatesCancel 验证 pipeline 把父 ctx 的取消传播进工具实现。
func TestH01PipelinePropagatesCancel(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())

	var observed bool
	tool := &tools.Tool{
		Name: "probe", Description: "probe",
		Execute: func(ctx context.Context, input map[string]any) (any, error) {
			select {
			case <-ctx.Done():
				observed = true
			case <-time.After(2 * time.Second):
			}
			return "ok", nil
		},
	}
	pipeline := tools.NewPipeline().WithTool(tool)

	// 第一次正常执行（父未取消）→ 工具 ctx 不应 Done。
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel() // 父取消
	}()
	res := pipeline.Run(parent, &tools.ToolCallRequest{
		CallID: brand.NewToolCallID("h1-1"), Tool: "probe", Input: map[string]any{},
	}, tool)
	if !observed {
		t.Fatal("父取消后工具 ctx 应观测到 Done（取消未传播进工具实现）")
	}
	if res.IsError {
		t.Fatalf("工具不应报错，仅应透传取消信号：%s", res.Error)
	}
}

// TestH01AgentPathCancellation 验证 Agent.executeTool 经 pipeline 调用工具时，工具收到的是
// Agent 绑定运行 ctx（SetRunContext）；父 ctx 取消后工具 ctx.Done 触发。
func TestH01AgentPathCancellation(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())

	sid := brand.NewSessionID("h1-agent")
	pipeline := tools.NewPipeline()
	var canceled bool
	tool := &tools.Tool{
		Name: "probe", Description: "probe",
		Execute: func(ctx context.Context, _ map[string]any) (any, error) {
			select {
			case <-ctx.Done():
				canceled = true
			case <-time.After(2 * time.Second):
			}
			return "ok", nil
		},
	}
	pipeline.WithTool(tool)

	adapter := &toolCallOnceLLM{toolName: "probe"}
	a := agent.NewAgent(sid, session.NewSessionLog(sid), nil, pipeline, adapter)
	a.SetToolProvider(func(name string) *tools.Tool { return tool })
	a.SetRunContext(parent, 0) // 绑定上游可取消 ctx
	_ = a.Run("do it")

	// 父取消：工具 ctx 应 Done。
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !canceled {
		time.Sleep(20 * time.Millisecond)
	}
	a.Dispose()
	if !canceled {
		t.Fatal("Agent 绑定的上游 ctx 取消后，经 pipeline 执行的工具应观测到 Done")
	}
}

// toolCallOnceLLM 是先产出一个工具调用、再完成的简易 LLM 桩。
type toolCallOnceLLM struct {
	toolName string
}

func (m *toolCallOnceLLM) Name() string { return "toolonce" }

func (m *toolCallOnceLLM) Chat(ctx context.Context, req llm.ChatRequest, cb func(llm.StreamChunk)) (llm.Usage, error) {
	if cb != nil {
		cb(llm.StreamChunk{Kind: llm.ChunkToolCall, ToolCall: &llm.ToolCall{ID: "call_1", Name: m.toolName, Input: map[string]any{}}})
		cb(llm.StreamChunk{Kind: llm.ChunkDone})
	}
	return llm.Usage{}, nil
}