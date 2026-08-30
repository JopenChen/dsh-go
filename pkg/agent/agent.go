// Package agent 提供 Agent Registry 与 Turn/Step 双循环执行器。
//
// 对齐上游：packages/core/agent + agent-loop
//
// 设计要点：
//   - Agent 通过 Inbox 双队列（turn 队列）接收 Run/Followup 输入；
//   - 后台 worker 串行处理 turn（单 Turn 串行保证）：turn/start → claim → pre-step →
//     多 Step → turn-stopping → turn/end；
//   - 每个 Step：step/start → 派生 → agent/request → llm/stream → assistant/chunk* →
//     若含工具调用则经 M23 四级流水线执行 → step/end；
//   - step/tool 错误 → agent/error 写入日志，turn 以 interrupted 关闭；
//   - pending turn 超过队列容量 → ErrTurnQueueFull（reject）。
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/llm"
	"github.com/JopenChen/dsh-go/pkg/session"
	"github.com/JopenChen/dsh-go/pkg/sysprompt"
	"github.com/JopenChen/dsh-go/pkg/tools"
)

// ErrTurnQueueFull 表示 pending turn 队列已满（拒绝新的 Run/Followup）。
var ErrTurnQueueFull = errors.New("agent: turn queue full, reject pending turn")

// CancelCause 是取消原因分类（M18 会扩展为 5 类）。
type CancelCause string

// 取消原因枚举（M18 完整分类）。
const (
	CancelUser   CancelCause = "user"
	CancelParent CancelCause = "parent"
)

// Agent 是代理执行器。
type Agent struct {
	// ID 会话 ID（复用 SessionLog 的 ID）。
	ID brand.SessionID
	// log 事件溯源日志（M04）。
	log *session.SessionLog
	// sys system prompt 组装器（M09）。
	sys *sysprompt.Assembler
	// pipeline 工具四级流水线（M23）。
	pipeline *tools.Pipeline
	// adapter LLM 适配器（M07）。
	adapter llm.LLMAdapter

	// turnCh turn 队列（容量 2：1 运行中 + 1 排队）。
	turnCh chan *turnReq
	// stopCh 停止 worker 信号。
	stopCh chan struct{}
	// wg worker 生命周期。
	wg sync.WaitGroup
	// started 是否已启动 worker。
	started bool
	// mu 保护 started。
	mu sync.Mutex
	// pendingToolRounds 上一步产出的工具调用数（用于续步判断）。
	pendingToolRounds int
	// toolProvider 按工具名解析工具（测试可注入 mock；生产由 harness 注入）。
	toolProvider func(name string) *tools.Tool
}

// turnReq 是单个 turn 的请求。
type turnReq struct {
	// kind "run" / "followup"。
	kind string
	// input 用户输入。
	input string
	// done 完成通知（cap 1，非阻塞）。
	done chan error
}

// NewAgent 创建代理。
// adapter 可为 nil（测试时由 Start 前的 mock 注入）。
func NewAgent(id brand.SessionID, log *session.SessionLog, sys *sysprompt.Assembler, pipeline *tools.Pipeline, adapter llm.LLMAdapter) *Agent {
	a := &Agent{
		ID:       id,
		log:      log,
		sys:      sys,
		pipeline: pipeline,
		adapter:  adapter,
		turnCh:   make(chan *turnReq, 2),
		stopCh:   make(chan struct{}),
	}
	return a
}

// SetToolProvider 注入工具解析函数。
func (a *Agent) SetToolProvider(fn func(name string) *tools.Tool) {
	a.toolProvider = fn
}

// Log 返回事件溯源日志（供测试/上层读取派生状态）。
func (a *Agent) Log() *session.SessionLog {
	return a.log
}

// ensureStarted 确保 worker 已启动。
func (a *Agent) ensureStarted() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.started {
		a.started = true
		a.wg.Add(1)
		go a.loop()
	}
}

