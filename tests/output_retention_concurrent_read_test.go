// 本文件验证任务 S16：Output Retention（canonical value 保留到 committed + 并发读保护）。
//
// 覆盖：10MB 大结果并发 reader 各得完整不缺失；读返回独立副本（改写互不影响）；
// 未保留读取返回 false；逻辑删除语义。
package tests

import (
	"bytes"
	"context"
	"sync"
	"testing"

	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/tools"
)

// TestOutputRetentionConcurrentReaders 验证并发两个 reader 读同一个 10MB 结果各得完整字节。
func TestOutputRetentionConcurrentReaders(t *testing.T) {
	ctx := context.Background()
	_ = ctx
	ret := tools.NewResultRetention()

	// 构造 10MB canonical 值（带确定模式便于校验完整性）。
	const size = 10 * 1024 * 1024
	big := make([]byte, size)
	for i := range big {
		big[i] = byte(i % 251)
	}
	callID := brand.NewToolCallID("tc-big")
	ret.Retain(callID.Raw(), big)

	// 两个并发 reader。
	var wg sync.WaitGroup
	results := make([][]byte, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			got, ok := ret.Read(callID.Raw())
			if !ok {
				results[idx] = nil
				return
			}
			results[idx] = got
		}(i)
	}
	wg.Wait()

	for i, got := range results {
		if got == nil {
			t.Fatalf("reader[%d] 未读到结果", i)
		}
		if len(got) != size {
			t.Fatalf("reader[%d] 长度 %d != %d（缺失）", i, len(got), size)
		}
		if !bytes.Equal(got, big) {
			t.Fatalf("reader[%d] 内容与原始不一致（截断/损坏）", i)
		}
	}
	// 两个 reader 拿到的是独立副本（数组地址不同），改一个不影响另一个。
	results[0][0] = ^results[0][0]
	if results[1][0] == results[0][0] {
		t.Fatal("两个 reader 应持有独立副本")
	}
}

// TestOutputRetentionRetainResult 验证 RetainResult 抽取 canonical 值（含 struct→JSON）。
func TestOutputRetentionRetainResult(t *testing.T) {
	ret := tools.NewResultRetention()
	callID := brand.NewToolCallID("tc-json")
	res := &tools.ToolCallResult{
		CallID: callID,
		IsError: false,
		Value:  map[string]any{"ok": true, "lines": 3},
	}
	ret.RetainResult(res)

	got, ok := ret.Read(callID.Raw())
	if !ok {
		t.Fatal("RetainResult 后应可读")
	}
	// JSON 序列化应以完整字节保留。
	if !bytes.HasPrefix(got, []byte("{")) || !bytes.Contains(got, []byte(`"lines":3`)) {
		t.Fatalf("保留值为预期 JSON，实际 %s", got)
	}
}

// TestOutputRetentionString 验证 string 值按 []byte 保留。
func TestOutputRetentionString(t *testing.T) {
	ret := tools.NewResultRetention()
	ret.Retain("tc-str", []byte("beef"))
	got, ok := ret.Read("tc-str")
	if !ok || string(got) != "beef" {
		t.Fatalf("string 应保留原文，实际 %q ok=%v", got, ok)
	}
}

// TestOutputRetentionMissing 验证未保留读取返回 false。
func TestOutputRetentionMissing(t *testing.T) {
	ret := tools.NewResultRetention()
	if _, ok := ret.Read("nope"); ok {
		t.Fatal("未保留的 callID 应返回 false")
	}
	// 逻辑删除后读取返回 false。
	ret.Retain("gone", []byte("x"))
	ret.Remove("gone")
	if _, ok := ret.Read("gone"); ok {
		t.Fatal("逻辑删除后应返回 false")
	}
}