// 文件系统观察策略（fs-observation-policy）：先读后写/编辑的新鲜度守卫。
package fs

import (
	"sync"
)

// FsObservation 是一次权威观察：present 携带版本，absent 表示确认缺失。
type FsObservation struct {
	Kind    string // "present" | "absent"
	Version FsVersion
}

// ObservationPolicy 记录每个 owner 对每个 target 的观察状态，并决定写/编辑守卫。
//
// 规则（对齐上游）：
//   - 未见 / absent → 写用 createIfAbsent；edit 未观察 → FS_NOT_OBSERVED，absent → FS_NOT_FOUND；
//   - present（读/写/编辑观察过某版本）→ 写用 replaceIfVersion，edit 用该版本守卫；
//   - 不执行任何文件 I/O。
type ObservationPolicy struct {
	mu    sync.Mutex
	state map[string]map[FsTargetKey]FsObservation // owner -> targetKey -> observation
}

// NewObservationPolicy 创建观察策略。
func NewObservationPolicy() *ObservationPolicy {
	return &ObservationPolicy{state: map[string]map[FsTargetKey]FsObservation{}}
}

// Record 记录一次观察。
func (p *ObservationPolicy) Record(owner string, key FsTargetKey, obs FsObservation) {
	p.mu.Lock()
	defer p.mu.Unlock()
	m, ok := p.state[owner]
	if !ok {
		m = map[FsTargetKey]FsObservation{}
		p.state[owner] = m
	}
	m[key] = obs
}

// Decision 是一次写/编辑的决策。
type Decision struct {
	// WriteIntent 写守卫（WriteDecision 用）。
	WriteIntent *FsWriteIntent
	// EditVersion 编辑守卫版本（EditDecision 用）。
	EditVersion *FsVersion
	// Deny 决策是否拒绝（edit 未观察/缺失时）。
	Deny bool
	// DenyCode 拒绝稳定码。
	DenyCode FsErrorCode
}

// DecideWrite 决定写意图：
//
//	未见/absent → createIfAbsent；present → replaceIfVersion。
func (p *ObservationPolicy) DecideWrite(owner string, key FsTargetKey) Decision {
	p.mu.Lock()
	obs, ok := p.state[owner][key]
	p.mu.Unlock()
	if !ok {
		return Decision{WriteIntent: &FsWriteIntent{Kind: "createIfAbsent"}}
	}
	if obs.Kind == "absent" {
		return Decision{WriteIntent: &FsWriteIntent{Kind: "createIfAbsent"}}
	}
	return Decision{WriteIntent: &FsWriteIntent{Kind: "replaceIfVersion", Version: obs.Version}}
}

// DecideEdit 决定编辑守卫：
//
//	present → 版本守卫；未见 → FS_NOT_OBSERVED；absent → FS_NOT_FOUND。
func (p *ObservationPolicy) DecideEdit(owner string, key FsTargetKey) Decision {
	p.mu.Lock()
	obs, ok := p.state[owner][key]
	p.mu.Unlock()
	if !ok {
		return Decision{Deny: true, DenyCode: FSErrNotObserved}
	}
	if obs.Kind == "absent" {
		return Decision{Deny: true, DenyCode: FSErrNotFound}
	}
	v := obs.Version
	return Decision{EditVersion: &v}
}

// DropOwner 丢弃某 owner 的全部观察（HMR/销毁安全）。
func (p *ObservationPolicy) DropOwner(owner string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.state, owner)
}

// ============================================================================
// 组合工具：把 obs-policy 接到 LocalFS 之上
// ============================================================================

// PolicyFS 把观察策略应用到 LocalFS：先读后写/编辑成为默认行为。
type PolicyFS struct {
	Local *LocalFS
	Obs   *ObservationPolicy
}

// NewPolicyFS 创建带观察策略的文件系统门面。
func NewPolicyFS(local *LocalFS, obs *ObservationPolicy) *PolicyFS {
	return &PolicyFS{Local: local, Obs: obs}
}

// ReflectObservation 读取后登记一次 present 观察（读/stat 调用方执行）。
func (p *PolicyFS) ReflectObservation(owner string, t FsTarget) {
	if v, ok := p.Local.ObservedVersion(t); ok {
		p.Obs.Record(owner, t.TargetKey, FsObservation{Kind: "present", Version: v})
	}
}

// Write 经观察策略守卫的文件写。
func (p *PolicyFS) Write(owner string, t FsTarget, content string) (FsWriteOutcome, error) {
	dec := p.Obs.DecideWrite(owner, t.TargetKey)
	return p.Local.WriteText(t, content, dec.WriteIntent)
}

// Edit 经观察策略守卫的文件编辑。
func (p *PolicyFS) Edit(owner string, t FsTarget, edit FsEditRequest) (FsEditOutcome, error) {
	dec := p.Obs.DecideEdit(owner, t.TargetKey)
	if dec.Deny {
		return FsEditOutcome{}, fsErr(dec.DenyCode, "edit %q blocked by observation policy", t.DisplayPath)
	}
	return p.Local.EditText(t, edit, dec.EditVersion)
}