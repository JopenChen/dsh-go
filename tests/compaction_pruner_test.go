// Package tests 的 compaction 文本修剪验收测试。
package tests

import (
	"strings"
	"testing"

	"github.com/JopenChen/dsh-go/pkg/compaction"
)

func TestPruneTextWithin(t *testing.T) {
	out, changed := compaction.PruneText("short", 100, 10, 10)
	if changed || out != "short" {
		t.Fatalf("within budget unchanged, got %q %v", out, changed)
	}
}

func TestPruneTextHeadTail(t *testing.T) {
	text := strings.Repeat("a", 50)
	out, changed := compaction.PruneText(text, 20, 5, 5)
	if !changed {
		t.Fatal("over budget should prune")
	}
	if !strings.HasPrefix(out, "aaaaa") || !strings.HasSuffix(out, "aaaaa") {
		t.Fatalf("head/tail not retained: %q", out)
	}
	if !strings.Contains(out, compaction.PruneMarker) {
		t.Fatal("marker present")
	}
}

func TestPruneTextRuneSafe(t *testing.T) {
	text := strings.Repeat("界", 30)
	out, changed := compaction.PruneText(text, 10, 2, 2)
	if !changed {
		t.Fatal("should prune")
	}
	// 不产生替换符 U+FFFD 即未拆 rune
	if strings.Contains(out, "\uFFFD") {
		t.Fatalf("rune split: %q", out)
	}
}
