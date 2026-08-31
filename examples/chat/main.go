// 本示例演示如何真实调用 DeepSeek 大模型进行多轮对话。
//
// 与 usage 示例（内存自包含、不发真实网络请求）不同，本示例会真正调用
// DeepSeek 官方 REST + SSE 接口，覆盖：
//  1. 从环境变量读取 DEEPSEEK_API_KEY，构造基于内存凭证库的 DeepSeek provider；
//  2. 从环境变量读取 DEEPSEEK_MODEL 指定模型（默认 deepseek-chat，可换 deepseek-reasoner）；
//  3. 组装多轮对话消息（system + user + assistant + user）并带上温度/最大长度设置；
//  4. 通过 LLMAdapter.Chat 发起流式对话，逐分片打印「推理内容 / 正文 / 工具调用」；
//  5. 打印最终 usage（含缓存命中/未命中 token 数）。
//
// 模型指定：
//   通过环境变量 DEEPSEEK_MODEL 指定，未设置时默认 deepseek-chat：
//     deepseek-chat      通用对话模型（默认）
//     deepseek-reasoner  推理模型，流式时额外产生 ChunkReasoning（思维链）分片
//
// 运行方式（先设置密钥，再在仓库根目录执行）：
//   Windows PowerShell：
//     $env:DEEPSEEK_API_KEY = "sk-你的密钥"
//     $env:DEEPSEEK_MODEL  = "deepseek-reasoner"   # 可选，切换模型
//     go run ./examples/chat
//   Linux/macOS：
//     DEEPSEEK_API_KEY="sk-你的密钥" DEEPSEEK_MODEL="deepseek-reasoner" go run ./examples/chat
//
// 说明：请将占位密钥替换为你自己的 DeepSeek API Key（https://platform.deepseek.com）。
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/credentials"
	"github.com/JopenChen/dsh-go/pkg/llm"
	"github.com/JopenChen/dsh-go/pkg/llm/provider_deepseek"
)

