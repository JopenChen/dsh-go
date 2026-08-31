// 教程：MCP（Model Context Protocol）客户端 → 工具桥接（教学示例）。
//
// 本示例演示 dsh-go 如何把「MCP 服务器暴露的工具」自动映射成 Agent 可调用的
// pkg/tools.Tool——这是 Agent 接入外部工具生态（数据库、浏览器、内部服务……）
// 的标准姿势。
//
// 关键点：MCP 本身是一个「JSON-RPC 协议」，与具体传输方式（stdio / SSE / 内存）
// 解耦。这里我们用【内存 Transport】模拟一个 MCP 服务器，因此不需要真实进程、
// 不需要网络，随时可跑可读。
//
// 演示链路：
//   mcp.NewClient(transport)         构造客户端
//     → Initialize()                初始化握手
//     → ListTools()                 tools/list：列出服务器暴露的工具
//     → bridge.ToTools(...)         桥接：映射为 pkg/tools.Tool（含 schema 强校验）
//     → tool.Execute(...)           通过统一接缝直接调用
//
// 运行方式（仓库根目录）：
//   go run ./examples/mcp
//
// 对照阅读：pkg/mcp/mcp.go（Transport / Client / Bridge / MapSchema）
package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/JopenChen/dsh-go/pkg/mcp"
	"github.com/JopenChen/dsh-go/pkg/tools"
)

func main() {
	ctx := context.Background()

	// ------------------------------------------------------------------
	// 1. 用一个「内存 MCP 服务器」作为 Transport。
	//    它实现 mcp.Transport 接口（RoundTrip），按协议应答 initialize /
	//    tools/list / tools/call——相当于把真实 MCP 服务器搬进进程里。
	// ------------------------------------------------------------------
	srv := newMemoryServer()
	client := mcp.NewClient(srv)

	// ------------------------------------------------------------------
	// 2. MCP 初始化握手（协议版本协商）。
	// ------------------------------------------------------------------
	if err := client.Initialize(ctx); err != nil {
		panic(err)
	}
	fmt.Println("— MCP 初始化握手成功 —")

	// ------------------------------------------------------------------
	// 3. 列出服务器暴露的工具（tools/list）。
	// ------------------------------------------------------------------
	schemas, err := client.ListTools(ctx)
	if err != nil {
		panic(err)
	}
	fmt.Printf("— MCP 服务器暴露工具数: %d —\n", len(schemas))
	for _, s := range schemas {
		fmt.Printf("  · %s — %s\n", s.Name, s.Description)
	}

	// ------------------------------------------------------------------
	// 4. 桥接：把 MCP 工具映射为 Agent 可直接调用的 pkg/tools.Tool。
	//    这是核心一步——外部工具从此「自动出现在工具注册表」里，
	//    后续 Agent / 流水线无需知道它来自 MCP。
	// ------------------------------------------------------------------
	bridge := mcp.NewBridge(client)
	toolList, err := bridge.ToTools(schemas)
	if err != nil {
		panic(err)
	}

	// 5. 通过统一接缝直接调用（等价于 Agent 的 tool/execute 阶段）。
	fmt.Println("\n— 通过桥接后的 Tool 统一调用 —")
	for _, t := range toolList {
		// 每个 MCP 工具自带 JSON Schema（M48 强校验子集），这里只演示调用；
		// 真实场景会走 tools.NewPipeline().WithTool(t) 挂进流水线。
		out, err := t.Execute(ctx, demoInputFor(t.Name))
		if err != nil {
			fmt.Printf("  [%s] ❌ %v\n", t.Name, err)
			continue
		}
		fmt.Printf("  [%s] => %v\n", t.Name, out)
	}

	// ------------------------------------------------------------------
	// 6. 教学点：把桥接后的工具挂进完整工具流水线（四级 Waterfall）。
	//    这样它就和本地工具完全同权——可被 pre-execute 拦截、可被审批、
	//    可被限制，真正做到"外部工具 = 一等公民"。
	// ------------------------------------------------------------------
	fmt.Println("\n— 挂进 tools 流水线（与本地工具同权）—")
	p := tools.NewPipeline()
	for _, t := range toolList {
		p.WithTool(t)
	}
	// 流水线可直接执行（这里简化直接跑 execute，不演示完整 waterfall 日志）。
	fmt.Printf("  · 已注册 %d 个 MCP 工具到流水线\n", len(toolList))
}

