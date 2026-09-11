// Package tests 的 instructions 指令发现验收测试。
package tests

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JopenChen/dsh-go/pkg/instructions"
)

func TestFindProjectRoot(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "proj")
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := instructions.FindProjectRoot(sub, []string{".git"})
	if got != root {
		t.Fatalf("root = %q want %q", got, root)
	}
}

func TestAncestorChain(t *testing.T) {
	root, _ := filepath.Abs(filepath.Clean("/p"))
	cwd := filepath.Join(root, "a", "b")
	chain := instructions.AncestorChain(root, cwd)
	if len(chain) != 3 || chain[0] != root || chain[2] != cwd {
		t.Fatalf("chain wrong: %v", chain)
	}
}

func TestDedupByDirectory(t *testing.T) {
	files := []instructions.LoadedFile{
		{DisplayPath: filepath.Clean("a/AGENTS.md"), Content: "x"},
		{DisplayPath: filepath.Clean("a/CLAUDE.md"), Content: " x "}, // 同目录 trim 相同 → 去
		{DisplayPath: filepath.Clean("b/AGENTS.md"), Content: "x"},   // 不同目录 → 留
	}
	kept := instructions.DedupByDirectory(files)
	if len(kept) != 2 {
		t.Fatalf("kept = %d want 2: %+v", len(kept), kept)
	}
}
