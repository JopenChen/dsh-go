// Package tests 的 sessiontitle 归一化验收测试。
package tests

import (
	"testing"

	"github.com/JopenChen/dsh-go/pkg/sessiontitle"
)

func TestCleanTitle(t *testing.T) {
	in := "\x1b[31ma\x1b[0m   b\x00c\t d"
	got := sessiontitle.CleanTitle(in)
	// 控制字符剔除（bc 相邻）、连续空白压缩（a|b、c|d 各保留一个空格）。
	if got != "a bc d" {
		t.Fatalf("clean = %q", got)
	}
}

func TestTruncateUTF8(t *testing.T) {
	// 每个中文字符 3 字节，预算 7 只能放 2 个完整字符。
	if got := sessiontitle.TruncateUTF8("你好世界", 7); got != "你好" {
		t.Fatalf("truncate = %q", got)
	}
}

func TestNormalizeTitle(t *testing.T) {
	if got := sessiontitle.Normalize("  hello   world  ", 100); got != "hello world" {
		t.Fatalf("normalize = %q", got)
	}
}
