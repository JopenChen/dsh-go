// Package mcp 提供 MCP（Model Context Protocol）客户端 → ToolDefinition 桥（任务 S13）。
//
// 对齐上游：packages 的 MCP 客户端接缝 + (原生 MCP 协议 spec)
//
// 设计要点：
//   - MCPTransport 抽象 JSON-RPC 上承载的传输方式（stdio / SSE……），生产可替换；
//   - MCPClient 按 MCP 协议初始化并列出工具（tools/list）与调用工具（tools/call）；
//   - ToolBridge 把 MCP 服务器暴露的工具自动映射为 pkg/tools.Tool（M48 强校验子集 schema
//     + Execute 包装），从而「工具自动出现在 ToolRegistry 并能正常调用」。
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/JopenChen/dsh-go/pkg/tools"
)

// ============================================================================
// MCP 工具 Schema
// ============================================================================

// MCPSchema 是一个由 MCP 服务器暴露的工具定义。
type MCPSchema struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema"` // JSON Schema（object 形式）
}

// ============================================================================
// Transport
// ============================================================================

// Request 是一条 JSON-RPC 请求。
type Request struct {
	ID     uint64         `json:"id"`
	Method string         `json:"method"`
	Params map[string]any `json:"params,omitempty"`
}

// Response 是一条 JSON-RPC 响应（result 原样保留）。
type Response struct {
	ID     uint64          `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  json.RawMessage `json:"error,omitempty"`
}

// Transport 封装 MCP JSON-RPC 往返（stdio / SSE 等实现）。
type Transport interface {
	// RoundTrip 发送一条请求，返回对应响应。
	RoundTrip(ctx context.Context, req Request) (Response, error)
}

// ============================================================================
// MCPClient
// ============================================================================

// Client 是 MCP 客户端：初始化 → 列工具 → 调工具。
type Client struct {
	transport Transport
	sessionID string
}

// NewClient 创建 MCP 客户端。
func NewClient(t Transport) *Client {
	return &Client{transport: t}
}

// Initialize 执行 MCP 初始化握手。
func (c *Client) Initialize(ctx context.Context) error {
	resp, err := c.transport.RoundTrip(ctx, Request{ID: 1, Method: "initialize", Params: map[string]any{
		"protocolVersion": "2025-06-18",
		"clientInfo":      map[string]any{"name": "dsh-go", "version": "0.1"},
	}})
	if err != nil {
		return err
	}
	if len(resp.Error) > 0 {
		return fmt.Errorf("mcp: initialize error: %s", resp.Error)
	}
	// 从 result 取 sessionId（可选）。
	var initRes struct {
		SessionID string `json:"sessionId,omitempty"`
	}
	_ = json.Unmarshal(resp.Result, &initRes)
	c.sessionID = initRes.SessionID
	return nil
}

// ListTools 列出 MCP 服务器暴露的全部工具。
func (c *Client) ListTools(ctx context.Context) ([]MCPSchema, error) {
	resp, err := c.transport.RoundTrip(ctx, Request{ID: 2, Method: "tools/list"})
	if err != nil {
		return nil, err
	}
	if len(resp.Error) > 0 {
		return nil, fmt.Errorf("mcp: list tools error: %s", resp.Error)
	}
	var out struct {
		Tools []MCPSchema `json:"tools"`
	}
	if err := json.Unmarshal(resp.Result, &out); err != nil {
		return nil, err
	}
	return out.Tools, nil
}

// CallTool 调用 MCP 工具并返回文本结果。
func (c *Client) CallTool(ctx context.Context, name string, input map[string]any) (string, error) {
	resp, err := c.transport.RoundTrip(ctx, Request{ID: 3, Method: "tools/call", Params: map[string]any{
		"name": name, "arguments": input,
	}})
	if err != nil {
		return "", err
	}
	if len(resp.Error) > 0 {
		return "", fmt.Errorf("mcp: call %q error: %s", name, resp.Error)
	}
	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError,omitempty"`
	}
	if err := json.Unmarshal(resp.Result, &out); err != nil {
		return "", err
	}
	var sb strings.Builder
	for _, c := range out.Content {
		if c.Type == "text" {
			sb.WriteString(c.Text)
		}
	}
	if out.IsError {
		return sb.String(), fmt.Errorf("mcp: tool %q returned isError", name)
	}
	return sb.String(), nil
}