func main() {
	// ------------------------------------------------------------------
	// 1. 从环境变量读取真实 API Key。
	//    基于内存后端构造凭证库，并显式写入 DEEPSEEK_API_KEY，
	//    之后 provider 每轮请求都会 Resolve 一次，保证改值立即生效。
	// ------------------------------------------------------------------
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		fmt.Println("缺少 DEEPSEEK_API_KEY 环境变量，示例结束。")
		fmt.Println("请先设置：PowerShell 下执行  $env:DEEPSEEK_API_KEY=\"sk-你的密钥\"")
		os.Exit(1)
	}

	store := credentials.NewMemoryStore()
	if err := store.Set(context.Background(), brand.NewCredentialRef("DEEPSEEK_API_KEY"), apiKey); err != nil {
		fmt.Printf("写入凭证失败: %v\n", err)
		os.Exit(1)
	}

	// ------------------------------------------------------------------
	// 2. 构造 DeepSeek provider。
	//    默认使用生产级连接池（MaxIdleConnsPerHost=100、IdleConnTimeout=90s）；
	//    如需自定义模型基地址/连接池，可用 provider_deepseek.WithBaseURL / WithTransport 等选项。
	// ------------------------------------------------------------------
	d := provider_deepseek.NewDeepSeek(store)
	tp := d.Transport()
	fmt.Printf("— DeepSeek 连接池: MaxIdleConnsPerHost=%d, IdleConnTimeout=%v —\n",
		tp.MaxIdleConnsPerHost, tp.IdleConnTimeout)

	// ------------------------------------------------------------------
	// 3. 组装多轮对话消息。
	//    ChatRequest.Messages 使用 llm.Message{Role, []ContentBlock}；
	//    System 作为独立字段交给 provider 组装成 system 角色消息。
	// ------------------------------------------------------------------
	messages := []llm.Message{
		llm.NewUserMessage("用一句话介绍你自己（你好，我是 dsh-go 示例）。"),
		// 模拟上一条助手回复，拼出「user→assistant→user」多轮结构。
		llm.NewAssistantText("你好！我是由 DeepSeek 模型驱动的 dsh-go 示例助手。"),
		llm.NewUserMessage("那你能用中文再给我讲一条 DeepSeek 官方的实用技巧吗？"),
	}

	// 从环境变量读取模型名；未设置时默认 deepseek-chat。
	model := os.Getenv("DEEPSEEK_MODEL")
	if model == "" {
		model = "deepseek-chat" // 通用对话模型（可换 deepseek-reasoner 推理模型）
	}

	req := llm.ChatRequest{
		// 模型名：默认为通用对话模型 deepseek-chat；通过环境变量 DEEPSEEK_MODEL 切换。
		Model:       model,
		System:      "你是一个非常友好、简洁的助手。请用中文回答，每次答复不超过 3 句话。",
		Messages:    messages,
		Temperature: 0.7,
		MaxTokens:   512,
	}

	// ------------------------------------------------------------------
	// 4. 发起流式对话。
	//    通过带超时的 ctx 控制整体时长（取消时 provider 会正确返回稳定失败分类）。
	//    回调里按分片类型分派：
	//      ChunkReasoning → 推理内容（仅 deepseek-reasoner 产生）；
	//      ChunkText      → 正文；
	//      ChunkToolCall  → 模型请求调用工具（若请求里带了 Tools 才会出现）。
	//    收到 ChunkDone 即流结束，随后 Chat 返回最终 usage。
	// ------------------------------------------------------------------
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	fmt.Printf("\n— 当前模型: %s，正在流式回复 —\n", model)
	usage, err := d.Chat(ctx, req, func(c llm.StreamChunk) {
		switch c.Kind {
		case llm.ChunkReasoning:
			// 推理过程以灰色前缀展示，便于区分。
			fmt.Printf("🧠 %s", c.Reasoning)
		case llm.ChunkText:
			fmt.Print(c.Text)
		case llm.ChunkToolCall:
			if c.ToolCall != nil {
				fmt.Printf("\n[工具调用] %s(%v)\n", c.ToolCall.Name, c.ToolCall.Input)
			}
		case llm.ChunkDone:
			fmt.Println("\n[流结束]")
		}
	})
	fmt.Println() // 结束正文换行

	if err != nil {
		// 失败已带稳定分类：overload / rate-limit / quota / invalid-credential 等，
		// 便于上层做重试或给用户更友好的提示。
		if f, ok := err.(*llm.LlmFailure); ok {
			fmt.Printf("\n❌ 调用失败 [kind=%s]: %s\n", f.Kind, f.Message)
		} else {
			fmt.Printf("\n❌ 调用失败: %v\n", err)
		}
		os.Exit(1)
	}

	// ------------------------------------------------------------------
	// 5. 打印用量统计。
	// ------------------------------------------------------------------
	fmt.Printf("\n— 用量 —\n")
	fmt.Printf("  prompt tokens      = %d\n", usage.PromptTokens)
	fmt.Printf("  completion tokens  = %d\n", usage.CompletionTokens)
	fmt.Printf("  cache hit tokens   = %d\n", usage.PromptCacheHitTokens)
	fmt.Printf("  cache miss tokens  = %d\n", usage.PromptCacheMissTokens)

	// ------------------------------------------------------------------
	// 6. 附带一个交互式续聊（可选，按回车/输入 exit 退出）。
	//    展示真正的多轮对话：每轮把模型的 assistant 回复收集并追加进上下文，
	//    保证 messages 始终满足「user ↔ assistant」交替，避免 ReAct 时序校验报错。
	// ------------------------------------------------------------------
	fmt.Println("\n— 交互式续聊（输入提问继续，输入 exit 退出）—")
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("\n你> ")
		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" || strings.EqualFold(line, "exit") {
			break
		}
		// 追加用户消息（保留会话上下文）。
		messages = append(messages, llm.NewUserMessage(line))
		req.Messages = messages

		// 每轮创建独立的超时预算（外层 60s 是首次调用专用），避免长会话中途超时。
		roundCtx, roundCancel := context.WithTimeout(context.Background(), 30*time.Second)

		// 收集本轮助手回复文本，用于追加进上下文，形成真正的多轮交替。
		var sb strings.Builder
		usage, err = d.Chat(roundCtx, req, func(c llm.StreamChunk) {
			if c.Kind == llm.ChunkText {
				sb.WriteString(c.Text)
				fmt.Print(c.Text)
			}
		})
		fmt.Println()
		if err != nil {
			if f, ok := err.(*llm.LlmFailure); ok {
				fmt.Printf("❌ [%s] %s\n", f.Kind, f.Message)
			} else {
				fmt.Printf("❌ %v\n", err)
			}
			break
		}
		// 把助手回复追加进上下文，形成连续的多轮对话。
		if sb.Len() > 0 {
			messages = append(messages, llm.NewAssistantText(sb.String()))
		}
		// 释放本轮超时预算，避免长会话积累未回收的 goroutine/定时器。
		roundCancel()
	}
}
