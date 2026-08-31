// 本示例演示如何在自己的 Go 服务中进程内使用 dsh-go（DeepSeek Agent 能力 SDK）。
//
// 覆盖核心能力：
//   1. 事件溯源会话（pkg/session）：append 事件 + fold 派生投影（消息/目标/待办/计划）；
//   2. 规划能力：Goal 工具集（goal.NewGoalToolset）+ slash 命令（pkg/commands）；
//   3. LLM Provider（pkg/llm/provider_deepseek）：构造 DeepSeek 适配器 + 观察连接池，
//      及官方对齐的失败分类（llm.ClassifyProviderDetail / NewProviderFailure）；
//   4. 持久化（pkg/persistence/jsonl）：落盘 / 读回。
//
// 运行方式（在仓库根目录）：
//   go run ./examples/usage
//
// 说明：示例为自包含内存态，不发起真实 LLM 网络调用；接入真实 API Key 时把
// DEEPSEEK_API_KEY 换成你的密钥并调用 provider 的 Chat 即可。
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/commands"
	"github.com/JopenChen/dsh-go/pkg/credentials"
	"github.com/JopenChen/dsh-go/pkg/goal"
	"github.com/JopenChen/dsh-go/pkg/llm"
	"github.com/JopenChen/dsh-go/pkg/llm/provider_deepseek"
	"github.com/JopenChen/dsh-go/pkg/persistence"
	"github.com/JopenChen/dsh-go/pkg/session"
)

