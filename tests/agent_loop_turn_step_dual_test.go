// 本文件对应任务 M08：Agent Registry + Turn/Step 双循环 Loop。
package tests

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/JopenChen/dsh-go/pkg/agent"
	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/llm"
	"github.com/JopenChen/dsh-go/pkg/session"
	"github.com/JopenChen/dsh-go/pkg/sysprompt"
	"github.com/JopenChen/dsh-go/pkg/tools"
)

// scriptedAdapter 是按脚本顺序返回流式分片的假 LLM 适配器。
type scriptedAdapter struct {
	mu        sync.Mutex
	scripts   [][]llm.StreamChunk // 每次 Chat 调用依次消费一个脚本
	order     []string            // 记录每次 Chat 调用的输入（串行验证用）
	callIdx   int                 // Chat 调用序号
	blockOn   chan struct{}       // 非 nil 时：首次 Chat 阻塞等待 release
	released  bool
	failOn    int // 第 N 次调用返回错误（从 1 开始）
}

func newScriptedAdapter() *scriptedAdapter {
	return &scriptedAdapter{}
}

// blockFirst 开启「首次 Chat 阻塞」模式（串行排队测试用）。
func (s *scriptedAdapter) blockFirst() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.blockOn == nil {
		s.blockOn = make(chan struct{})
	}
}

// script 追加一段脚本。
func (s *scriptedAdapter) script(chunks ...llm.StreamChunk) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.scripts = append(s.scripts, chunks)
}

func (s *scriptedAdapter) Name() string { return "scripted" }

func (s *scriptedAdapter) release() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.released {
		s.released = true
		close(s.blockOn)
	}
}

func (s *scriptedAdapter) Chat(ctx context.Context, req llm.ChatRequest, cb func(llm.StreamChunk)) (llm.Usage, error) {
	s.mu.Lock()
	// 记录输入（取最后一条 user 消息）
	lastUser := ""
	for _, m := range req.Messages {
		if m.Role == llm.RoleUser {
			for _, b := range m.Content {
				if b.Kind == llm.BlockText {
					lastUser = b.Text
				}
			}
		}
	}
	s.order = append(s.order, lastUser)
	s.callIdx++
	idx := s.callIdx
	block := s.blockOn
	failOn := s.failOn
	chunks := []llm.StreamChunk{}
	if idx <= len(s.scripts) {
		chunks = s.scripts[idx-1]
	}
	s.mu.Unlock()

	// 首次调用阻塞（串行验证）
	if block != nil && idx == 1 {
		select {
		case <-block:
		case <-ctx.Done():
			return llm.Usage{}, ctx.Err()
		}
	}

	// 故障注入
	if failOn != 0 && idx == failOn {
		return llm.Usage{}, errors.New("injected llm failure")
	}

	for _, c := range chunks {
		if cb != nil {
			cb(c)
		}
	}
	if cb != nil {
		cb(llm.StreamChunk{Kind: llm.ChunkDone})
	}
	return llm.Usage{PromptTokens: 1, CompletionTokens: 1}, nil
}

func (s *scriptedAdapter) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.callIdx
}

// buildAgent 构造一个带 echo 工具的 Agent。
func buildAgent(t *testing.T, adapter *scriptedAdapter) *agent.Agent {
	t.Helper()
	sl := session.NewSessionLog(brand.NewSessionID("agent_1"))
	sys := sysprompt.New()

	// echo 工具
	echo := &tools.Tool{
		Name:        "echo",
		Description: "echo input",
		Execute: func(ctx context.Context, input map[string]any) (any, error) {
			return map[string]any{"echo": input["msg"]}, nil
		},
	}
	pipeline := tools.NewPipeline().WithTool(echo)

	a := agent.NewAgent(brand.NewSessionID("agent_1"), sl, sys, pipeline, adapter)
	a.SetToolProvider(func(name string) *tools.Tool {
		if name == "echo" {
			return echo
		}
		return nil
	})
	return a
}

// waitFor 轮询等待条件成立（超时返回 false）。
func waitFor(cond func() bool) bool {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// countTurnEnds 统计日志中 turn/end 数量。
func countTurnEnds(sl *session.SessionLog) int {
	n := 0
	for _, ev := range sl.Events() {
		if ev.Type == session.EventTurnEnd {
			n++
		}
	}
	return n
}

// lastEventOfType 返回日志中最后一条指定类型事件。
func lastEventOfType(sl *session.SessionLog, typ session.EventType) (session.SessionEvent, bool) {
	events := sl.Events()
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type == typ {
			return events[i], true
		}
	}
	return session.SessionEvent{}, false
}

// TestAgentTurnStepTextOnly 验证纯文本 turn：turn/start→step→turn/end finished。
func TestAgentTurnStepTextOnly(t *testing.T) {
	adapter := newScriptedAdapter()
	adapter.script(llm.StreamChunk{Kind: llm.ChunkText, Text: "你好"})
	a := buildAgent(t, adapter)

	if err := a.Run("hello"); err != nil {
		t.Fatalf("Run 失败: %v", err)
	}
	if !waitFor(func() bool { return countTurnEnds(a.Log()) == 1 }) {
		t.Fatal("turn 未在超时内完成")
	}

	// turn/end 应为 finished
	ev, ok := lastEventOfType(a.Log(), session.EventTurnEnd)
	if !ok {
		t.Fatal("缺少 turn/end")
	}
	if td, ok := ev.Data.(session.TurnEndData); !ok || td.Reason != session.ReasonFinished {
		t.Fatalf("turn/end 应为 finished: %+v", ev.Data)
	}
}

