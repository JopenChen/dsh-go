// Package tests 的 workspace 路径辅助验收测试。
package tests

import (
	"path/filepath"
	"testing"

	"github.com/JopenChen/dsh-go/pkg/workspace"
)

func TestResolvePath(t *testing.T) {
	want := filepath.Join("/root", "a/b")
	if got := workspace.ResolvePath("/root", "a/b"); got != want {
		t.Fatalf("join = %q want %q", got, want)
	}
	if got := workspace.ResolvePath("/root", "/abs"); got != "/abs" {
		t.Fatalf("abs unchanged = %q", got)
	}
}

func TestAbbreviateHome(t *testing.T) {
	if got := workspace.AbbreviateHome("/home/u", "/home/u"); got != "~" {
		t.Fatalf("home = %q", got)
	}
	if got := workspace.AbbreviateHome("/home/u/x", "/home/u"); got != "~/x" {
		t.Fatalf("sub = %q", got)
	}
	if got := workspace.AbbreviateHome("/other", "/home/u"); got != "/other" {
		t.Fatalf("outside unchanged = %q", got)
	}
}

func TestTitleOf(t *testing.T) {
	if got := workspace.TitleOf("/a/b/c"); got != "c" {
		t.Fatalf("title = %q", got)
	}
}