// ============================================================================
// ToolBridge：MCP → pkg/tools.Tool
// ============================================================================

// Bridge 把 MCP 服务器工具映射为 pkg/tools.Tool。
type Bridge struct {
	client *Client
}

// NewBridge 创建桥接器。
func NewBridge(client *Client) *Bridge {
	return &Bridge{client: client}
}

// ToTool 将一条 MCP 工具 schema 映射为等价 tool（Execute 包装 CallTool）。
func (b *Bridge) ToTool(schema MCPSchema) (*tools.Tool, error) {
	node, err := MapSchema(schema.InputSchema)
	if err != nil {
		return nil, err
	}
	client := b.client
	name := schema.Name
	return &tools.Tool{
		Name:        name,
		Description: schema.Description,
		Schema:      node,
		Execute: func(ctx context.Context, input map[string]any) (any, error) {
			text, cerr := client.CallTool(ctx, name, input)
			if cerr != nil {
				return nil, cerr
			}
			return text, nil
		},
	}, nil
}

// ToolRegistry 把已列出的工具批量注册为 *tools.Tool。
func (b *Bridge) ToTools(schemas []MCPSchema) ([]*tools.Tool, error) {
	out := make([]*tools.Tool, 0, len(schemas))
	for _, s := range schemas {
		tl, err := b.ToTool(s)
		if err != nil {
			return nil, err
		}
		out = append(out, tl)
	}
	return out, nil
}

// MapSchema 把通用 JSON Schema map 映射为强校验子集 JsonSchemaNode（M48 子集）。
// 仅映射子集支持的关键字：type/properties/required/items/enum/description/const/default。
// 遇到不支持的（如 anyOf）返回错误（fail-closed）。
func MapSchema(m map[string]any) (*tools.JsonSchemaNode, error) {
	if m == nil {
		return nil, nil
	}
	node := &tools.JsonSchemaNode{}
	if desc, ok := m["description"].(string); ok {
		node.Description = desc
	}
	if tv, ok := m["type"].(string); ok {
		st, err := mapType(tv)
		if err != nil {
			return nil, err
		}
		node.Type = st
	}
	if err := checkUnsupported(m); err != nil {
		return nil, err
	}
	// enum / const / default
	if e, ok := m["enum"].([]any); ok {
		node.Enum = e
	}
	if c, ok := m["const"]; ok {
		node.Const = c
	}
	if d, ok := m["default"]; ok {
		node.Default = d
	}
	// object properties
	if props, ok := m["properties"].(map[string]any); ok {
		node.Properties = map[string]*tools.JsonSchemaNode{}
		for k, v := range props {
			if sub, ok := v.(map[string]any); ok {
				child, err := MapSchema(sub)
				if err != nil {
					return nil, err
				}
				node.Properties[k] = child
			}
		}
	}
	if req, ok := m["required"].([]any); ok {
		for _, r := range req {
			if s, ok := r.(string); ok {
				node.Required = append(node.Required, s)
			}
		}
	}
	if add, ok := m["additionalProperties"].(bool); ok && !add {
		f := false
		node.AdditionalProperties = &f
	}
	// array items
	if items, ok := m["items"].(map[string]any); ok {
		child, err := MapSchema(items)
		if err != nil {
			return nil, err
		}
		node.Items = child
	}
	return node, nil
}

// mapType 映射 MCP/JSON 类型到强校验子集类型。
func mapType(t string) (tools.SchemaType, error) {
	switch t {
	case "string":
		return tools.TypeString, nil
	case "number":
		return tools.TypeNumber, nil
	case "integer":
		return tools.TypeInteger, nil
	case "boolean":
		return tools.TypeBoolean, nil
	case "array":
		return tools.TypeArray, nil
	case "object":
		return tools.TypeObject, nil
	case "null":
		return tools.TypeNull, nil
	default:
		return "", fmt.Errorf("mcp: unsupported type %q", t)
	}
}

// checkUnsupported 对不支持的关键字 fail-closed。
func checkUnsupported(m map[string]any) error {
	for k := range m {
		switch k {
		case "type", "description", "properties", "required", "items", "enum",
			"const", "default", "additionalProperties":
			continue
		case "anyOf", "oneOf", "allOf", "not", "ref", "$ref":
			return fmt.Errorf("mcp: unsupported schema keyword %q (fail-closed)", k)
		}
	}
	return nil
}