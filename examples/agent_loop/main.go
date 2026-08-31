// 本示例演示 dsh-go 的 Agent Turn/Step 双循环用法（进程内集成）。
//
// 流程：NewAgent 装配（SessionLog + SystemPrompt + 工具流水线 + LLM 适配器）→
//   Run(input) 提交一个 turn → Agent 走 turn/start → step/start → 组装 prompt →
//   LLM 流式 chunk → 若有 tool_call 则执行工具（step 续步）→ step/end → turn/end。
//
// 运行方式（仓库根目录）：
//   go run ./examples/agent_loop
//
// 说明：示例使用一个脚本化假 LLM 适配器【不发起真实网络】；接入真实模型时
// 换用 provider_deepseek.NewDeepSeek(store)（见 examples/usage）即可。
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/JopenChen/dsh-go/pkg/agent"
	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/llm"
	"github.com/JopenChen/dsh-go/pkg/session"
	"github.com/JopenChen/dsh-go/pkg/sysprompt"
	"github.com/JopenChen/dsh-go/pkg/tools"
)

// scriptedLLM 是脚本化假 LLM：按调用次数依次产出（可含 tool_call → text）。
// 不发起真实网络，便于 println 演示 turn/step 流转。
type scriptedLLM struct {
	replies []func(cb func(llm.StreamChunk)) // 每次 Chat 消费一个
	callN   int
}

func (d *scriptedLLM) Name() string { return "scripted-demo" }

func (d *scriptedLLM) Chat(ctx context.Context, req llm.ChatRequest, cb func(llm.StreamChunk)) (llm.Usage, error) {
	d.callN++
	if d.callN <= len(d.replies) {
		d.replies[d.callN-1](cb)
	}
	// 末尾发 done 分片（agent 据此收尾 message 与 turn）。
	cb(llm.StreamChunk{Kind: llm.ChunkDone})
	return llm.Usage{PromptTokens: int(len(req.Messages)), CompletionTokens: 2}, nil
}

func main() {
	ctx := context.Background()

	// 1. 会话日志（事件溯源；Agent 把 turn/step/assistant/工具 事件都写进来）。
	sl := session.NewSessionLog(brand.NewSessionID("agent-loop-demo"))

	// 2. System prompt 组装器 + 一个 persona section。
	sys := sysprompt.New()
	sys.Register("persona", 100, "你是一个使用 dsh-go 的演示 Agent。")

	// 3. 工具流水线 + 一个演示工具（e.g. "now" 返回当前时间，模拟工具执行续步）。
	p := tools.NewPipeline().WithTool(&tools.Tool{
		Name:        "now",
		Description: "返回当前时间",
		Execute: func(ctx context.Context, input map[string]any) (any, error) {
			return map[string]any{"time": time.Now().Format(time.RFC3339)}, nil
		},
	})

	// 4. 脚本化 LLM：第一次调用产出一个 tool_call（now），第二次产出最终文本 ——
	//    以此展示「turn 内先工具续步、再结束」的 Step 循环。
	adapter := &scriptedLLM{
		replies: []func(cb func(llm.StreamChunk)){
			func(cb func(llm.StreamChunk)) {
				cb(llm.StreamChunk{Kind: llm.ChunkToolCall, ToolCall: &llm.ToolCall{
					ID: "call-1", Name: "now", Input: map[string]any{},
				}})
			},
			func(cb func(llm.StreamChunk)) {
				cb(llm.StreamChunk{Kind: llm.ChunkText, Text: "当前时间已获取。"})
			},
		},
	}

	// 5. 装配 Agent；把工具名解析到流水线里的工具。
	a := agent.NewAgent(brand.NewSessionID("agent-loop-demo"), sl, sys, p, adapter)
	a.SetToolProvider(func(name string) *tools.Tool {
		// 简洁演示：仅支持 "now"；真实场景由 harness 注册表解析。
		if name == "now" {
			return &tools.Tool{
				Name: "now",
				Execute: func(ctx context.Context, input map[string]any) (any, error) {
					return map[string]any{"time": time.Now().Format(time.RFC3339)}, nil
				},
			}
		}
		return nil
	})

	// 6. 提交一个 turn（异步）。可绑定可取消父 ctx（H01）。
	a.SetRunContext(ctx, 30*time.Second)
	if err := a.Run("现在几点？"); err != nil {
		fmt.Printf("Run 失败: %v\n", err)
		return
	}

	// 7. 等待该 turn 写完（轮询日志直到出现 turn/end）。
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if hasTurnEnd(sl) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// 8. 关闭 Agent（停止 worker）。
	a.Dispose()

	// 9. 打印事件溯源流，展示 turn/step 双循环结构。
	printEvents(sl)
	fmt.Println("\nAgent Turn/Step 循环示例完成。")
}

// hasTurnEnd 判断日志里是否已出现 turn/end（以此判定单 turn 收尾）。
func hasTurnEnd(sl *session.SessionLog) bool {
	evs := sl.Events()
	for _, ev := range evs {
		if ev.Type == session.EventTurnEnd {
			return true
		}
	}
	return false
}

// printEvents 打印会话事件序列（便于观察双循环）。
func printEvents(sl *session.SessionLog) {
	seq := 0
	for _, ev := range sl.Events() {
		seq++
		_ = seq
		fmt.Printf("  #%d %s\n", ev.Seq, ev.Type)
	}
}