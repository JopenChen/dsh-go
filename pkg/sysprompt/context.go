// 本文件对应任务 M10：PromptContext 动态注册与快照。
//
// 对齐上游：packages/core/system-prompt/context-prompt
//
// 与 PromptSection 的差异：
//   - Section → system prompt 前缀，静态声明（M09）；
//   - Context → user-msg 末尾追加的动态片段（如运行时上下文快照），可增删；
//   - compaction 删除老上下文后仍保留最新快照（change-only 持久化），保证可重建。
package sysprompt

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"sync"
)

// RuntimeContext 是动态上下文片段（注册表管理，增删即生效）。
type RuntimeContext struct {
	// Name 唯一标识。
	Name string
	// Order 排序（决定在 user-msg 末尾的拼接顺序）。
	Order int
	// Content 动态内容（由调用方每次提供最新快照）。
	Content string
}

// ContextRegistry 是 PromptContext 注册表 + 快照哈希。
type ContextRegistry struct {
	mu      sync.RWMutex
	ctxs    map[string]*RuntimeContext
	lastHash string
}

// NewContextRegistry 创建上下文注册表。
func NewContextRegistry() *ContextRegistry {
	return &ContextRegistry{ctxs: map[string]*RuntimeContext{}}
}

// Register 注册（或更新）一个上下文片段。返回是否发生变化（hash diff）。
func (r *ContextRegistry) Register(name string, order int, content string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.ctxs[name]; exists && r.ctxs[name].Content == content && r.ctxs[name].Order == order {
		return false // 无变化
	}
	r.ctxs[name] = &RuntimeContext{Name: name, Order: order, Content: content}
	return true
}

// Unregister 移除一个上下文片段。
func (r *ContextRegistry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.ctxs, name)
}

// Compute 按 order 升序拼接全部上下文片段（确定性），并计算稳定哈希。
func (r *ContextRegistry) Compute() (text string, hash string) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]*RuntimeContext, 0, len(r.ctxs))
	for _, c := range r.ctxs {
		items = append(items, c)
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Order < items[j].Order })
	parts := make([]string, 0, len(items))
	for _, c := range items {
		if c.Content != "" {
			parts = append(parts, c.Content)
		}
	}
	text = strings.Join(parts, "\n")
	if text == "" {
		hash = "" // 空快照
	} else {
		sum := sha256.Sum256([]byte(text))
		hash = hex.EncodeToString(sum[:])
	}
	return text, hash
}

// Snapshot 返回当前全部片段的稳定快照（change-only 持久化用）。
func (r *ContextRegistry) Snapshot() map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := map[string]string{}
	for name, c := range r.ctxs {
		out[name] = c.Content
	}
	return out
}

// LastHash 返回最近一次 Compute 的哈希（用于 change-only 注入判断）。
func (r *ContextRegistry) LastHash() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.lastHash
}

// SetLastHash 记录已注入的哈希（避免重复注入）。
func (r *ContextRegistry) SetLastHash(h string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastHash = h
}

// GoalRoundContext 是目标续轮的动态上下文（goal.active 时注入，完成时停止）。
type GoalRoundContext struct {
	// Active 目标是否处于活跃状态。
	Active bool
	// Round 当前轮次。
	Round int
	// GoalDesc 目标描述。
	GoalDesc string
}

// Render 渲染续轮提示文本（goal.active 时才有内容）。
func (g *GoalRoundContext) Render() string {
	if !g.Active {
		return "" // goal.complete 后自动停止注入
	}
	return "[目标续轮 #" + itoa(g.Round) + "] " + g.GoalDesc
}

// itoa 最小整数转字符串。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}