// TestAgentTurnWithTool 验证工具调用 turn：step1 返回工具调用 → 执行 → step2 → turn/end。
func TestAgentTurnWithTool(t *testing.T) {
	adapter := newScriptedAdapter()
	// step1：返回工具调用 echo
	adapter.script(llm.StreamChunk{Kind: llm.ChunkToolCall, ToolCall: &llm.ToolCall{ID: "call_1", Name: "echo", Input: map[string]any{"msg": "hi"}}})
	// step2：返回最终文本
	adapter.script(llm.StreamChunk{Kind: llm.ChunkText, Text: "done"})
	a := buildAgent(t, adapter)

	if err := a.Run("use echo"); err != nil {
		t.Fatalf("Run 失败: %v", err)
	}
	if !waitFor(func() bool { return countTurnEnds(a.Log()) == 1 }) {
		t.Fatal("turn 未在超时内完成")
	}

	// 应存在 tool/call 与 tool/result
	hasCall, hasResult := false, false
	for _, ev := range a.Log().Events() {
		switch ev.Type {
		case session.EventToolCall:
			hasCall = true
		case session.EventToolResult:
			hasResult = true
		}
	}
	if !hasCall || !hasResult {
		t.Fatalf("应存在 tool/call 与 tool/result: call=%v result=%v", hasCall, hasResult)
	}
	// 两次 Chat 调用（step1 + step2）
	if adapter.callCount() != 2 {
		t.Fatalf("Chat 调用次数 = %d, want 2", adapter.callCount())
	}
}

// TestAgentFollowupSerialQueue 验证 running 期间 2 条 Followup 串行排队、第 3 条 reject。
func TestAgentFollowupSerialQueue(t *testing.T) {
	adapter := newScriptedAdapter()
	adapter.blockFirst() // 开启首调用阻塞以观察 running 状态
	// 为 3 个 turn 各准备文本脚本
	adapter.script(llm.StreamChunk{Kind: llm.ChunkText, Text: "r1"})
	adapter.script(llm.StreamChunk{Kind: llm.ChunkText, Text: "r2"})
	adapter.script(llm.StreamChunk{Kind: llm.ChunkText, Text: "r3"})
	a := buildAgent(t, adapter)

	// 首个 turn 会阻塞（验证 running 状态）
	if err := a.Run("first"); err != nil {
		t.Fatalf("Run 失败: %v", err)
	}
	// 等待 worker 进入 running（Chat 阻塞中）
	if !waitFor(func() bool { return adapter.callCount() >= 1 }) {
		t.Fatal("首个 Chat 未开始")
	}

	// running 期间 2 条 Followup 串行排队（队列容量 2）
	if err := a.Followup("f1"); err != nil {
		t.Fatalf("Followup f1 应排队成功: %v", err)
	}
	if err := a.Followup("f2"); err != nil {
		t.Fatalf("Followup f2 应排队成功: %v", err)
	}
	// 第 3 条 → reject（队列满）
	if err := a.Followup("f3"); err != nil {
		if err != agent.ErrTurnQueueFull {
			t.Fatalf("第 3 条应 reject 为 ErrTurnQueueFull, 实际 %v", err)
		}
	} else {
		t.Fatal("第 3 条 Followup 应被拒绝")
	}

	// 释放首个 turn → 三个 turn 串行执行
	adapter.release()
	if !waitFor(func() bool { return countTurnEnds(a.Log()) == 3 }) {
		t.Fatal("3 个 turn 未在超时内完成")
	}

	// 串行顺序验证
	adapter.mu.Lock()
	order := append([]string(nil), adapter.order...)
	adapter.mu.Unlock()
	if len(order) != 3 || order[0] != "first" || order[1] != "f1" || order[2] != "f2" {
		t.Fatalf("串行顺序异常: %v", order)
	}
}

// TestAgentStepErrorInterruptsTurn 验证 step 错误 → agent/error + turn interrupted。
func TestAgentStepErrorInterruptsTurn(t *testing.T) {
	adapter := newScriptedAdapter()
	adapter.failOn = 1 // 首次 Chat 返回错误
	a := buildAgent(t, adapter)

	if err := a.Run("trigger error"); err != nil {
		t.Fatalf("Run 入队失败: %v", err)
	}
	if !waitFor(func() bool { return countTurnEnds(a.Log()) == 1 }) {
		t.Fatal("turn 未在超时内完成")
	}

	// agent/error 应存在
	if _, ok := lastEventOfType(a.Log(), session.EventAgentError); !ok {
		t.Fatal("缺少 agent/error 事件")
	}
	// turn/end 应为 interrupted
	ev, _ := lastEventOfType(a.Log(), session.EventTurnEnd)
	if td, ok := ev.Data.(session.TurnEndData); !ok || td.Reason != session.ReasonInterrupted {
		t.Fatalf("turn/end 应为 interrupted: %+v", ev.Data)
	}
}

// TestAgentDispose 验证 Dispose 停止 worker。
func TestAgentDispose(t *testing.T) {
	adapter := newScriptedAdapter()
	adapter.script(llm.StreamChunk{Kind: llm.ChunkText, Text: "x"})
	a := buildAgent(t, adapter)
	_ = a.Run("x")
	if !waitFor(func() bool { return countTurnEnds(a.Log()) == 1 }) {
		t.Fatal("turn 未完成")
	}
	a.Dispose()
}
