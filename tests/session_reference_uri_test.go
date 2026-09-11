// Package tests 的 session-reference URI 验收测试。
package tests

import (
	"testing"

	"github.com/JopenChen/dsh-go/pkg/sessionquery"
)

func TestSessionURIRoundTrip(t *testing.T) {
	id := "sess-abc-123"
	uri := sessionquery.EncodeURI(id)
	got, err := sessionquery.DecodeURI(uri)
	if err != nil || got != id {
		t.Fatalf("round trip got %q err=%v", got, err)
	}
}

func TestSessionURIRejectBad(t *testing.T) {
	if _, err := sessionquery.DecodeURI("http://x"); err != sessionquery.ErrInvalidURI {
		t.Fatalf("wrong scheme rejected, got %v", err)
	}
	if _, err := sessionquery.DecodeURI(sessionquery.Scheme + "!!!"); err != sessionquery.ErrInvalidURI {
		t.Fatalf("bad payload rejected, got %v", err)
	}
}

func TestSessionMention(t *testing.T) {
	m := sessionquery.FormatMention("sid", "标签")
	if !startsWith(m, "@[标签](") {
		t.Fatalf("mention wrong: %s", m)
	}
}

func startsWith(s, p string) bool {
	return len(s) >= len(p) && s[:len(p)] == p
}
