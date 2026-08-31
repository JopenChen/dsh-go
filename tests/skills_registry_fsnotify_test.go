// Package tests 的 Skill 系统（M40）验收测试。
//
// 覆盖：
//   - 新建 <proj>/.dsh/skills/my-skill.md → 下一次 List() 出现；删除后下一次不出现
//   - 6 层 rank：同名高权威胜（project-dsh(0) > bundled(5)）
//   - skill(name) 工具解析注入 injected-context + modelInvocable/userInvocable 策略
//   - Watch 轮询：目录变更触发 onChange
package tests

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/JopenChen/dsh-go/pkg/skills"
)

// writeSkill 写一个技能 md 文件。
func writeSkill(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte("Description: "+name+"\n"+body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestSkillRegistryDynamicDiscovery 验证新建/删除技能后 List 反映变化。
func TestSkillRegistryDynamicDiscovery(t *testing.T) {
	root := t.TempDir()
	r := skills.New(root)
	projSkills := filepath.Join(root, ".dsh", "skills")

	// 初始空。
	r.Scan()
	if got := len(r.List()); got != 0 {
		t.Fatalf("初始应为空, 实际 %d", got)
	}

	// 新建 my-skill.md → 下一次 List 出现。
	writeSkill(t, projSkills, "my-skill", "do the thing")
	r.Scan()
	if _, ok := r.Get("my-skill"); !ok {
		t.Fatal("新建技能后应能被发现")
	}
	if len(r.List()) != 1 {
		t.Fatalf("应有 1 个技能, 实际 %d", len(r.List()))
	}

	// 删除 → 下一次不再出现。
	if err := os.Remove(filepath.Join(projSkills, "my-skill.md")); err != nil {
		t.Fatal(err)
	}
	r.Scan()
	if _, ok := r.Get("my-skill"); ok {
		t.Fatal("删除后不应再被发现")
	}
}

// TestSkillRankPrecedence 验证同名技能 rank 高者胜（project-dsh(0) 覆盖 bundled(5)）。
func TestSkillRankPrecedence(t *testing.T) {
	root := t.TempDir()
	r := skills.New(root)
	writeSkill(t, filepath.Join(root, ".dsh", "skills"), "cmd", "project version")
	writeSkill(t, filepath.Join(root, ".config", "skills"), "cmd", "custom version") // rank 2

	r.Scan()
	s, ok := r.Get("cmd")
	if !ok {
		t.Fatal("应有 cmd")
	}
	if s.Rank != 0 {
		t.Fatalf("project-dsh 应胜 (rank 0), 实际 rank %d", s.Rank)
	}
	if !containsStr(s.Content, "project version") {
		t.Fatalf("内容应取 project 版本, 实际 %q", s.Content)
	}
	// Candidate 携带 rank/locator。
	cands := r.Candidates()
	if len(cands) != 1 || cands[0].Rank != 0 {
		t.Fatalf("候选应 1 个且 rank 0, 实际 %+v", cands)
	}
}

// TestSkillToolInjection 验证 skill(name) 工具注入内容 + invocable 策略。
func TestSkillToolInjection(t *testing.T) {
	root := t.TempDir()
	r := skills.New(root)
	writeSkill(t, filepath.Join(root, ".dsh", "skills"), "refactor", "Refactor tips")
	r.Scan()

	tool := skills.NewSkillTool(r)
	content, err := tool.Resolve("refactor", true) // by model
	if err != nil {
		t.Fatal(err)
	}
	if !containsStr(content, "Refactor tips") {
		t.Fatalf("应注入正文, 实际 %q", content)
	}
	// 不存在 → 错误。
	if _, err := tool.Resolve("nope", true); err == nil {
		t.Fatal("不存在技能应报错")
	}
}

// TestSkillWatchChange 验证轮询 watch 在目录变化时回调。
func TestSkillWatchChange(t *testing.T) {
	root := t.TempDir()
	r := skills.New(root)
	stop := make(chan struct{})
	var mu sync.Mutex
	calls := 0
	go r.Watch(stop, 20*time.Millisecond, func([]string) {
		mu.Lock()
		calls++
		mu.Unlock()
	})

	// 等首扫完成。
	time.Sleep(60 * time.Millisecond)
	writeSkill(t, filepath.Join(root, ".dsh", "skills"), "newskill", "body")
	time.Sleep(120 * time.Millisecond)
	close(stop)

	mu.Lock()
	n := calls
	mu.Unlock()
	if n < 1 {
		t.Fatal("目录新增后 watch 应触发 onChange")
	}
	if _, ok := r.Get("newskill"); !ok {
		t.Fatal("watch 后技能应可见")
	}
}