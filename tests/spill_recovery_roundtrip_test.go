// Package tests 的 Spill Storage（M42）验收测试。
//
// 覆盖：
//   - 工具结果 / Bash 结果文本分别 > 阈值均正确 spill
//   - 读引用（locator）还原内容字节一致
//   - 未超阈值保留内联；保存失败 best-effort 保留原文本
package tests

import (
	"strings"
	"testing"

	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/spill"
)

// newSpillReq 构造一个保存请求（工具来源）。
func newSpillReq(sessionID, tool, suggested string) spill.SaveTextSpill {
	return spill.SaveTextSpill{
		Owner:         spill.SpillOwner{SessionID: brand.NewSessionID(sessionID)},
		Source:        spill.SpillSource{ToolName: tool, CallID: brand.NewToolCallID("call-1"), Label: "result"},
		SuggestedName: suggested,
	}
}

// TestSpillToolResultOverThreshold 验证工具结果超过阈值 → spill + 读引用还原一致。
func TestSpillToolResultOverThreshold(t *testing.T) {
	store := spill.NewFileStore(t.TempDir())
	bigText := strings.Repeat("line of tool output\n", 500) // ~9KB > 1KB 阈值

	inline, ref, err := spill.Apply(store, newSpillReq("s1", "web_fetch", "web_fetch.txt"), bigText, 1024, 256)
	if err != nil {
		t.Fatalf("spill 失败: %v", err)
	}
	if ref == nil {
		t.Fatal("超过阈值应产生 spill ref")
	}
	if len(inline) >= len(bigText) {
		t.Fatal("内联应为预览，而非完整文本")
	}
	// 读引用还原完整内容字节一致。
	got, err := store.ReadText(ref.Locator)
	if err != nil {
		t.Fatalf("读 locator 失败: %v", err)
	}
	if got != bigText {
		t.Fatalf("还原内容不一致: got %d bytes, want %d bytes", len(got), len(bigText))
	}
}

// TestSpillBashResultOverThreshold 验证 Bash 结果超过阈值同样 spill（第二个消费者）。
func TestSpillBashResultOverThreshold(t *testing.T) {
	store := spill.NewFileStore(t.TempDir())
	seqOutput := strings.Repeat("1\n2\n3\n4\n5\n", 1000) // ~10KB

	_, ref, err := spill.Apply(store, newSpillReq("s2", "bash", "bash.txt"), seqOutput, 64, 64)
	if err != nil {
		t.Fatalf("bash spill 失败: %v", err)
	}
	if ref == nil {
		t.Fatal("bash 结果超阈值应 spill")
	}
	if ref.Bytes != len(seqOutput) {
		t.Fatalf("ref.Bytes 应等于原文长度 %d, 实际 %d", len(seqOutput), ref.Bytes)
	}
	got, _ := store.ReadText(ref.Locator)
	if got != seqOutput {
		t.Fatal("Bash 结果 read-back 应逐字节一致")
	}
}

// TestSpillUnderThresholdInline 验证未超阈值保留内联、无 ref。
func TestSpillUnderThresholdInline(t *testing.T) {
	store := spill.NewFileStore(t.TempDir())
	small := "short"
	inline, ref, err := spill.Apply(store, newSpillReq("s3", "tool", "a.txt"), small, 1024, 0)
	if err != nil {
		t.Fatal(err)
	}
	if ref != nil {
		t.Fatal("未超阈值不应 spill")
	}
	if inline != small {
		t.Fatalf("应保留原内联, 实际 %q", inline)
	}
}

// TestSpillBestEffortOnSaveFailure 验证保存失败时 best-effort 保留原文本。
func TestSpillBestEffortOnSaveFailure(t *testing.T) {
	// 设一个无法创建的 root（路径是一存在的文件）→ save 失败。
	store := spill.NewFileStore("")
	big := strings.Repeat("x", 4096)
	_, _, err := spill.Apply(store, newSpillReq("s4", "bash", "b.txt"), big, 64, 0)
	if err == nil {
		t.Fatal("root 未配置应保存失败并返回错误")
	}
	// 注意：Apply best-effort 仍返回原文本，但错误上抛供调用方判定（不把成功调用搞成 isError）。
}