// 教程：沙箱 + 审批（Sandbox & Approval）——Agent 的"受控执行"（教学示例）。
//
// 让 Agent 操作真实文件/进程前，必须先过两道安全闸：
//   A. 审批（Approval）：工具调用是放行、拒绝，还是询问用户？
//   B. 沙箱（Sandbox）：即便放行，文件副作用被约束在哪个边界内？
//
// 本项目语义：
//   - 审批策略四选一：allow-all / deny-all / ask-dangerous / ask-dangerous-tool-edit；
//     生效策略按「预设层 → 用户层 → 会话层」解析，会话层优先（nearest-scope-wins）；
//     ask 的"准许"只放行本次调用（allowed-once），下次继续问；
//   - 沙箱模式三选一：read-only / workspace-write / danger-full-access；
//     策略按「显式 override → 会话记录 → 部署默认」解析。
//
// 本示例演示：
//   1. 审批：三层策略解析（预设→用户→会话）+ 三态决策（allow/deny/ask）；
//   2. 沙箱：三种模式的策略解析与 danger 判定；
//   3. 组合：danger 工具在 ask-dangerous 下的安全默认（stub 默认拒绝）。
//
// 运行方式（仓库根目录）：
//   go run ./examples/sandbox_approval
//
// 对照阅读：pkg/approval/approval.go · pkg/sandbox/sandbox.go · pkg/userq/userq.go
package main

import (
	"fmt"

	"github.com/JopenChen/dsh-go/pkg/approval"
	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/sandbox"
	"github.com/JopenChen/dsh-go/pkg/userq"
)

func main() {
	// ------------------------------------------------------------------
	// A. 审批
	// ------------------------------------------------------------------
	// 预设→策略映射：预设 "safe" → deny-all；"dev" → ask-dangerous。
	presetPolice := func(preset string) (approval.Policy, bool) {
		switch preset {
		case "safe":
			return approval.PolicyDenyAll, true
		case "dev":
			return approval.PolicyAskDangerous, true
		default:
			return "", false
		}
	}
	// userq 用 stub（默认选"拒绝"→ 演示 fail-closed 安全默认）。
	uq := userq.New(userq.NewStub())
	svc := approval.New(presetPolice, uq)

	// 1) 三层策略解析：预设层 "dev" → ask-dangerous（source=preset）。
	eff := svc.Resolve("dev", "")
	fmt.Println("— A1. 策略解析（预设 dev）—")
	fmt.Printf("  生效策略=%s, 来源=%s\n", eff.Policy, eff.Source)

	// 2) 会话层 override：把某个会话临时压成 deny-all（优先级最高）。
	sessID := "sess-1"
	svc.SetSessionPolicy(sessID, approval.PolicyDenyAll)
	eff2 := svc.Resolve("dev", sessID)
	fmt.Println("— A2. 会话层 override（nearest-scope-wins）—")
	fmt.Printf("  生效策略=%s, 来源=%s\n", eff2.Policy, eff2.Source)

	// 3) 三态决策：deny-all 下，连读操作也被拒（fail-closed）。
	d, err := svc.Evaluate(approval.Request{Tool: "fs_read", Preset: "dev", SessionID: sessID})
	fmt.Println("— A3. 决策三态（deny-all）—")
	fmt.Printf("  fs_read => %s (err=%v)\n", d, err)

	// 4) ask-dangerous：危险工具触发 ask；stub 默认"拒绝"→ deny（安全默认）。
	//    非危险工具（如 fs_read）直接 allow，不打扰用户。
	d2, _ := svc.Evaluate(approval.Request{Tool: "shell", Preset: "dev"}) // shell 在危险白名单
	d3, _ := svc.Evaluate(approval.Request{Tool: "fs_read", Preset: "dev"})
	fmt.Println("— A4. ask-dangerous（stub 默认拒绝 = fail-closed）—")
	fmt.Printf("  shell(危险) => %s\n  fs_read(非危险) => %s\n", d2, d3)
	fmt.Printf("  (累计 ask 次数=%d)\n", svc.AskCount())

	// ------------------------------------------------------------------
	// B. 沙箱
	// ------------------------------------------------------------------
	// 构造内存会话（实现 sandbox.Session：提供 id / cwd / 会话级沙箱覆盖）。
	ms := &memSession{id: brand.NewSessionID("sbox-sess"), cwd: "/workspace/app", mode: sandbox.ModeWorkspaceWrite}
	ps := sandbox.NewPolicyService(sandbox.ModeReadOnly, "/fallback")

	// 1) 无显式 override：会话记录模式 workspace-write 生效。
	p1 := ps.Resolve(sandbox.SandboxPolicyRequest{Session: ms})
	fmt.Println("— B1. 沙箱策略（会话覆盖 workspace-write）—")
	fmt.Printf("  mode=%s, root=%s, danger=%v\n", p1.Mode, p1.WorkspaceRoot, p1.IsDanger())

	// 2) 显式 override 为 danger-full-access：最高优先级，越过会话记录。
	danger := sandbox.ModeDangerFullAccess
	p2 := ps.Resolve(sandbox.SandboxPolicyRequest{Session: ms, Mode: &danger})
	fmt.Println("— B2. 显式 override danger-full-access —")
	fmt.Printf("  mode=%s, danger=%v（绕过沙箱，直接原始 argv）\n", p2.Mode, p2.IsDanger())

	// 3) 无会话（agentless）：回落部署默认 read-only + 回落根。
	p3 := ps.Resolve(sandbox.SandboxPolicyRequest{})
	fmt.Println("— B3. agentless 回落 —")
	fmt.Printf("  mode=%s, root=%s, danger=%v\n", p3.Mode, p3.WorkspaceRoot, p3.IsDanger())

	fmt.Println("\n— 结论：审批定「能不能」、沙箱定「在哪儿」，组合成受控执行 —")
}

// memSession 是一个内存沙箱会话（实现 sandbox.Session 接口，教学用）。
type memSession struct {
	id   brand.SessionID
	cwd  string
	mode sandbox.SandboxMode
}

func (m *memSession) ID() brand.SessionID { return m.id }
func (m *memSession) Cwd() string         { return m.cwd }
func (m *memSession) SandboxMode() (sandbox.SandboxMode, bool) {
	if m.mode == "" {
		return "", false
	}
	return m.mode, true
}