// loop 是 turn worker：串行处理队列中的 turn。
func (a *Agent) loop() {
	defer a.wg.Done()
	for {
		select {
		case <-a.stopCh:
			return
		case req := <-a.turnCh:
			a.runTurn(req)
		}
	}
}

// Run 提交一个新的 turn（异步入队）。
// 队列满时返回 ErrTurnQueueFull。
func (a *Agent) Run(input string) error {
	a.ensureStarted()
	req := &turnReq{kind: "run", input: input, done: make(chan error, 1)}
	select {
	case a.turnCh <- req:
		return nil
	default:
		return ErrTurnQueueFull
	}
}

// Followup 追加一个 followup turn（running 期间串行排队）。
func (a *Agent) Followup(input string) error {
	a.ensureStarted()
	req := &turnReq{kind: "followup", input: input, done: make(chan error, 1)}
	select {
	case a.turnCh <- req:
		return nil
	default:
		return ErrTurnQueueFull
	}
}

// Dispose 关闭代理：停止 worker。
func (a *Agent) Dispose() {
	a.mu.Lock()
	if !a.started {
		a.mu.Unlock()
		return
	}
	close(a.stopCh)
	a.started = false
	a.mu.Unlock()
	a.wg.Wait()
}

// Cancel 取消当前 turn（写入取消事件）。
func (a *Agent) Cancel(cause CancelCause) {
	_, _ = a.log.Append(session.TurnStoppingData{Reason: "cancel:" + string(cause)})
}

// runTurn 执行单个 turn（串行保证：一次只运行一个 turn）。
func (a *Agent) runTurn(req *turnReq) {
	var turnErr error
	defer func() {
		if req.done != nil {
			req.done <- turnErr
		}
	}()

	// turn/start
	if _, err := a.log.Append(session.TurnStartData{}); err != nil {
		turnErr = err
		return
	}

	// 记录 user/message（run 与 followup 均为用户输入）
	if _, err := a.log.Append(session.UserMessageData{Content: req.input, Source: req.kind}); err != nil {
		_ = a.failTurn(turnErr, err)
		turnErr = err
		return
	}

	// 多 Step 循环：每步调用 LLM，直到无工具调用或出错
	stepSeq := uint64(1)
	for {
		// step/start
		if _, err := a.log.Append(session.StepStartData{StepSeq: stepSeq}); err != nil {
			_ = a.failTurn(turnErr, err)
			turnErr = err
			return
		}

		err := a.runStep(req, stepSeq)
		// step/end
		_, _ = a.log.Append(session.StepEndData{StepSeq: stepSeq})

		if err != nil {
			// agent/error + turn 以 interrupted 关闭
			_ = a.failTurn(turnErr, err)
			turnErr = err
			return
		}

		// 检查本轮是否产出了工具调用且已执行；这里由 runStep 返回的
		// "是否有工具调用"决定是否继续下一 step
		cont := a.lastStepHadToolCall()
		if !cont {
			break
		}
		stepSeq++
	}

	// turn-stopping → turn/end (finished)
	_, _ = a.log.Append(session.TurnStoppingData{Reason: "finished"})
	_, _ = a.log.Append(session.TurnEndData{Reason: session.ReasonFinished})
}

// lastStepHadToolCall 返回上一步是否执行过工具调用（内部状态）。
func (a *Agent) lastStepHadToolCall() bool {
	return a.pendingToolRounds > 0
}

// failTurn 记录 agent/error 并以 interrupted 关闭 turn。
func (a *Agent) failTurn(prev, err error) error {
	_, _ = a.log.Append(session.AgentErrorData{Message: err.Error(), Pkg: "pkg/agent"})
	_, _ = a.log.Append(session.TurnEndData{Reason: session.ReasonInterrupted})
	return err
}

