// 本文件对应任务 M24：Tool Restriction allow/deny。
//
// 对齐上游：packages/core/tools/restriction
//
// 设计要点：
//   - Restriction 是某一作用域层的工具掩码（allow/exempt 与 deny 两个集合）；
//   - RestrictionSet 以「host → 最近作用域」的有序层栈承载多层掩码（层范式对齐 M03 scope：
//     host 最外层、作用域层越近越优先），解析采用 nearest-scope-wins：
//     从最近层向 host 层逐层查找，第一个「提及」该工具的层决定放行/拒绝；
//   - 因此「host 层 deny + scope 层 exempt」后工具恢复可用；两层相交（host deny + scope deny）
//     结果为拒绝；未提及则默认放行；
//   - 应用场景：Subagent 父限子能力（父在 host/外层 deny）、Preset 隐藏工具（Filter）。
package tools

import (
	"slices"
	"sync"
)

// Restriction 是单层的工具访问掩码。
//
//   - Allow：显式放行（exempt）的工具名集合；命中即覆盖外层 deny；
//   - Deny：显式拒绝的工具名集合。
type Restriction struct {
	// Allow 显式放行（exempt）的工具名集合。
	Allow []string `json:"allow,omitempty"`
	// Deny 显式拒绝的工具名集合。
	Deny []string `json:"deny,omitempty"`
}

// RestrictionAllow 便捷构造一个仅放行的掩码。
func RestrictionAllow(names ...string) Restriction {
	return Restriction{Allow: names}
}

// RestrictionDeny 便捷构造一个仅拒绝的掩码。
func RestrictionDeny(names ...string) Restriction {
	return Restriction{Deny: names}
}

// RestrictionLayer 是带层域名的掩码层。
type RestrictionLayer struct {
	// Key 层域名（host / session:xxx / user:xxx / subagent:xxx），仅审计用。
	Key string
	// Mask 本层掩码。
	Mask Restriction
}

// RestrictionSet 是分层工具掩码注册表。
type RestrictionSet struct {
	mu     sync.RWMutex
	layers []RestrictionLayer // 索引 0 为 host（最外层），索引越靠后作用域越近（越优先）
}

// NewRestrictionSet 创建空的分层掩码。
func NewRestrictionSet() *RestrictionSet {
	return &RestrictionSet{}
}

// Host 设置 host 层掩码（最外层）。
func (rs *RestrictionSet) Host(mask Restriction) {
	rs.SetLayer("host", mask)
}

// Scope 追加一个作用域层掩码（追加后成为最近的层，高于所有外层）。
// key 形如 "session:xxx" / "user:xxx" / "subagent:xxx"。
func (rs *RestrictionSet) Scope(key string, mask Restriction) {
	rs.SetLayer(key, mask)
}

// SetLayer 追加/更新指定层掩码。已存在的同名层被覆盖为最近层；新层追加到最末（最近）。
func (rs *RestrictionSet) SetLayer(key string, mask Restriction) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	for i := range rs.layers {
		if rs.layers[i].Key == key {
			rs.layers[i].Mask = mask
			return
		}
	}
	rs.layers = append(rs.layers, RestrictionLayer{Key: key, Mask: mask})
}

// Allowed 判断某工具是否允许使用（nearest-scope-wins）。
//
// 规则：从最近层向 host 层逐层查找，第一个提及该工具（出现在本层 Allow 或 Deny）的层
// 决定结果：Allow 命中 → true（exempt），Deny 命中 → false；所有层均未提及 → true（默认放行）。
func (rs *RestrictionSet) Allowed(tool string) bool {
	if rs == nil {
		return true
	}
	rs.mu.RLock()
	layers := make([]RestrictionLayer, len(rs.layers))
	copy(layers, rs.layers)
	rs.mu.RUnlock()
	// 最近层在末尾，向 host（开头）回溯。
	for i := len(layers) - 1; i >= 0; i-- {
		mask := layers[i].Mask
		if slices.Contains(mask.Allow, tool) {
			return true
		}
		if slices.Contains(mask.Deny, tool) {
			return false
		}
	}
	return true
}

// Denied 是 Allowed 的取反（供审计/断言）。
func (rs *RestrictionSet) Denied(tool string) bool {
	return !rs.Allowed(tool)
}

// Filter 应用掩码过滤工具列表（用于 Preset 隐藏工具 / Subagent 限制子能力）。
// 返回保留的（Allowed 为 true）工具，保持输入顺序。
func (rs *RestrictionSet) Filter(tools []*Tool) []*Tool {
	if rs == nil {
		return tools
	}
	out := make([]*Tool, 0, len(tools))
	for _, t := range tools {
		if t != nil && rs.Allowed(t.Name) {
			out = append(out, t)
		}
	}
	return out
}

// Layers 返回 host→最近的层浅拷贝（审计/测试用）。
func (rs *RestrictionSet) Layers() []RestrictionLayer {
	rs.mu.RLock()
	defer rs.mu.RUnlock()
	out := make([]RestrictionLayer, len(rs.layers))
	copy(out, rs.layers)
	return out
}