// demoInputFor 返回每个演示工具的一个合法入参（用于 Execute 教学演示）。
func demoInputFor(name string) map[string]any {
	switch name {
	case "echo":
		return map[string]any{"text": "你好，MCP"}
	case "add":
		return map[string]any{"a": float64(2), "b": float64(3)}
	default:
		return map[string]any{}
	}
}

// ---------------------------------------------------------------------------
// 内存 MCP 服务器（实现 mcp.Transport）
//
// 真实场景中你会用 mcp 包默认的 stdio/SSE Transport 去连一个外部 MCP 进程；
// 这里为了教学可复现，把"服务器行为"直接写死在进程内，演示同样的协议往来。
// ---------------------------------------------------------------------------

// memoryServer 是内存版 MCP 服务器，按 JSON-RPC 应答三种方法。
type memoryServer struct {
	// 可把暴露的工具表放这里（真实服务器通常由服务端注册表提供）。
}

// newMemoryServer 构造内存服务器。
func newMemoryServer() *memoryServer {
	return &memoryServer{}
}

// RoundTrip 实现 mcp.Transport：收到请求后返回一条 JSON-RPC 响应。
func (s *memoryServer) RoundTrip(ctx context.Context, req mcp.Request) (mcp.Response, error) {
	switch req.Method {
	case "initialize":
		// 握手：返回协议版本与服务信息。
		return ok(req.ID, map[string]any{
			"protocolVersion": "2025-06-18",
			"serverInfo":      map[string]any{"name": "demo-memory-server", "version": "0.0.1"},
		}), nil

	case "tools/list":
		// 暴露两个演示工具：echo（回显）与 add（加法）。
		return ok(req.ID, map[string]any{"tools": []map[string]any{
			{
				"name":        "echo",
				"description": "回显输入文本",
				"inputSchema": map[string]any{
					"type":     "object",
					"required": []any{"text"},
					"properties": map[string]any{
						"text": map[string]any{"type": "string", "description": "要回显的文本"},
					},
				},
			},
			{
				"name":        "add",
				"description": "两数相加",
				"inputSchema": map[string]any{
					"type":     "object",
					"required": []any{"a", "b"},
					"properties": map[string]any{
						"a": map[string]any{"type": "number"},
						"b": map[string]any{"type": "number"},
					},
				},
			},
		}}), nil

	case "tools/call":
		// 根据工具名执行"真实逻辑"，把结果按 MCP 协议回包。
		name, _ := req.Params["name"].(string)
		args, _ := req.Params["arguments"].(map[string]any)
		switch name {
		case "echo":
			text, _ := args["text"].(string)
			return ok(req.ID, map[string]any{"content": []map[string]any{{"type": "text", "text": "echo: " + text}}}), nil
		case "add":
			a, _ := args["a"].(float64)
			b, _ := args["b"].(float64)
			return ok(req.ID, map[string]any{"content": []map[string]any{{"type": "text", "text": fmt.Sprintf("%.0f", a+b)}}}), nil
		default:
			return errResp(req.ID, fmt.Sprintf("unknown tool %q", name)), nil
		}
	}
	return errResp(req.ID, "method not found: "+req.Method), nil
}

// ok 构造一条成功响应。
func ok(id uint64, result map[string]any) mcp.Response {
	raw, _ := json.Marshal(result)
	return mcp.Response{ID: id, Result: raw}
}

// errResp 构造一条错误响应。
func errResp(id uint64, msg string) mcp.Response {
	raw, _ := json.Marshal(map[string]any{"message": msg})
	return mcp.Response{ID: id, Error: raw}
}