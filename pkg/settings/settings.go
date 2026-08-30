// Package settings 提供设置接缝（ctx.settings）：分层作用域 + 路径操作 + CAS + 密钥脱敏。
//
// 对齐上游：packages/settings/settings + settings-file
//
// 设计要点：
//   - SettingsScope 基于 pkg/scope 分层：host 层定义默认值，session 层可覆盖（nearest-wins）；
//   - 更新通过 SettingsPathOp{set/unset} 表达，携带 expectedRevision 做 CAS 乐观锁；
//   - describe(redactSecrets:true) 对 secret 路径只输出 set/unset 操作位，value 全脱敏，
//     避免把密钥明文泄漏给模型或日志。
package settings

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/JopenChen/dsh-go/pkg/scope"
)

// Path 是设置路径（如 "sandbox.mode"，点分）。
type Path string

// splitPath 将点分路径拆为段。
func splitPath(p Path) []string {
	if p == "" {
		return nil
	}
	return strings.Split(string(p), ".")
}

// ============================================================================
// SettingsNamespace
// ============================================================================

// SettingsNamespace 描述一个设置命名空间（含哪些路径属于 secret）。
type SettingsNamespace struct {
	// Name 命名空间名。
	Name string
	// SecretPaths 属于密钥的路径集合（describe 时脱敏）。
	SecretPaths map[Path]struct{}
}

// NewNamespace 创建命名空间。
func NewNamespace(name string) *SettingsNamespace {
	return &SettingsNamespace{Name: name, SecretPaths: map[Path]struct{}{}}
}

// MarkSecret 将某路径标记为密钥。
func (ns *SettingsNamespace) MarkSecret(p Path) {
	ns.SecretPaths[p] = struct{}{}
}

// IsSecret 判断路径是否密钥。
func (ns *SettingsNamespace) IsSecret(p Path) bool {
	_, ok := ns.SecretPaths[p]
	return ok
}

// ============================================================================
// SettingsScope
// ============================================================================

// PathOp 是单个设置路径操作。
type PathOp struct {
	// Set 非 nil 表示设置该路径为该值；否则表示 unset。
	Set *json.RawMessage `json:"set,omitempty"`
}

// NewSetOp 构造 set 操作。
func NewSetOp(v any) PathOp {
	raw, _ := json.Marshal(v)
	rm := json.RawMessage(raw)
	return PathOp{Set: &rm}
}

// NewUnsetOp 构造 unset 操作。
func NewUnsetOp() PathOp {
	return PathOp{}
}

// SettingsScope 是设置的作用域视图（host 层 + session 层）。
type SettingsScope struct {
	mu        sync.Mutex
	ns        *SettingsNamespace
	revision  uint64
	hostLayer *scope.Layer[string]
	sessLayer *scope.Layer[string]
}

// NewSettingsScope 创建设置作用域（host 层先建立）。
func NewSettingsScope(ns *SettingsNamespace) *SettingsScope {
	return &SettingsScope{
		ns:        ns,
		hostLayer: scope.NewLayer[string](scope.Key("host")),
		sessLayer: scope.NewLayer[string](scope.Key("session")),
	}
}

// Revision 返回当前修订号（每次 update/replace 后递增）。
func (s *SettingsScope) Revision() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.revision
}

// ApplyHost 设置 host 层默认值（expectedRevision=0 表示初始或强制）。
func (s *SettingsScope) ApplyHost(path Path, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	s.hostLayer.Register(string(path), string(raw), 0)
	return nil
}

// Get 按 nearest-wins 读取路径值（返回原始 JSON 字节）。
func (s *SettingsScope) Get(path Path) (json.RawMessage, bool) {
	scoped := scope.NewScopedLayers[string]().Push(s.hostLayer).Push(s.sessLayer)
	if v, ok := scoped.Get(string(path)); ok {
		return json.RawMessage(v), true
	}
	return nil, false
}

// Update 以 CAS 语义应用一组路径操作：
//   - expectedRevision 必须等于当前 Revision，否则返回 ErrRevisionMismatch；
//   - 每个 op 应用到 session 层（覆盖 host 层默认值）；
//   - 成功后 Revision +1。
func (s *SettingsScope) Update(expectedRevision uint64, ops map[Path]PathOp) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.revision != expectedRevision {
		return &ErrRevisionMismatch{Expected: expectedRevision, Actual: s.revision}
	}
	for path, op := range ops {
		if op.Set == nil {
			// unset：从 session 层移除，回落到 host 默认值
			s.sessLayer.Unregister(string(path))
		} else {
			s.sessLayer.Register(string(path), string(*op.Set), 0)
		}
	}
	s.revision++
	return nil
}

// Replace 整体替换 session 层设置（CAS）。
func (s *SettingsScope) Replace(expectedRevision uint64, values map[Path]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.revision != expectedRevision {
		return &ErrRevisionMismatch{Expected: expectedRevision, Actual: s.revision}
	}
	// 重建 session 层
	ns := scope.NewLayer[string](scope.Key("session"))
	for path, v := range values {
		raw, err := json.Marshal(v)
		if err != nil {
			return err
		}
		ns.Register(string(path), string(raw), 0)
	}
	s.sessLayer = ns
	s.revision++
	return nil
}

// ErrRevisionMismatch 是 CAS 修订号冲突错误。
type ErrRevisionMismatch struct {
	Expected uint64
	Actual   uint64
}

// Error 实现 error 接口。
func (e *ErrRevisionMismatch) Error() string {
	return fmt.Sprintf("settings: revision mismatch: expected %d, actual %d", e.Expected, e.Actual)
}

// IsRevisionMismatch 判断是否为修订冲突错误。
func IsRevisionMismatch(err error) bool {
	_, ok := err.(*ErrRevisionMismatch)
	return ok
}

// DescribedSetting 是 describe 输出的单条设置。
type DescribedSetting struct {
	Path   Path   `json:"path"`
	Op     string `json:"op"` // "set" / "unset" / "default"
	Value  any    `json:"value,omitempty"`
	Secret bool   `json:"secret,omitempty"`
}

// Describe 输出当前设置描述。redactSecrets=true 时 secret 路径只给操作位不给明文。
func (s *SettingsScope) Describe(redactSecrets bool) []DescribedSetting {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 合并所有路径（host + session）
	scoped := scope.NewScopedLayers[string]().Push(s.hostLayer).Push(s.sessLayer)
	merged := scoped.Merge()
	pathSet := map[Path]struct{}{}
	for _, e := range merged {
		if e.Name != "" {
			pathSet[Path(e.Name)] = struct{}{}
		}
	}
	paths := make([]Path, 0, len(pathSet))
	for p := range pathSet {
		paths = append(paths, p)
	}
	sort.Slice(paths, func(i, j int) bool { return paths[i] < paths[j] })

	var out []DescribedSetting
	for _, p := range paths {
		secret := s.ns.IsSecret(p)
		// 判定来源层：session 优先
		sessSet := s.sessLayer.Has(string(p))
		hostSet := s.hostLayer.Has(string(p))
		op := "unset"
		if sessSet {
			op = "set"
		} else if hostSet {
			op = "default"
		}
		ds := DescribedSetting{Path: p, Op: op, Secret: secret}
		if redactSecrets && secret {
			// 脱敏：只给操作位，value 为 nil
		} else if v, ok := s.Get(p); ok {
			var anyV any
			_ = json.Unmarshal(v, &anyV)
			ds.Value = anyV
		}
		out = append(out, ds)
	}
	return out
}
