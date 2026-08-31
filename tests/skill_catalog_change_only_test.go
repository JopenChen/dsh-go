// Package tests 的 N04（D3 纪律）验收测试。
//
// 覆盖：
//   - CatalogText 跨 1000 次调用逐字节相同（无随机/时间戳）
//   - 添加/删除 skill → CatalogHash 变化 → Injector 触发
//   - skills 不变时 change-only 只注入 1 次（首次）
package tests

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JopenChen/dsh-go/pkg/skills"
)

// TestN04CatalogStable 验证 CatalogText 跨 1000 次调用逐字节相同。
func TestN04CatalogStable(t *testing.T) {
	root := t.TempDir()
	r := skills.New(root)
	for _, n := range []string{"b", "a", "c"} {
		writeSkill(t, filepath.Join(root, ".dsh", "skills"), n, "desc of "+n)
	}
	r.Scan()
	first := r.CatalogText()
	firstHash := r.CatalogHash()
	if first == "" {
		t.Fatal("CatalogText 不应为空")
	}
	for i := 0; i < 1000; i++ {
		if got := r.CatalogText(); got != first {
			t.Fatalf("第 %d 次 CatalogText 不一致", i)
		}
		if got := r.CatalogHash(); got != firstHash {
			t.Fatalf("CatalogHash 不稳定")
		}
	}
	// 字典序稳定：a 在 b 前。
	if indexOf(first, "a ") > indexOf(first, "b ") {
		t.Fatalf("catalog 应按 name 字典序: %q", first)
	}
}

// TestN04ChangeOnlyInject 验证添加/删除 skill → hash 变化 → 注入触发；不变时不重复注入。
func TestN04ChangeOnlyInject(t *testing.T) {
	root := t.TempDir()
	r := skills.New(root)
	proj := filepath.Join(root, ".dsh", "skills")
	writeSkill(t, proj, "alpha", "AAA")
	r.Scan()

	in := skills.NewInjector(r)
	// 首次注入。
	content, injected := in.MaybeInject()
	if !injected || content == "" {
		t.Fatal("首次应注入")
	}
	// skills 不变 → 反复调用不重复注入。
	for i := 0; i < 50; i++ { // 模拟 50 轮
		if _, injected := in.MaybeInject(); injected {
			t.Fatalf("skills 不变时第 %d 轮应不注入", i)
		}
	}
	if in.InjectCount() != 1 {
		t.Fatalf("应只注入 1 次, 实际 %d", in.InjectCount())
	}

	// 添加一个 skill → hash 变化 → 注入。
	writeSkill(t, proj, "beta", "BBB")
	r.Scan()
	if _, injected := in.MaybeInject(); !injected {
		t.Fatal("新增 skill 后应重新注入")
	}
	if in.InjectCount() != 2 {
		t.Fatalf("应有两次注入, 实际 %d", in.InjectCount())
	}

	// 删除 alpha → hash 变化 → 注入。
	if err := os.Remove(filepath.Join(proj, "alpha.md")); err != nil {
		t.Fatal(err)
	}
	r.Scan()
	if _, injected := in.MaybeInject(); !injected {
		t.Fatal("删除 skill 后应重新注入")
	}
}

// TestN04InjectorBlockedInAvailableSkills 验证注入内容含 <available_skills> 区块。
func TestN04InjectorBlockedInAvailableSkills(t *testing.T) {
	root := t.TempDir()
	r := skills.New(root)
	writeSkill(t, filepath.Join(root, ".dsh", "skills"), "refactor", "tool tips")
	r.Scan()
	in := skills.NewInjector(r)
	content, _ := in.MaybeInject()
	if indexOf(content, "<available_skills>") < 0 || indexOf(content, "</available_skills>") < 0 {
		t.Fatalf("注入应含 <available_skills> 区块: %q", content)
	}
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}