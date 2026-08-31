// Package approval 提供审批策略（Approval Policy）接缝。
//
// 对齐上游：packages/core/approval
//
// 本文件对应任务 M27：Approval Policy 接缝。
//
// 设计要点：
//   - Policy 枚举：allow-all / deny-all / ask-dangerous / ask-dangerous-tool-edit；
//   - Effective（生效策略）解析遵循「预设层 → 用户层 → 会话层」三层 override 顺序，
//     会话层优先级最高（nearest-scope-wins）；并记录 Source 用于审计；
//   - ask→allowed-once 语义：ask 策略通过 User Questions 向用户询问，用户「准许」只对
//     这一次工具调用放行；下一次同工具调用继续 ask，绝不做按工具名的永久放行；
//   - 与 M22 PreToolDecision（tools/pre-execute 三态）协同：本包产出 Allow/Deny/Ask 决策。
//
// 依赖接缝：User Questions（userq）用于 ask；预设→策略的映射以 PresetPolice 函数注入，
// 从而 pkg/approval 不反向依赖 pkg/presets（M27 与 M28 相互依赖，靠单向注入破除环）。
package approval

import (
	"context"
	"fmt"
	"sync"

	"github.com/JopenChen/dsh-go/pkg/scope"
	"github.com/JopenChen/dsh-go/pkg/userq"
)

// ============================================================================
// 策略与决策
// ============================================================================

// Policy 是审批策略。
type Policy string

// 审批策略枚举（与 pkg/session.ApprovalPolicy 对齐）。
const (
	PolicyAllowAll          Policy = "allow-all"
	PolicyDenyAll           Policy = "deny-all"
	PolicyAskDangerous      Policy = "ask-dangerous"
	PolicyAskDangerousEdit  Policy = "ask-dangerous-tool-edit"
)

// EffSource 是生效策略的来源层级。
type EffSource string

const (
	SourcePreset  EffSource = "preset"
	SourceUser    EffSource = "user"
	SourceSession EffSource = "session"
)

// Effective 是一次解析出的生效审批策略（含来源审计）。
type Effective struct {
	Policy Policy     `json:"policy"`
	Source EffSource  `json:"source"`
}

// Decision 是工具预执行决策（三态），供 M22 PreToolDecision 复用。
type Decision int

// 决策三态。
const (
	DecideAllow Decision = iota
	DecideDeny
	DecideAsk
)

// String 返回决策的可读形式。
func (d Decision) String() string {
	switch d {
	case DecideAllow:
		return "allow"
	case DecideDeny:
		return "deny"
	case DecideAsk:
		return "ask"
	default:
		return "unknown"
	}
}

// ============================================================================
// Service
// ============================================================================

// PresetPolice 把预设名映射为审批策略；ok=false 表示该预设未定义、回落默认。
type PresetPolice func(preset string) (Policy, bool)

// validate 由预设映射解析出策略的包装（注入时统一无预设回落 deny-all，fail-closed）。
func (p PresetPolice) resolve(preset string) (Policy, bool) {
	if p == nil {
		return PolicyDenyAll, false
	}
	pol, ok := p(preset)
	if !ok {
		return PolicyDenyAll, false
	}
	return pol, true
}

// DangerClass 判断某工具是否属于"危险"类别（用于 ask-dangerous 系列触发 ask）。
type DangerFunc func(tool string) bool

// Request 是单次工具执行的审批请求。
type Request struct {
	// Tool 是工具名。
	Tool string
	// CallID 是本次工具调用的唯一标识（allowed-once 粒度锚点）。
	CallID string
	// Preset 是当前权限预设名（决定预设层策略）。
	Preset string
	// SessionID 是调用会话标识（决定会话层 override；空串表示无会话 override）。
	SessionID string
	// DangerousOverride 提供危险分类；nil 时按内置白名单。
	DangerousOverride DangerFunc
}

// Service 是审批服务（ctx.approval）。
type Service struct {
	preset PresetPolice
	uq     *userq.Service

	// 用户层 override（M03 scope）。key 固定为 "policy"；更高层 entries 抢占。
	userLayer    *scope.Layer[Policy]
	sessionLayer *scope.Layer[Policy]

	// 内置危险工具白名单（DangerFunc 缺省用）。
	dangerous map[string]bool

	mu   sync.Mutex
	asks int // 累计 ask 次数（测试断言）
}

// New 创建审批服务。preset 注入预设→策略映射；uq 注入用户提问服务；
// presetLayer 通过 PresetPolice 在运行时求值，不入 scope。
func New(preset PresetPolice, uq *userq.Service) *Service {
	return &Service{
		preset:       preset,
		uq:           uq,
		userLayer:    scope.NewLayer[Policy](scope.Key("user")),
		sessionLayer: scope.NewLayer[Policy](scope.Key("session")),
		dangerous:    defaultDangerous(),
	}
}