// runStep 执行单个 step：派生 → 请求 → 流式 → 工具执行。
func (a *Agent) runStep(req *turnReq, stepSeq uint64) error {
	// agent/request
	_, _ = a.log.Append(session.AgentRequestData{Provider: a.adapterName(), Model: a.modelName()})

	// 组装 prompt
	system := ""
	if a.sys != nil {
		system = a.sys.Assemble()
	}
	history := session.DeriveMessages(a.log.Events())
	messages := make([]llm.Message, 0, len(history))
	for _, m := range history {
		if m.Role == "user" {
			messages = append(messages, llm.NewUserMessage(m.Content))
		} else {
			messages = append(messages, llm.NewAssistantText(m.Content))
		}
	}

	var toolCalls []*llm.ToolCall
	_, err := a.adapter.Chat(context.Background(), llm.ChatRequest{
		Model:    a.modelName(),
		System:   system,
		Messages: messages,
		Tools:    a.toolSchemas(),
	}, func(chunk llm.StreamChunk) {
		switch chunk.Kind {
		case llm.ChunkText:
			_, _ = a.log.Append(session.AssistantChunkData{Text: chunk.Text})
		case llm.ChunkReasoning:
			_, _ = a.log.Append(session.AssistantReasoningData{Text: chunk.Reasoning})
		case llm.ChunkToolCall:
			if chunk.ToolCall != nil {
				toolCalls = append(toolCalls, chunk.ToolCall)
			}
		}
	})
	if err != nil {
		return fmt.Errorf("llm step failed: %w", err)
	}

	// 执行工具调用
	if len(toolCalls) > 0 {
		a.pendingToolRounds = len(toolCalls)
		for _, tc := range toolCalls {
			if err := a.executeTool(tc); err != nil {
				return err
			}
		}
		a.pendingToolRounds = len(toolCalls)
	} else {
		a.pendingToolRounds = 0
	}
	return nil
}

// pendingToolRounds 记录上一步的工具调用数量（用于续步判断）。
// 若上一步有工具调用则继续下一步（与上游 React 循环一致）。

// executeTool 经 M23 流水线执行单个工具调用。
func (a *Agent) executeTool(tc *llm.ToolCall) error {
	callID := brand.NewToolCallID(tc.ID)
	// tool/call
	if _, err := a.log.Append(session.ToolCallData{CallID: callID, Tool: tc.Name, Input: toRaw(tc.Input)}); err != nil {
		return err
	}

	if a.pipeline == nil {
		// 无流水线时直接记录结果
		_, _ = a.log.Append(session.ToolResultData{CallID: callID, IsError: false, Output: "(no pipeline)"})
		return nil
	}

	req := &tools.ToolCallRequest{CallID: callID, Tool: tc.Name, Input: tc.Input}
	res := a.pipeline.Run(context.Background(), req, a.findTool(tc.Name))
	_, err := a.log.Append(session.ToolResultData{
		CallID: callID, IsError: res.IsError, Output: stringify(res.Value),
	})
	return err
}

// adapterName / modelName 返回适配器信息（简化：固定值）。
func (a *Agent) adapterName() string {
	if a.adapter == nil {
		return "unknown"
	}
	return a.adapter.Name()
}

func (a *Agent) modelName() string {
	return "deepseek-chat"
}

// toolSchemas 从流水线中导出工具 schema（简化：空）。
func (a *Agent) toolSchemas() []llm.ToolSchema {
	return nil
}

// findTool 通过 agent 注入的工具解析函数解析工具；未注入时返回 nil。
func (a *Agent) findTool(name string) *tools.Tool {
	if a.toolProvider != nil {
		return a.toolProvider(name)
	}
	return nil
}

// jsonMarshal 便捷 JSON 序列化。
func jsonMarshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

// toRaw 将 map 参数转为 json.RawMessage。
func toRaw(input map[string]any) []byte {
	if input == nil {
		return nil
	}
	b, _ := jsonMarshal(input)
	return b
}

// stringify 将工具结果转为字符串。
func stringify(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := jsonMarshal(v)
	return string(b)
}
