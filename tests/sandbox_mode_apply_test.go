// Package tests 的沙箱接缝（M26）验收测试。
//
// 覆盖：
//   - 同一会话下 Bash 与 FS 两个消费者 resolve 得到一致 ExecutionPolicy（enforced + scope）
//   - 模式优先级（显式覆盖 > 会话 override > 部署默认）
//   - workspaceRoot 规范化与根回落
//   - danger 模式绕过、受约束模式降级
//   - Provider fail-closed（SANDBOX_UNAVAILABLE）
package tests

import (
	"testing"

	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/sandbox"
)

// fakeSession 是 sandbox.Session 的测试桩。
type fakeSession struct {
	id   string
	cwd  string
	mode sandbox.SandboxMode
	has  bool
}

func (f *fakeSession) ID() brand.SessionID { return brand.NewSessionID(f.id) }
func (f *fakeSession) Cwd() string         { return f.cwd }
func (f *fakeSession) SandboxMode() (sandbox.SandboxMode, bool) {
	return f.mode, f.has
}

// TestSandboxBothConsumersConsistentPolicy 验证同一会话下 Bash 与 FS 解析到一致策略。
func TestSandboxBothConsumersConsistentPolicy(t *testing.T) {
	svc := sandbox.NewPolicyService(sandbox.ModeReadOnly, "/fallback")
	sess := &fakeSession{id: "s1", cwd: `C:\work`, mode: sandbox.ModeWorkspaceWrite, has: true}

	// 两个消费者（Bash / FS）各自调用 Resolve 得到策略。
	bashPolicy := svc.Resolve(sandbox.SandboxPolicyRequest{Session: sess})
	fsPolicy := svc.Resolve(sandbox.SandboxPolicyRequest{Session: sess})

	// 一致（enforced + scope）：模式与 workspace 边界完全相等。
	if bashPolicy.Mode != fsPolicy.Mode || bashPolicy.WorkspaceRoot != fsPolicy.WorkspaceRoot {
		t.Fatalf("Bash/FS 策略不一致: bash=%+v fs=%+v", bashPolicy, fsPolicy)
	}
	if bashPolicy.Mode != sandbox.ModeWorkspaceWrite {
		t.Fatalf("应采用会话覆盖 workspace-write, 实际 %s", bashPolicy.Mode)
	}
	if bashPolicy.WorkspaceRoot != `C:\work` {
		t.Fatalf("workspace root 应取会话 cwd, 实际 %q", bashPolicy.WorkspaceRoot)
	}
	if bashPolicy.IsDanger() {
		t.Fatal("workspace-write 不应是 danger")
	}
}

// TestSandboxModePrecedence 验证模式优先级：显式 > 会话 > 默认。
func TestSandboxModePrecedence(t *testing.T) {
	svc := sandbox.NewPolicyService(sandbox.ModeReadOnly, "/root")

	// 无会话、无覆盖 → 部署默认。
	p := svc.Resolve(sandbox.SandboxPolicyRequest{})
	if p.Mode != sandbox.ModeReadOnly {
		t.Fatalf("无输入应回落默认 read-only, 实际 %s", p.Mode)
	}

	// 会话 override 覆盖默认。
	sess := &fakeSession{id: "s", cwd: "/ws", mode: sandbox.ModeDangerFullAccess, has: true}
	p = svc.Resolve(sandbox.SandboxPolicyRequest{Session: sess})
	if p.Mode != sandbox.ModeDangerFullAccess {
		t.Fatalf("会话 override 应为 danger, 实际 %s", p.Mode)
	}
	if !p.IsDanger() {
		t.Fatal("IsDanger() 应为 true")
	}

	// 显式覆盖最终胜出。
	explicit := sandbox.ModeWorkspaceWrite
	p = svc.Resolve(sandbox.SandboxPolicyRequest{Session: sess, Mode: &explicit})
	if p.Mode != sandbox.ModeWorkspaceWrite {
		t.Fatalf("显式覆盖应为 workspace-write, 实际 %s", p.Mode)
	}
}

// TestSandboxWorkspaceRootFallback 验证根回落与规范化。
func TestSandboxWorkspaceRootFallback(t *testing.T) {
	svc := sandbox.NewPolicyService(sandbox.ModeWorkspaceWrite, "/fallback")
	// 无 cwd 会话 → 回落根。
	p := svc.Resolve(sandbox.SandboxPolicyRequest{})
	if p.WorkspaceRoot == "" || p.WorkspaceRoot != svc.Resolve(sandbox.SandboxPolicyRequest{}).WorkspaceRoot {
		t.Fatalf("回落根异常: %q", p.WorkspaceRoot)
	}
	// cwd 规范化。
	sess := &fakeSession{id: "s", cwd: "relative/x"}
	p = svc.Resolve(sandbox.SandboxPolicyRequest{Session: sess})
	if p.WorkspaceRoot == "relative/x" {
		t.Fatalf("workspaceRoot 应规范化为绝对路径, 实际 %q", p.WorkspaceRoot)
	}
}

// TestSandboxToConfined 验证 danger 之外的策略可降级为受约束策略；danger 拒绝降级。
func TestSandboxToConfined(t *testing.T) {
	base := sandbox.SandboxExecutionPolicy{
		Mode:          sandbox.ModeWorkspaceWrite,
		WorkspaceRoot: "/ws",
	}
	confined, err := base.ToConfined()
	if err != nil {
		t.Fatalf("workspace-write 应可降级: %v", err)
	}
	if confined.Mode != sandbox.ConfinedWorkspaceWrite {
		t.Fatalf("降级模式应为 workspace-write, 实际 %s", confined.Mode)
	}
	// danger 不可降级
	danger := base
	danger.Mode = sandbox.ModeDangerFullAccess
	if _, err := danger.ToConfined(); err == nil {
		t.Fatal("danger-full-access 降级应报错")
	}
}

// TestSandboxProviderFailClosed 验证无后端时 confine 必须 fail-closed。
func TestSandboxProviderFailClosed(t *testing.T) {
	prov := &sandbox.UnavailableProvider{}
	_, err := prov.Confine(nil, sandbox.SandboxPolicy{})
	if err == nil {
		t.Fatal("无可用后端必须返回错误，绝不允许静默放行")
	}
	if err.Error() != sandbox.SandboxUnavailableErrorCode && !containsStr(err.Error(), sandbox.SandboxUnavailableErrorCode) {
		t.Fatalf("应携带 SANDBOX_UNAVAILABLE 码, 实际: %v", err)
	}
}

// TestSandboxRunnerFailureRule 验证 runner 失败证据规则（退出码门控 + info 行排除 + 致命签名）。
func TestSandboxRunnerFailureRule(t *testing.T) {
	rule := sandbox.RunnerFailureRule{
		AllowedExitCodes:    []int{125},
		FatalSignatures:     []string{"cannot start sandbox runner", "exec format error"},
		InformationalLines:  []string{"landlock: fallback to baseline"},
	}
	// 退出码不匹配 → 不判定失败。
	if _, ok := rule.Matches(1, "boom"); ok {
		t.Fatal("退出码 1 不在白名单，不应判定失败")
	}
	// 匹配 exitCode=125 + 致命签名行（info 行被排除）。
	stderr := "landlock: fallback to baseline\ncannot start sandbox runner: permission denied\n"
	sig, ok := rule.Matches(125, stderr)
	if !ok {
		t.Fatal("应判定 runner 失败")
	}
	if sig == "" {
		t.Fatal("应返回匹配到的致命签名")
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}