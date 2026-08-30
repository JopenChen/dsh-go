// Package presets 提供 Agent Preset（代理预设）接缝。
//
// 对齐上游：packages/core/agent-preset
//
// 设计要点：
//   - AgentPreset 描述一组独立的 tools/prompt 组合；不同 preset 挂载到不同 Agent 实例后，
//     每个 Agent 拥有独立的工具集与系统提示词；
//   - 注册中心按 standingKey 组织：同一 key 的 preset 组合叠加（composeFrom），
//     recompose 重建组合、select 按 key 选取。
package presets

import (
	"sort"
	"sync"

	"github.com/JopenChen/dsh-go/pkg/scope"
)

// StandingKey 是 preset 的常驻键（如 "code" / "research" / "default"）。
type StandingKey string

// AgentPreset 是一个代理预设组合。
type AgentPreset struct {
	// Key 常驻键。
	Key StandingKey
	// Tools 工具名集合（该 preset 可用的工具白名单）。
	Tools []string
	// Prompt 系统提示词片段。
	Prompt string
}

// PresetRegistry 是 Agent Preset 注册中心（基于 scope 分层，host 默认 + 覆盖）。
type PresetRegistry struct {
	mu        sync.RWMutex
	hostLayer *scope.Layer[*AgentPreset]
	layers    *scope.ScopedLayers[*AgentPreset]
}

// NewPresetRegistry 创建空注册中心。
func NewPresetRegistry() *PresetRegistry {
	host := scope.NewLayer[*AgentPreset](scope.Key("host"))
	return &PresetRegistry{
		hostLayer: host,
		layers:    scope.NewScopedLayers[*AgentPreset]().Push(host),
	}
}

// Mount 挂载一个 preset（到 host 层；同名覆盖，其余保留）。
func (r *PresetRegistry) Mount(p *AgentPreset) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hostLayer.Register(string(p.Key), p, 0)
}

// ComposeFrom 基于多个基础 preset 组合出一个新 preset（工具并集 + prompt 拼接）。
func ComposeFrom(key StandingKey, bases ...*AgentPreset) *AgentPreset {
	toolSet := map[string]struct{}{}
	var prompt string
	for _, b := range bases {
		if b == nil {
			continue
		}
		for _, t := range b.Tools {
			toolSet[t] = struct{}{}
		}
		if prompt != "" {
			prompt += "\n"
		}
		prompt += b.Prompt
	}
	tools := make([]string, 0, len(toolSet))
	for t := range toolSet {
		tools = append(tools, t)
	}
	sort.Strings(tools)
	return &AgentPreset{Key: key, Tools: tools, Prompt: prompt}
}

// Select 按 key 选取 preset（nearest-scope-wins）。
func (r *PresetRegistry) Select(key StandingKey) (*AgentPreset, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.layers.Get(string(key))
}

// StandingKeyFor 返回某 Agent 当前应使用的 standing key（简化：直接返回传入 key）。
func StandingKeyFor(key StandingKey) StandingKey {
	return key
}

// Recompose 重建注册中心中某 key 的组合（用新 preset 覆盖）。
func (r *PresetRegistry) Recompose(p *AgentPreset) {
	r.Mount(p)
}