func main() {
	ctx := context.Background()

	// ------------------------------------------------------------------
	// 1. 事件溯源会话：创建一条会话日志，追加事件。
	// ------------------------------------------------------------------
	sl := session.NewSessionLog(brand.NewSessionID("demo-session-1"))

	// 追加用户消息（唯一写路径 Append；时序不变量由 pkg/session 自动校验）。
	if _, err := sl.Append(session.UserMessageData{Content: "帮我评估 dsh-go 的用法"}); err != nil {
		panic(err)
	}
	// 追加一次 user→assistant 回环（真实流程中由 Agent 写入）。
	if _, err := sl.Append(session.AssistantMessageData{Content: "好的，我来分析。"}); err != nil {
		panic(err)
	}

	// fold 派生投影（只读，事件不被改变）。
	proj := session.FoldAll(sl.Events())
	fmt.Printf("— 事件数=%d, 派生消息=%d —\n", len(sl.Events()), len(proj.Messages))
	for _, m := range proj.Messages {
		fmt.Printf("  [%s] %s\n", m.Role, m.Content)
	}

	// 增量投影（H04）：Append 后 O(1) 取派生状态。
	sl.EnableIncrementalProjection()
	fmt.Printf("— 增量投影一层: goal.Present=%v, todo.Present=%v —\n",
		sl.Projection().Goal.Present, sl.Projection().Todo.Present)

	// ------------------------------------------------------------------
	// 2. 规划能力：Goal 工具集（state machine + 稳定错误码 + BlockReason）。
	// ------------------------------------------------------------------
	toolset := goal.NewGoalToolset(sl)
	// 列出 goal_* 工具名（接入 pkg/tools 四级流水线即可被 LLM 调用）。
	names := make([]string, 0, len(toolset.Tools()))
	for _, t := range toolset.Tools() {
		names = append(names, t.Name)
	}
	fmt.Printf("— goal 工具集: %v —\n", names)
	// 演示稳定错误码：maxRounds 非法 → GOAL_INVALID_MAX_ROUNDS。
	var ge *goal.GoalError
	if _, err := callTool(toolset, "goal_set_max_rounds", map[string]any{"maxRounds": float64(-1)}); err != nil {
		if asErr, ok := err.(*goal.GoalError); ok {
			ge = asErr
		}
	}
	if ge != nil {
		fmt.Printf("— 稳定错误码: %s —\n", ge.Code)
	}

	// ------------------------------------------------------------------
	// 3. slash 命令（/plan on 等，写 plan/mode 事件而非普通消息）。
	// ------------------------------------------------------------------
	cmd := commands.NewRegistry()
	out, err := cmd.Dispatch(ctx, "/plan on", sl)
	if err != nil {
		fmt.Printf("  /plan 执行错误: %v\n", err)
	} else {
		fmt.Printf("— 命令 /plan on => %s —\n", out)
	}

	// ------------------------------------------------------------------
	// 4. LLM Provider + 官方对齐失败分类。
	//    用内存凭证库注入 key 构造 DeepSeek 适配器（连接池已调优）。
	// ------------------------------------------------------------------
	store := credentials.NewMemoryStore()
	if err := store.Set(ctx, brand.NewCredentialRef("DEEPSEEK_API_KEY"), "sk-你的密钥"); err != nil {
		panic(err)
	}
	d := provider_deepseek.NewDeepSeek(store) // 可选 provider_deepseek.WithBaseURL(...)
	tp := d.Transport()
	fmt.Printf("— DeepSeek 连接池: MaxIdleConnsPerHost=%d, IdleConnTimeout=%v —\n",
		tp.MaxIdleConnsPerHost, tp.IdleConnTimeout)

	// 真实流式调用示例（注释：发起 Chat 拿 StreamChunk 与 Usage）：
	//   usage, err := d.Chat(ctx, llm.ChatRequest{Model: "deepseek-chat", Messages: ...},
	//       func(c llm.StreamChunk) { _ = c })

	// 失败分类：把 provider 报错文本映射到稳定 kind（R04 对齐官方）。
	kind := llm.ClassifyProviderDetail("Insufficient_quota for the workspace")
	fmt.Printf("— provider 文本分类 => kind=%q —\n", kind)
	f := llm.NewProviderFailure("quota exceeded")
	fmt.Printf("— NewProviderFailure => kind=%q —\n", f.Kind)

	// ------------------------------------------------------------------
	// 5. 持久化：把会话落盘到本地目录，再读回。
	// ------------------------------------------------------------------
	dir := filepath.Join(".", "tmp-dsh-demo")
	_ = os.RemoveAll(dir)
	if err := persistDemo(ctx, sl, dir); err != nil {
		fmt.Printf("  persistence 演示: %v\n", err)
	}
	_ = os.RemoveAll(dir)

	fmt.Println("\n示例完成：dsh-go 可在 Go 服务中直接集成 DeepSeek Agent 规划能力。")
}

// callTool 简化按名取工具并调用。
func callTool(ts *goal.GoalToolset, name string, input map[string]any) (any, error) {
	for _, t := range ts.Tools() {
		if t.Name == name {
			return t.Execute(context.Background(), input)
		}
	}
	return nil, fmt.Errorf("demo: tool %q not found", name)
}

// persistDemo 演示 JSONL 持久化：存 header+事件 → flush → load 读回。
func persistDemo(ctx context.Context, sl *session.SessionLog, dir string) error {
	backend, err := persistence.NewJSONL(dir,
		100, // batchSize：满 100 条即异步落盘
		persistence.WithShardCount(4))
	if err != nil {
		return err
	}
	defer backend.Close()
	id := sl.SessionID()

	// 写头部，再追加全部事件（batch 窗口内写入，Close 时兜底 flush）。
	if err := backend.SaveHeader(ctx, session.NewSessionHeader(id, "/workspace")); err != nil {
		return err
	}
	for _, ev := range sl.Events() {
		if err := backend.Append(ctx, id, ev); err != nil {
			return err
		}
	}
	// 显式 Flush（checkpoint）→ 立即落盘；随后 Load 读回。
	if err := backend.Flush(ctx, id); err != nil {
		return err
	}
	_, loaded, err := backend.Load(ctx, id)
	if err != nil {
		return err
	}
	fmt.Printf("— 持久化 read-back: %d 条事件 —\n", len(loaded))
	return nil
}