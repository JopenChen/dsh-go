// 本文件复刻官方 packages/core/agent/src/model-selection.ts 的核心语义。
//
// 官方用两个 waterfall（system-prompt/assemble、agent/request）把"可变的模型选择"
// 耦合到 prompt 装配与请求路由：prompt 装配前快照所选模型，请求时应用该快照，
// 从而一次并发切换只在后续 step 生效，不会把"装配面"与"请求面"撕裂成两个模型。
//
// dsh-go 的 sysprompt 是同步 Assembler（非 Cordis 异步事件），因此这里不照搬事件
// 接线，而是把 current/assembled 双快照固化为一个并发安全的小状态机，由 Agent 在
// 同步装配/请求的对应位置显式调用 Capture / Apply。
package agent

import "sync"

// ModelSelection 是为一次活跃 Agent 选定的完整 provider/model 及可选推理努力。
type ModelSelection struct {
	// Provider 已注册的提供方路由。
	Provider string
	// Model 提供方拥有的模型 id。
	Model string
	// ReasoningEffort 适配器拥有的推理努力；为空表示采用提供方/默认行为。
	ReasoningEffort string
}

// ModelSelectionRef 是可变模型选择 + 为当前 step 捕获的快照。
type ModelSelectionRef struct {
	mu sync.RWMutex
	// current 下一个进入 prompt 装配的 step 将要使用的选择。
	current *ModelSelection
	// assembled 当前 step 进入 prompt 装配时捕获的选择。
	assembled *ModelSelection
}

// NewModelSelectionRef 创建空选择器。
func NewModelSelectionRef() *ModelSelectionRef {
	return &ModelSelectionRef{}
}

// Select 更新"下一步"要用的选择（可在运行期并发切换；nil 表示清除选择、用默认）。
func (r *ModelSelectionRef) Select(sel *ModelSelection) {
	r.mu.Lock()
	r.current = cloneSelection(sel)
	r.mu.Unlock()
}

// Current 返回当前选择的拷贝。
func (r *ModelSelectionRef) Current() *ModelSelection {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return cloneSelection(r.current)
}

// Capture 在一次 step 的 prompt 装配边界调用：把当前 current 冻结为 assembled，
// 返回该快照（即便 current 为 nil 也返回 nil，表示本 step 不指定模型）。
func (r *ModelSelectionRef) Capture() *ModelSelection {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.assembled = cloneSelection(r.current)
	return cloneSelection(r.assembled)
}

// Assembled 返回当前 step 已捕获快照的拷贝。
func (r *ModelSelectionRef) Assembled() *ModelSelection {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return cloneSelection(r.assembled)
}

// RequestConfig 是一次模型请求中与模型路由相关的可改字段。
type RequestConfig struct {
	Provider        string
	Model           string
	ReasoningEffort string
}

// Apply 用当前 step 捕获的快照改写请求配置，对齐官方 agent/request 语义：
//   - 没有捕获选择 → 原样返回（继承配置）；
//   - 有捕获 → 先清除继承的 reasoningEffort，再应用 provider/model；
//     捕获自带 effort 则写入，为空则保持清除（恢复所选模型的提供方/默认行为）。
func (r *ModelSelectionRef) Apply(cfg RequestConfig) RequestConfig {
	r.mu.RLock()
	sel := cloneSelection(r.assembled)
	r.mu.RUnlock()

	if sel == nil {
		return cfg
	}
	out := cfg
	out.ReasoningEffort = "" // 清除继承努力
	out.Provider = sel.Provider
	out.Model = sel.Model
	if sel.ReasoningEffort != "" {
		out.ReasoningEffort = sel.ReasoningEffort
	}
	return out
}

// cloneSelection 深拷贝选择，避免内外共享指针。
func cloneSelection(s *ModelSelection) *ModelSelection {
	if s == nil {
		return nil
	}
	cp := *s
	return &cp
}
