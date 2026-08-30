// Package invariant 提供不变量（Invariant）校验注册中心。
//
// 对齐上游：packages/runtime-diagnostics/invariants
//
// 设计动机：
//   - dsh-go 的事件溯源 Session 依赖严格的时序不变量（turn 开闭 / step 配对 /
//     tool call↔result 匹配 / goal revision CAS 等），这些不变量在开发期必须被
//     持续校验，一旦违规立即暴露包归属与违规细节，避免把坏状态写进持久化文件；
//   - 生产环境为性能考虑可整体关闭（SetEnabled(false) 或构建标签 dsh_prod），
//     校验开关对业务代码透明。
//
// 使用方式：
//   - 开发期在 harness 初始化时调用 invariant.NewRegistry() 并逐包 Register；
//   - 任意位置可调用 registry.Run() 执行全部已注册检查器，收集所有违规错误；
//   - 违规错误带 "INVARIANT [pkgName]: ..." 前缀，便于按包定位问题。
package invariant

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

// 全局开关：默认开启（开发期），生产入口调用 SetEnabled(false) 关闭。
// 使用 atomic.Bool 保证并发安全，且不影响正常调用路径的加锁开销。
var enabled atomic.Bool

func init() {
	enabled.Store(true)
}

// SetEnabled 全局开启/关闭不变量校验。
// 生产构建应在 main 入口调用 SetEnabled(false)。
func SetEnabled(on bool) {
	enabled.Store(on)
}

// IsEnabled 返回当前不变量校验全局开关状态。
func IsEnabled() bool {
	return enabled.Load()
}

// InvariantError 表示一次不变量违规，携带包归属信息。
type InvariantError struct {
	// Pkg 为注册该检查器时声明的归属包名（如 "pkg/session"）。
	Pkg string
	// Msg 为违规的具体描述。
	Msg string
}

// Error 实现 error 接口，输出带包归属前缀，便于按包定位。
func (e *InvariantError) Error() string {
	return fmt.Sprintf("INVARIANT [%s]: %s", e.Pkg, e.Msg)
}

// Checker 是单个不变量检查函数：返回 nil 表示通过，返回 error 表示违规。
type Checker func() error

// Registry 是不变量注册中心，按包名分组管理检查器。
type Registry struct {
	mu       sync.Mutex
	checkers map[string][]Checker // pkgName -> 该包注册的检查器列表
	ledger   []string             // 违规登记账本（用于审计与测试断言）
}

// NewRegistry 创建空的不变量注册中心。
func NewRegistry() *Registry {
	return &Registry{
		checkers: make(map[string][]Checker),
		ledger:   make([]string, 0),
	}
}

// Register 注册一个不变量检查器到指定包名下。
//   - pkgName 为空会返回错误（不变量必须可归属到具体包）；
//   - 检查器为 nil 会被忽略。
//
// 开发期约定：每个包在初始化时把自己的不变量全部注册进来，
// 由 harness 在关键写路径（如 Session.Append）前统一 Run()。
func (r *Registry) Register(pkgName string, checker Checker) error {
	if pkgName == "" {
		return fmt.Errorf("invariant: pkgName must not be empty")
	}
	if checker == nil {
		return fmt.Errorf("invariant: checker must not be nil for pkg %q", pkgName)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.checkers[pkgName] = append(r.checkers[pkgName], checker)
	return nil
}

// Run 执行全部已注册检查器，返回所有违规错误（按包名排序，保证确定性输出）。
//   - 全局开关关闭时直接返回 nil（生产路径零开销）；
//   - 单个检查器 panic 会被捕获并转为错误，避免一个包的 bug 拖垮整体校验。
func (r *Registry) Run() []error {
	if !IsEnabled() {
		return nil
	}
	r.mu.Lock()
	pkgs := make([]string, 0, len(r.checkers))
	for pkg := range r.checkers {
		pkgs = append(pkgs, pkg)
	}
	// 深拷贝检查器列表，避免 Run 期间被并发 Register 影响
	snapshot := make(map[string][]Checker, len(r.checkers))
	for pkg, list := range r.checkers {
		cp := make([]Checker, len(list))
		copy(cp, list)
		snapshot[pkg] = cp
	}
	r.mu.Unlock()

	sort.Strings(pkgs)
	var failures []error
	for _, pkg := range pkgs {
		for _, checker := range snapshot[pkg] {
			if err := runSafe(checker); err != nil {
				// 若检查器返回的不是 InvariantError，包装成带包归属的错误
				var invErr *InvariantError
				if as, ok := err.(*InvariantError); ok {
					invErr = as
				} else {
					invErr = &InvariantError{Pkg: pkg, Msg: err.Error()}
				}
				failures = append(failures, invErr)
				r.record(pkg, invErr.Error())
			}
		}
	}
	return failures
}

// runSafe 安全执行检查器，捕获 panic 转为 error。
func runSafe(checker Checker) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("invariant checker panicked: %v", p)
		}
	}()
	return checker()
}

// record 将违规登记到账本（带包名与详情）。
func (r *Registry) record(pkg, detail string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ledger = append(r.ledger, detail)
}

// Ledger 返回违规登记账本快照（按登记顺序），供审计与测试断言。
func (r *Registry) Ledger() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.ledger))
	copy(out, r.ledger)
	return out
}

// MustSatisfy 便捷方法：执行全部检查，任一违规即 panic（开发期断言用）。
// 生产关闭开关后该方法退化为 no-op。
func (r *Registry) MustSatisfy() {
	if !IsEnabled() {
		return
	}
	failures := r.Run()
	if len(failures) > 0 {
		msgs := make([]string, 0, len(failures))
		for _, f := range failures {
			msgs = append(msgs, f.Error())
		}
		panic("invariant violated: " + strings.Join(msgs, "; "))
	}
}
