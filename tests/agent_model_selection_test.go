// Package tests 的 ModelSelection（model_selection.go）验收测试。
package tests

import (
	"testing"

	"github.com/JopenChen/dsh-go/pkg/agent"
)

func TestModelSelectionCaptureFreezes(t *testing.T) {
	r := agent.NewModelSelectionRef()
	r.Select(&agent.ModelSelection{Provider: "deepseek", Model: "v3"})

	captured := r.Capture()
	if captured.Provider != "deepseek" || captured.Model != "v3" {
		t.Fatal("capture should freeze current selection")
	}

	// 捕获后再切换 current，不影响已捕获的 assembled
	r.Select(&agent.ModelSelection{Provider: "openai", Model: "gpt"})
	assembled := r.Assembled()
	if assembled.Provider != "deepseek" {
		t.Fatal("later switch must not change the captured snapshot of the current step")
	}
}

func TestModelSelectionApplyOverridesRequest(t *testing.T) {
	r := agent.NewModelSelectionRef()
	r.Select(&agent.ModelSelection{Provider: "p", Model: "m", ReasoningEffort: "high"})
	r.Capture()

	out := r.Apply(agent.RequestConfig{Provider: "old", Model: "oldm", ReasoningEffort: "inherited"})
	if out.Provider != "p" || out.Model != "m" || out.ReasoningEffort != "high" {
		t.Fatalf("apply should override provider/model/effort, got %+v", out)
	}
}

func TestModelSelectionApplyClearsInheritedEffortWhenAbsent(t *testing.T) {
	r := agent.NewModelSelectionRef()
	r.Select(&agent.ModelSelection{Provider: "p", Model: "m"}) // 无 effort
	r.Capture()

	out := r.Apply(agent.RequestConfig{ReasoningEffort: "inherited"})
	if out.ReasoningEffort != "" {
		t.Fatal("absent selected effort should clear inherited effort to restore default")
	}
}

func TestModelSelectionNoCaptureKeepsConfig(t *testing.T) {
	r := agent.NewModelSelectionRef()
	out := r.Apply(agent.RequestConfig{Provider: "x", Model: "y", ReasoningEffort: "z"})
	if out.Provider != "x" || out.Model != "y" || out.ReasoningEffort != "z" {
		t.Fatal("without a captured selection request config must be preserved")
	}
}

func TestModelSelectionCurrentIsCopy(t *testing.T) {
	r := agent.NewModelSelectionRef()
	r.Select(&agent.ModelSelection{Provider: "a"})
	c := r.Current()
	c.Provider = "b"
	if r.Current().Provider != "a" {
		t.Fatal("Current must return an independent copy")
	}
}
