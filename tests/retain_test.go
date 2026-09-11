// Package tests 的 retain 有界保留验收测试。
package tests

import (
	"testing"

	"github.com/JopenChen/dsh-go/pkg/retain"
)

func TestItemRetainerHead(t *testing.T) {
	r := retain.NewItemRetainer[int](3)
	for i := 0; i < 5; i++ {
		r.Push(i)
	}
	res := r.Finish()
	if len(res.Items) != 3 || res.Items[0] != 0 || res.Items[2] != 2 {
		t.Fatalf("kept wrong: %+v", res.Items)
	}
	if res.Seen != 5 || res.Omitted != 2 || !res.Truncated {
		t.Fatalf("omission wrong: seen=%d omitted=%d trunc=%v", res.Seen, res.Omitted, res.Truncated)
	}
}

func TestItemRetainerNoTrunc(t *testing.T) {
	r := retain.NewItemRetainer[string](10)
	r.Push("a")
	res := r.Finish()
	if res.Truncated || res.Omitted != 0 || len(res.Items) != 1 {
		t.Fatalf("should not truncate: %+v", res)
	}
}
