// Package tests 的 launchenv 分层环境验收测试。
package tests

import (
	"testing"

	"github.com/JopenChen/dsh-go/pkg/launchenv"
)

func TestLaunchEnvPrecedence(t *testing.T) {
	s := launchenv.New([]launchenv.LayerInput{
		{Source: launchenv.SourceUserEnv, Values: map[string]string{"K": "user", "ONLY": "u"}},
		{Source: launchenv.SourceProcess, Values: map[string]string{"K": "proc"}},
	})
	e, ok := s.Get("K")
	if !ok || e.Value != "proc" || e.Source != launchenv.SourceProcess {
		t.Fatalf("process must win: %+v", e)
	}
	e, ok = s.Get("ONLY")
	if !ok || e.Value != "u" {
		t.Fatalf("user fallback failed: %+v", e)
	}
	if _, ok := s.Get("MISSING"); ok {
		t.Fatal("missing must not resolve")
	}
}

func TestLaunchEnvGetFrom(t *testing.T) {
	s := launchenv.New([]launchenv.LayerInput{
		{Source: launchenv.SourceProcess, Values: map[string]string{"K": "proc"}},
		{Source: launchenv.SourceUserEnv, Values: map[string]string{"K": "user"}},
	})
	// 只允许 user 层，应拿到 user。
	if e, ok := s.GetFrom("K", []launchenv.Source{launchenv.SourceUserEnv}); !ok || e.Value != "user" {
		t.Fatalf("getFrom = %+v ok=%v", e, ok)
	}
}
