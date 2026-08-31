// Package tests 的 N03（D2 纪律）验收测试。
//
// 覆盖：
//   - 纯函数：同一组装器 1000 次渲染逐字节相同（含系统哈希稳定）
//   - 静态检测捕获反模式（time.Now / os.Getwd / rand）
//   - 干净 section 通过检测（0 violation）
package tests

import (
	"testing"

	"github.com/JopenChen/dsh-go/pkg/sysprompt"
)

// TestN03SystemPromptPureFunction 验证 1000 次渲染逐字节相同（纯函数证明）。
func TestN03SystemPromptPureFunction(t *testing.T) {
	a := sysprompt.New()
	a.Register("persona", sysprompt.SectionOrderPersona, "You are a helpful agent.")
	a.Register("policy", sysprompt.SectionOrderPolicy, "Follow plan mode policy.")
	a.Register("tools", sysprompt.SectionOrderToolsSchema, "# tools schema")

	first := a.Assemble()
	firstHash := a.AssembleHash()
	for i := 0; i < 1000; i++ {
		if got := a.Assemble(); got != first {
			t.Fatalf("第 %d 次渲染不一致（纯函数破坏）", i)
		}
		if got := a.AssembleHash(); got != firstHash {
			t.Fatalf("第 %d 次哈希不稳定: %s != %s", i, got, firstHash)
		}
	}
}

// TestN03StaticCheckCatchesBanned 验证静态检测捕获反模式。
func TestN03StaticCheckCatchesBanned(t *testing.T) {
	bad := `Today is ` + "`time.Now()`" + ` in $(os.Getwd())` // 显式以反模式表示
	// 构造触发文本。
	dirty := "const now = time.Now(); const cwd = os.Getwd(); use math/rand"
	violations := sysprompt.StaticCheck(dirty)
	if len(violations) == 0 {
		t.Fatalf("应捕获 time.Now/os.Getwd/rand 反模式")
	}
	if sysprompt.IsPure(dirty) {
		t.Fatal("含反模式文本不应是纯函数")
	}
	_ = bad
}

// TestN03StaticCheckClean 验证干净 section 通过检测（0 violation）。
func TestN03StaticCheckClean(t *testing.T) {
	clean := "You are a helpful assistant. Follow the workspace policy."
	if v := sysprompt.StaticCheck(clean); len(v) != 0 {
		t.Fatalf("干净文本应无违规: %v", v)
	}
	if !sysprompt.IsPure(clean) {
		t.Fatal("干净文本应是纯函数")
	}
}

// TestN03CheckSections 验证整组装器级别检测。
func TestN03CheckSections(t *testing.T) {
	a := sysprompt.New()
	a.Register("persona", sysprompt.SectionOrderPersona, "You are helpful.")
	a.Register("bad", sysprompt.SectionOrderPolicy, "os.Gos.Getwd() dynamic")
	_, forbidden := sysprompt.CheckSections(a)
	_ = forbidden
	// 该 section 含 os.Getwd，应 forbidden=true。
	// 修正文本后再检查。
	if !forbidden {
		t.Fatal("含反模式 section 应 forbidden")
	}
	// 移除后应干净。
	a.Unregister("bad")
	viol, forb := sysprompt.CheckSections(a)
	if len(viol) != 0 || forb {
		t.Fatalf("移除后应 0 violation: %v", viol)
	}
}

// TestN03OrderConst 验证 order 写死为常量（不可被运行时覆盖语义）。
func TestN03OrderConst(t *testing.T) {
	if sysprompt.SectionOrderPersona >= sysprompt.SectionOrderPolicy {
		t.Fatal("persona(100) 应小于 policy(200)")
	}
	// Assembler 以传入 order 为准但 Section 只读取常量式字段。
	a := sysprompt.New()
	a.Register("policy", sysprompt.SectionOrderPolicy, "p")
	s := a.Sections()[0]
	if s.Order != sysprompt.SectionOrderPolicy {
		t.Fatalf("order 应来自常量 200, 实际 %d", s.Order)
	}
}