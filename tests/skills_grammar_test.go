// Package tests 的 skill grammar 验收测试。
package tests

import (
	"strings"
	"testing"

	"github.com/JopenChen/dsh-go/pkg/skills"
)

func TestSkillIsName(t *testing.T) {
	good := []string{"a", "abc", "a-b", "a-b-c1"}
	bad := []string{"", "A", "a--b", "-a", "a b", "a_b"}
	for _, g := range good {
		if !skills.IsName(g) {
			t.Errorf("%q should be valid", g)
		}
	}
	for _, b := range bad {
		if skills.IsName(b) {
			t.Errorf("%q should be invalid", b)
		}
	}
}

func TestSkillRenderContent(t *testing.T) {
	out := skills.RenderContent("demo", "runtime", "do x")
	for _, want := range []string{`<skill_content name="demo">`, "<skill_instructions>", "do x", "</skill_content>"} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing %q: %s", want, out)
		}
	}
}
