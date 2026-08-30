// 本文件对应任务 M16：Session Projections 投影注册中心。
//
// 对齐上游：packages/core/session-projection
//
// SDK 侧读取派生状态的唯一标准接口：禁止直接读 events，必须通过注册的投影读取。
// 每个投影维护一个 State，订阅 changelog 做增量更新；snapshot 与 fold* 结果一致。
package session

import (
	"sort"
	"sync"

	"github.com/JopenChen/dsh-go/pkg/brand"
)

// ProjectionState 是投影状态的类型约束。
type ProjectionState interface {
	// Equal 判断两个状态是否相等（用于 changelog 增量判断）。
	Equal(other ProjectionState) bool
}

// ProjectionDefinition 定义单个投影。
type ProjectionDefinition[S ProjectionState] struct {
	// ID 投影唯一标识。
	ID brand.ProjectionID
	// Init 初始状态。
	Init S
	// Apply 增量应用一条事件到状态。
	Apply func(state S, ev SessionEvent) S
	// Merge 从全部事件重新派生状态（重放；等价于 fold*）。
	Merge func(events []SessionEvent) S
}

// Projection 是一个已注册投影的运行实例。
type Projection[S ProjectionState] struct {
	def   *ProjectionDefinition[S]
	state S
}

// State 返回当前投影状态。
func (p *Projection[S]) State() S {
	return p.state
}

// projectionBox 是运行时投影的统一抽象盒（包内私有接口）。
type projectionBox interface {
	apply(ev SessionEvent) bool
	projectionID() brand.ProjectionID
	stateValue() any
	rebuild(events []SessionEvent)
}

// 以下方法使 *Projection[S] 实现 projectionBox。

func (p *Projection[S]) apply(ev SessionEvent) bool {
	if p.def == nil || p.def.Apply == nil {
		return false
	}
	newState := p.def.Apply(p.state, ev)
	changed := !newState.Equal(p.state)
	if changed {
		p.state = newState
	}
	return changed
}

func (p *Projection[S]) projectionID() brand.ProjectionID {
	return p.def.ID
}

func (p *Projection[S]) stateValue() any { return p.state }

func (p *Projection[S]) rebuild(events []SessionEvent) {
	if p.def == nil {
		return
	}
	if p.def.Merge != nil {
		p.state = p.def.Merge(events)
		return
	}
	p.state = p.def.Init
	for _, ev := range events {
		if p.def.Apply != nil {
			p.state = p.def.Apply(p.state, ev)
		}
	}
}

// Registry 是投影注册中心。
type Registry struct {
	mu        sync.RWMutex
	defs      map[string]projectionBox // ProjectionID.Raw() -> projectionBox
	seq       uint64
	changelog []ProjectionChange
}

// ProjectionChange 是投影 changelog 中的一条变更。
type ProjectionChange struct {
	// ProjectionID 变更的投影。
	ProjectionID brand.ProjectionID `json:"projectionId"`
	// Seq changelog 序号。
	Seq uint64 `json:"seq"`
	// EventSeq 触发变更的事件 seq。
	EventSeq uint64 `json:"eventSeq"`
	// Changed 状态是否发生变化。
	Changed bool `json:"changed"`
}

// NewProjectionRegistry 创建投影注册中心。
func NewProjectionRegistry() *Registry {
	return &Registry{defs: map[string]projectionBox{}}
}

// RegisterProjection 注册一个投影定义（顶层泛型函数，因 Go 方法不支持类型参数）。
func RegisterProjection[S ProjectionState](r *Registry, def *ProjectionDefinition[S]) (*Projection[S], error) {
	if def == nil || def.ID.IsZero() {
		return nil, &projectionError{"nil or zero-id projection"}
	}
	p := &Projection[S]{def: def, state: def.Init}
	r.mu.Lock()
	r.defs[def.ID.Raw()] = p
	r.mu.Unlock()
	return p, nil
}

// projectionError 是投影错误。
type projectionError struct{ msg string }

func (e *projectionError) Error() string { return "projection: " + e.msg }

// ApplyEvents 将一批新事件增量应用到全部投影，并生成 changelog。
func (r *Registry) ApplyEvents(events []SessionEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, ev := range events {
		r.seq++
		for _, box := range r.defs {
			changed := box.apply(ev)
			r.changelog = append(r.changelog, ProjectionChange{
				ProjectionID: box.projectionID(),
				Seq:          r.seq,
				EventSeq:     ev.Seq,
				Changed:      changed,
			})
		}
	}
}

// Changelog 返回 changelog 快照（按产生顺序）。
func (r *Registry) Changelog() []ProjectionChange {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ProjectionChange, len(r.changelog))
	copy(out, r.changelog)
	return out
}

// SnapshotAll 返回全部投影的当前状态快照（按投影 ID 排序）。
func (r *Registry) SnapshotAll() map[brand.ProjectionID]any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := map[brand.ProjectionID]any{}
	keys := make([]string, 0, len(r.defs))
	for id := range r.defs {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	for _, id := range keys {
		out[brand.NewProjectionID(id)] = r.defs[id].stateValue()
	}
	return out
}

// Rebuild 从全部事件重放重建全部投影（等价 fold*，供一致性校验）。
func (r *Registry) Rebuild(events []SessionEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, box := range r.defs {
		box.rebuild(events)
	}
}