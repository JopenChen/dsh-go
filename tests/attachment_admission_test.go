// Package tests 的 attachment canonical base64 准入验收测试。
package tests

import (
	"testing"

	"github.com/JopenChen/dsh-go/pkg/attachment"
)

func TestCanonicalBase64OK(t *testing.T) {
	raw, err := attachment.DecodeCanonicalBase64("aGVsbG8=") // "hello"
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "hello" {
		t.Fatalf("decoded = %q", raw)
	}
}

func TestCanonicalBase64Reject(t *testing.T) {
	bad := []string{
		"",
		"aGVsbG8",    // 缺 padding
		"aGVsb G8=",  // 含空白
		"!!!",
	}
	for _, b := range bad {
		if _, err := attachment.DecodeCanonicalBase64(b); err != attachment.ErrInvalidBase64 {
			t.Fatalf("input %q should be rejected, got %v", b, err)
		}
	}
}