// SetDangerous 覆写危险工具白名单。
func (s *Service) SetDangerous(m map[string]bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dangerous = m
}

func defaultDangerous() map[string]bool {
	return map[string]bool{
		"shell":    true,
		"bash":     true,
		"subprocess": true,
		"fs_write": true,
		"fs_edit":  true,
		"rm":       true,
	}
}

// SetUserPolicy 设置用户层策略 override（source=user）。
func (s *Service) SetUserPolicy(p Policy) {
	s.userLayer.Unregister("policy")
	s.userLayer.Register("policy", p, 100)
}

// SetUserPolicyByKey 设置某个用户的策略 override。
func (s *Service) SetUserPolicyByKey(userKey string, p Policy) {
	s.userLayer.Unregister(userKey)
	s.userLayer.Register(userKey, p, 100)
}

// SetSessionPolicy 设置会话层策略 override（source=session，优先级最高）。
func (s *Service) SetSessionPolicy(sessionID string, p Policy) {
	s.sessionLayer.Unregister(sessionID)
	s.sessionLayer.Register(sessionID, p, 100)
}

// ClearSessionPolicy 移除会话层 override。
func (s *Service) ClearSessionPolicy(sessionID string) {
	s.sessionLayer.Unregister(sessionID)
}

// AskCount 返回累计 ask 次数（测试断言用）。
func (s *Service) AskCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.asks
}

// Resolve 解析生效审批策略：预设层 → 用户层 → 会话层（会话最高）。
func (s *Service) Resolve(preset string, sessionID string) Effective {
	// 会话层 override 优先（最近作用域胜）。
	if sessionID != "" {
		if p, ok := s.sessionLayer.Get(sessionID); ok {
			return Effective{Policy: p, Source: SourceSession}
		}
	}
	// 用户层 override。
	if p, ok := s.userLayer.Get("policy"); ok {
		return Effective{Policy: p, Source: SourceUser}
	}
	// 预设层。
	if p, ok := s.preset.resolve(preset); ok {
		return Effective{Policy: p, Source: SourcePreset}
	}
	// 无任何层 → fail-closed 默认 deny-all。
	return Effective{Policy: PolicyDenyAll, Source: SourcePreset}
}

// Evaluate 对一次工具调用给出 allow/deny/ask 决策。
//
//   - allow-all → allow；deny-all → deny；
//   - ask-dangerous / ask-dangerous-tool-edit → 若工具危险则 ask（调用 UQ），否则 allow；
//   - ask 用户"准许"只放行本次调用（allowed-once），下一次同工具继续 ask。
func (s *Service) Evaluate(req Request) (Decision, error) {
	eff := s.Resolve(req.Preset, req.SessionID)
	switch eff.Policy {
	case PolicyAllowAll:
		return DecideAllow, nil
	case PolicyDenyAll:
		return DecideDeny, nil
	case PolicyAskDangerous, PolicyAskDangerousEdit:
		if !s.isDangerous(req.Tool, req.DangerousOverride) {
			return DecideAllow, nil
		}
		allowed, err := s.ask(req)
		if err != nil {
			// UQ 失败链：读阻断失败按 deny 处理（fail-closed），但错误上抛供上层审计。
			return DecideDeny, err
		}
		if allowed {
			return DecideAllow, nil
		}
		return DecideDeny, nil
	default:
		return DecideDeny, fmt.Errorf("approval: unknown policy %q", eff.Policy)
	}
}

// isDangerous 判断工具是否危险：优先使用请求方提供的分类，缺省用内置白名单。
func (s *Service) isDangerous(tool string, fn DangerFunc) bool {
	if fn != nil {
		return fn(tool)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dangerous[tool]
}

// ask 通过 User Questions 向用户询问并返回是否准许。
func (s *Service) ask(req Request) (bool, error) {
	s.mu.Lock()
	s.asks++
	s.mu.Unlock()
	if s.uq == nil {
		return false, fmt.Errorf("approval: no userq service for ask on %q", req.Tool)
	}
	idx, _, err := s.uq.Ask(context.Background(), userq.QuestionOptions{
		Prompt:       fmt.Sprintf("是否准许危险工具调用 %s？", req.Tool),
		Choices:      []string{"准许", "拒绝"},
		DefaultIndex: 1, // 默认拒绝，fail-closed
		Intent:       "approval",
	})
	if err != nil {
		// 提问失败视为拒绝（fail-closed），错误上抛。
		return false, err
	}
	// allowed-once：仅对本次调用放行，不持久化到任何层。
	return idx == 0, nil
}