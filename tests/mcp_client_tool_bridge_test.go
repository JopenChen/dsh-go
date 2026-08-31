// 本文件验证任务 S13：MCP Client → Tool Bridge。
//
// 覆盖：连接 mock MCP server → tools/list 自动映射为 ToolDefinition(tool)，调度执行
// tools/call 正常返回；未支持 schema 关键字 fail-closed；调用 isError 时返回错误。
package tests

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/JopenChen/dsh-go/pkg/mcp"
	"github.com/JopenChen/dsh-go/pkg/tools"
)

// mockMCP 是一个内存版 MCP JSON-RPC server（Transport 实现）。
type mockMCP struct {
	tools []mcp.MCPSchema
	calls int
}

func (m *mockMCP) RoundTrip(_ context.Context, req mcp.Request) (mcp.Response, error) {
	switch req.Method {
	case "initialize":
		return mcp.Response{ID: req.ID, Result: json.RawMessage(`{"protocolVersion":"2025-06-18","serverInfo":{"name":"mock"}}`)}, nil
	case "tools/list":
		b, _ := json.Marshal(map[string]any{"tools": m.tools})
		return mcp.Response{ID: req.ID, Result: b}, nil
	case "tools/call":
		m.calls++
		return mcp.Response{ID: req.ID, Result: json.RawMessage(`{"content":[{"type":"text","text":"mcp:ok"}],"isError":false}`)}, nil
	default:
		return mcp.Response{ID: req.ID, Result: json.RawMessage(`{}`)}, nil
	}
}

// TestMCPListToolsAutoRegister 验证连上 mock server 后端工具自动映射且可调用。
func TestMCPListToolsAutoRegister(t *testing.T) {
	ctx := context.Background()
	server := &mockMCP{tools: []mcp.MCPSchema{
		{Name: "read_file", Description: "读取文件",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string", "description": "文件路径"},
				},
				"required": []any{"path"},
			}},
		{Name: "list_dir", Description: "列目录",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}}},
	}}
	client := mcp.NewClient(server)
	if err := client.Initialize(ctx); err != nil {
		t.Fatal(err)
	}

	// tools/list 自动取回。
	schemas, err := client.ListTools(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(schemas) != 2 {
		t.Fatalf("应列出 2 个工具，实际 %d", len(schemas))
	}

	// 桥接为 ToolDefinition。
	bridge := mcp.NewBridge(client)
	tls, err := bridge.ToTools(schemas)
	if err != nil {
		t.Fatal(err)
	}
	if len(tls) != 2 {
		t.Fatalf("应映射 2 个 tool，实际 %d", len(tls))
	}
	if tls[0].Name != "read_file" || tls[0].Schema == nil {
		t.Fatalf("read_file 映射异常: %+v", tls[0])
	}

	// 正常调用 → tools/call 返回文本。
	out, err := tls[0].Execute(ctx, map[string]any{"path": "/a/b"})
	if err != nil {
		t.Fatal(err)
	}
	if s, ok := out.(string); !ok || s != "mcp:ok" {
		t.Fatalf("read_file 调用结果应为 mcp:ok，实际 %v", out)
	}
	if server.calls != 1 {
		t.Fatalf("tools/call 应被调用 1 次，实际 %d", server.calls)
	}
}

// TestMCPUnsupportedSchemaFailClosed 验证不支持的 schema 关键字 fail-closed。
func TestMCPUnsupportedSchemaFailClosed(t *testing.T) {
	_, err := mcp.MapSchema(map[string]any{"type": "object", "anyOf": []any{}})
	if err == nil {
		t.Fatal("含 anyOf 的 schema 应 fail-closed 报错")
	}
}

// TestMCPToolIsError 验证 MCP 返回 isError=true 时调用返回错误。
func TestMCPToolIsError(t *testing.T) {
	ctx := context.Background()
	client := mcp.NewClient(errorTransport{})
	_, err := client.CallTool(ctx, "failing", map[string]any{})
	if err == nil {
		t.Fatal("MCP isError=true 时 CallTool 应返回错误")
	}
}

// errorTransport 是恒错误 transport（占位）。
type errorTransport struct{}

func (errorTransport) RoundTrip(ctx context.Context, req mcp.Request) (mcp.Response, error) {
	return mcp.Response{ID: req.ID, Result: json.RawMessage(`{"content":[],"isError":true}`)}, nil
}

// TestMCPToolMappedSchema 验证 schema 映射出 properties/required/enum。
func TestMCPToolMappedSchema(t *testing.T) {
	node, err := mcp.MapSchema(map[string]any{
		"type":                  "object",
		"properties":            map[string]any{"mode": map[string]any{"type": "string", "enum": []any{"a", "b"}}},
		"required":              []any{"mode"},
		"additionalProperties":  false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if node.Type != tools.TypeObject {
		t.Fatalf("type 应为 object，实际 %s", node.Type)
	}
	if len(node.Required) != 1 || node.Required[0] != "mode" {
		t.Fatalf("required 应含 mode，实际 %v", node.Required)
	}
	mode := node.Properties["mode"]
	if mode == nil || len(mode.Enum) != 2 {
		t.Fatalf("mode 子 schema enum 应为 [a b]，实际 %+v", mode)
	}
	if node.AdditionalProperties == nil || *node.AdditionalProperties {
		t.Fatal("additionalProperties=false 应被映射")
	}
}