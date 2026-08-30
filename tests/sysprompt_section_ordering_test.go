// 本文件对应任务 M09：SystemPrompt 组装 + Section 注册表。
package tests

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/JopenChen/dsh-go/pkg/sysprompt"
	"github.com/JopenChen/dsh-go/pkg/tools"
)

// TestSyspromptSectionOrdering 验证 persona → policy → runtime-context → plan:policy → tools 的顺序。
func TestSyspromptSectionOrdering(t *testing.T) {
	a := sysprompt.New()

	// 故意乱序注册，验证按 order 组装
	a.Register("tools", sysprompt.SectionOrderToolsSchema, "TOOLS_SECTION")
	a.Register("persona", sysprompt.SectionOrderPersona, "PERSONA_SECTION")
	a.Register("plan_policy", sysprompt.SectionOrderPlanPolicy, "PLAN_POLICY_SECTION")
	a.Register("policy", sysprompt.SectionOrderPolicy, "POLICY_SECTION")
	a.Register("runtime_ctx", sysprompt.SectionOrderRuntimeContext, "RUNTIME_CTX_SECTION")

	out := a.Assemble()

	// 各 section 应严格按 order 顺序出现
	idxPersona := strings.Index(out, "PERSONA_SECTION")
	idxPolicy := strings.Index(out, "POLICY_SECTION")
	idxCtx := strings.Index(out, "RUNTIME_CTX_SECTION")
	idxPlan := strings.Index(out, "PLAN_POLICY_SECTION")
	idxTools := strings.Index(out, "TOOLS_SECTION")

	if !(idxPersona < idxPolicy && idxPolicy < idxCtx && idxCtx < idxPlan && idxPlan < idxTools) {
		t.Fatalf("section 顺序错误: persona=%d policy=%d ctx=%d plan=%d tools=%d",
			idxPersona, idxPolicy, idxCtx, idxPlan, idxTools)
	}
}

// TestSyspromptRegisterOverwrite 验证同名 Section 覆盖 + Unregister 移除。
func TestSyspromptRegisterOverwrite(t *testing.T) {
	a := sysprompt.New()
	a.Register("policy", sysprompt.SectionOrderPolicy, "v1")
	a.Register("policy", sysprompt.SectionOrderPolicy, "v2")

	out := a.Assemble()
	if !strings.Contains(out, "v2") || strings.Contains(out, "v1") {
		t.Fatalf("同名覆盖失败: %q", out)
	}

	a.Unregister("policy")
	if strings.Contains(a.Assemble(), "v2") {
		t.Fatal("Unregister 后不应再出现 policy section")
	}
	if a.Has("policy") {
		t.Fatal("Unregister 后 Has 应为 false")
	}
}

// TestSyspromptOrderConstants 验证 order 常量值与上游一致（写死不可被运行时覆盖）。
func TestSyspromptOrderConstants(t *testing.T) {
	if sysprompt.SectionOrderPersona != 100 {
		t.Fatal("persona order 应为 100")
	}
	if sysprompt.SectionOrderPolicy != 200 {
		t.Fatal("policy order 应为 200")
	}
	if sysprompt.SectionOrderRuntimeContext != 300 {
		t.Fatal("runtime-context order 应为 300")
	}
	if sysprompt.SectionOrderPlanPolicy != 500 {
		t.Fatal("plan:policy order 应为 500")
	}
	if sysprompt.SectionOrderToolsSchema != 600 {
		t.Fatal("tools schema order 应为 600")
	}
}

// TestSyspromptToolsSchemaInjection 验证 tools schema 注入：确定性 + 字典序 + 参数 schema 合法。
func TestSyspromptToolsSchemaInjection(t *testing.T) {
	// 两个工具，故意乱序传入
	toolList := []*tools.Tool{
		{Name: "zebra_tool", Description: "z tool", Schema: mustCompile(t, `{"type":"object","properties":{"z":{"type":"string"}}}`)},
		{Name: "alpha_tool", Description: "a tool", Schema: mustCompile(t, `{"type":"object","properties":{"a":{"type":"integer"}}}`)},
	}

	text1, err := sysprompt.ToolsSectionText(toolList)
	if err != nil {
		t.Fatalf("ToolsSectionText 失败: %v", err)
	}
	text2, _ := sysprompt.ToolsSectionText(toolList)

	// 确定性：两次调用一致
	if text1 != text2 {
		t.Fatal("tools schema 输出不确定")
	}

	// 按 name 字典序（alpha_tool 在前）
	idxAlpha := strings.Index(text1, "alpha_tool")
	idxZebra := strings.Index(text1, "zebra_tool")
	if !(idxAlpha > 0 && idxZebra > idxAlpha) {
		t.Fatalf("工具应按字典序: %q", text1)
	}

	// 参数 schema 是合法 JSON
	// 提取 JSON 数组部分（第一行是标题，其余是 JSON）
	jsonStart := strings.Index(text1, "[")
	if jsonStart < 0 {
		t.Fatal("tools schema 应包含 JSON 数组")
	}
	var parsed []any
	if err := json.Unmarshal([]byte(text1[jsonStart:]), &parsed); err != nil {
		t.Fatalf("tools schema 不是合法 JSON: %v", err)
	}
	if len(parsed) != 2 {
		t.Fatalf("应包含 2 个工具: %d", len(parsed))
	}
}

// mustCompile 便捷编译 schema，失败即 panic。
func mustCompile(t *testing.T, raw string) *tools.JsonSchemaNode {
	t.Helper()
	node, err := tools.Compile([]byte(raw))
	if err != nil {
		t.Fatalf("Compile 失败: %v", err)
	}
	return node
}

// TestSyspromptEmpty 验证空组装器返回空串。
func TestSyspromptEmpty(t *testing.T) {
	a := sysprompt.New()
	if a.Assemble() != "" {
		t.Fatalf("空组装器应返回空串: %q", a.Assemble())
	}
}
