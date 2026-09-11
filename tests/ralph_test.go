// Package tests 的 Ralph 循环验收测试。
package tests

import (
	"context"
	"testing"

	"github.com/JopenChen/dsh-go/pkg/workflow"
)

func TestRalphCompletes(t *testing.T) {
	runner := func(_ context.Context, _, prior string) (workflow.RoundReport, error) {
		if prior == "" {
			return workflow.RoundReport{Status: workflow.StatusContinue, NextSteps: []string{"do"}}, nil
		}
		return workflow.RoundReport{Status: workflow.StatusComplete, Evidence: "done"}, nil
	}
	res, err := workflow.RunRalph(context.Background(), "obj", runner, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != workflow.RunCompleted || res.Rounds != 2 {
		t.Fatalf("result = %+v", res)
	}
}

func TestRalphBlocked(t *testing.T) {
	runner := func(_ context.Context, _, _ string) (workflow.RoundReport, error) {
		return workflow.RoundReport{Status: workflow.StatusBlocked, Blocker: "need human"}, nil
	}
	res, _ := workflow.RunRalph(context.Background(), "obj", runner, 10, 0)
	if res.Status != workflow.RunBlocked {
		t.Fatalf("should block: %+v", res)
	}
}

func TestRalphBudgetLimited(t *testing.T) {
	runner := func(_ context.Context, _, _ string) (workflow.RoundReport, error) {
		return workflow.RoundReport{Status: workflow.StatusContinue, NextSteps: []string{"x"}}, nil
	}
	res, _ := workflow.RunRalph(context.Background(), "obj", runner, 3, 0)
	if res.Status != workflow.RunBudgetLimited || res.Rounds != 3 {
		t.Fatalf("should be budget limited: %+v", res)
	}
}

func TestRalphInvalidReport(t *testing.T) {
	runner := func(_ context.Context, _, _ string) (workflow.RoundReport, error) {
		// complete 但缺 evidence，应校验失败。
		return workflow.RoundReport{Status: workflow.StatusComplete}, nil
	}
	if _, err := workflow.RunRalph(context.Background(), "obj", runner, 3, 0); err == nil {
		t.Fatal("invalid report must error")
	}
}
