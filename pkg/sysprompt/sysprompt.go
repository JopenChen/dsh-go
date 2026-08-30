// Package sysprompt 提供系统提示词（System Prompt）的 Section 注册与有序组装。
//
// 对齐上游：packages/core/system-prompt
//
// 设计要点：
//   - PromptSection{Name/Order/Text}：每个能力块是一个 Section，注册时给定固定的 order；
//   - Assembler 按 order 升序合并所有 Section，构成最终 system prompt；
//   - 顺序被写死为常量（对齐上游）：persona → policy → runtime-context-snapshot →
//     plan:policy → tools schema → skill catalog。顺序错误会导致模型行为漂移，
//     因此 order 必须来自常量字段而非运行时推导。
package sysprompt

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/JopenChen/dsh-go/pkg/tools"
)

// ============================================================================
// Section 顺序常量（对齐上游 packages/core/system-prompt，写死不可被运行时覆盖）
// ============================================================================

// Section 顺序常量（数值即上游原始 order）。
const (
	// SectionOrderPersona 角色设定。
	SectionOrderPersona = 100
	// SectionOrderPolicy 通用策略。
	SectionOrderPolicy = 200
	// SectionOrderRuntimeContext 运行时上下文快照。
	SectionOrderRuntimeContext = 300
	// SectionOrderPlanPolicy 计划模式策略（仅在 plan mode 注入）。
	SectionOrderPlanPolicy = 500
	// SectionOrderToolsSchema 工具 JSON Schema。
	SectionOrderToolsSchema = 600
	// SectionOrderSkillCatalog 技能目录（D3 纪律：稳定序列化 + change-only 注入）。
	SectionOrderSkillCatalog = 700
)

// ============================================================================
// Section & Assembler
// ============================================================================

// Section 是一个系统提示词块。
type Section struct {
	// Name 唯一标识（用于增删与调试）。
	Name string `json:"name"`
	// Order 固定排序值（读取常量字段，运行时不可被覆盖）。
	Order int `json:"order"`
	// Text 块内容。
	Text string `json:"text"`
}

// Assembler 是 Section 注册与有序组装器。
type Assembler struct {
	mu       sync.RWMutex
	sections []*Section
}

// New 创建空组装器。
func New() *Assembler {
	return &Assembler{sections: []*Section{}}
}

// Register 注册一个 Section（同名覆盖，order 以传入为准）。
func (a *Assembler) Register(name string, order int, text string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, s := range a.sections {
		if s.Name == name {
			s.Order = order
			s.Text = text
			return
		}
	}
	a.sections = append(a.sections, &Section{Name: name, Order: order, Text: text})
}

// Unregister 移除一个 Section。
func (a *Assembler) Unregister(name string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i, s := range a.sections {
		if s.Name == name {
			a.sections = append(a.sections[:i], a.sections[i+1:]...)
			return
		}
	}
}

// Has 判断某 Section 是否存在（用于 plan:policy 的审批开关判断）。
func (a *Assembler) Has(name string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, s := range a.sections {
		if s.Name == name {
			return true
		}
	}
	return false
}

// Sections 返回按 order 升序排序后的 Section 副本。
func (a *Assembler) Sections() []*Section {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]*Section, len(a.sections))
	copy(out, a.sections)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Order < out[j].Order })
	return out
}

// Assemble 将所有 Section 按 order 升序拼接为最终 system prompt。
// 相邻 Section 之间以一个空行分隔。
func (a *Assembler) Assemble() string {
	parts := make([]string, 0, len(a.sections))
	for _, s := range a.Sections() {
		if strings.TrimSpace(s.Text) == "" {
			continue
		}
		parts = append(parts, s.Text)
	}
	return strings.Join(parts, "\n\n")
}

// ============================================================================
// 工具 Schema 注入
// ============================================================================

// toolsSchemaEntry 是工具 schema 注入时单个工具的结构。
type toolsSchemaEntry struct {
	Type        string `json:"type"`
	Function    toolsFunction `json:"function"`
}

// toolsFunction 是 OpenAI 风格的 function 描述。
type toolsFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// ToolsSectionText 将工具列表渲染为 tools schema Section 文本（确定性输出）。
// 工具按 name 字典序排列；参数 schema 使用 M48 的确定性 Marshal。
func ToolsSectionText(toolList []*tools.Tool) (string, error) {
	if len(toolList) == 0 {
		return "", nil
	}
	// 按 name 字典序排序（D3 纪律：无随机顺序）
	sorted := append([]*tools.Tool(nil), toolList...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	entries := make([]toolsSchemaEntry, 0, len(sorted))
	for _, tl := range sorted {
		var params json.RawMessage
		if tl.Schema != nil {
			raw, err := tl.Schema.Marshal()
			if err != nil {
				return "", fmt.Errorf("sysprompt: marshal schema for tool %q: %w", tl.Name, err)
			}
			params = raw
		} else {
			params = json.RawMessage(`{"type":"object"}`)
		}
		entries = append(entries, toolsSchemaEntry{
			Type: "function",
			Function: toolsFunction{
				Name:        tl.Name,
				Description: tl.Description,
				Parameters:  params,
			},
		})
	}

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return "", fmt.Errorf("sysprompt: marshal tools schema: %w", err)
	}
	return "# 可用工具（Tools Schema）：\n" + string(data), nil
}
