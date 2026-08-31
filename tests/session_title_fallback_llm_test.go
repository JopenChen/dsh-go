// 本文件验证任务 S08：Session Title（latest-wins fold + fallback + LLM helper）。
//
// 覆盖场景：
//   - Fallback：首条用户消息前缀 30 字截断（含多字节字符不切坏）；
//   - LLM 未启用 → 强制走 fallback；
//   - LLM 正常产出 → Source=llm，标题被采纳；
//   - LLM 失败 / 产出为空 → 回退到 fallback，Source=fallback；
//   - （可选）与 session 事件折叠联动：写入 session/title 后 FoldSessionTitle 能读出。
package tests

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/llm"
	"github.com/JopenChen/dsh-go/pkg/session"
	"github.com/JopenChen/dsh-go/pkg/sessiontitle"
)

// ============================================================================
// Mock LLMAdapter
// ============================================================================

// mockLLM 是可控的 LLMAdapter 双桩。
type mockLLM struct {
	// text 每次 Chat 回放的文本流；为空则立即返回 err（模拟 LLM 失败）。
	text string
	err  error
}

func (m *mockLLM) Name() string { return "mock-title" }

func (m *mockLLM) Chat(ctx context.Context, req llm.ChatRequest, cb func(llm.StreamChunk)) (llm.Usage, error) {
	if m.err != nil {
		return llm.Usage{}, m.err
	}
	// 按字符流式回放，模拟 text 分片。
	for _, r := range m.text {
		cb(llm.StreamChunk{Kind: llm.ChunkText, Text: string(r)})
	}
	cb(llm.StreamChunk{Kind: llm.ChunkDone})
	return llm.Usage{}, nil
}

// ============================================================================
// 测试用例
// ============================================================================

// TestFallbackPrefix 验证 30 字前缀截断与多字节安全。
func TestFallbackPrefix(t *testing.T) {
	long := "这是一个非常长的用户消息，用来验证标题前缀会被截断到三十个字以内，后面的内容就不再出现在标题里了。"
	got := sessiontitle.Fallback(long)
	if len([]rune(got)) != 30 && len([]rune(got)) != 31 { // 30 字 + 可能的「…」
		t.Fatalf("fallback 应截断到约 30 字(不含…计30字)，实际 %d 字(%q)", len([]rune(got)), got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("超长输入应以省略号结尾，实际 %q", got)
	}
	// 多字节安全：截断点不得产生不合法的 UTF-8 序列。
	if !isValidUTF8(got) {
		t.Fatalf("fallback 输出包含非法 UTF-8: %q", got)
	}
}

// TestFallbackShort 验证短消息原样保留（不追加省略号）。
func TestFallbackShort(t *testing.T) {
	got := sessiontitle.Fallback("修复登录报错")
	if got != "修复登录报错" {
		t.Fatalf("短消息应原样返回，实际 %q", got)
	}
}

// TestFallbackEmpty 验证空内容返回空标题。
func TestFallbackEmpty(t *testing.T) {
	if got := sessiontitle.Fallback("   "); got != "" {
		t.Fatalf("空白内容应返回空标题，实际 %q", got)
	}
}

// TestNoLLMFallsBack 验证未启用 LLM 时强制走 fallback。
func TestNoLLMFallsBack(t *testing.T) {
	gen := &sessiontitle.Generator{} // LLM 为 nil
	res := gen.Generate(context.Background(), "请帮我部署一个 go 服务")
	if res.Source != sessiontitle.SourceFallback {
		t.Fatalf("无 LLM 时应 Source=fallback，实际 %v", res.Source)
	}
	if res.Title == "" {
		t.Fatal("fallback 标题不能为空")
	}
}

// TestLLMGenerated 验证 LLM 正常产出时标题被采纳。
func TestLLMGenerated(t *testing.T) {
	gen := &sessiontitle.Generator{LLM: &mockLLM{text: "【部署 Go 服务】"}}
	res := gen.Generate(context.Background(), "请帮我部署一个 go 服务")
	if res.Source != sessiontitle.SourceLLM {
		t.Fatalf("LLM 成功时应 Source=llm，实际 %v", res.Source)
	}
	// 清理引号类符号后应为纯标题。
	if trimmed := sessiontitle.TrimTitle(res.Title); sessiontitle.TrimTitle(trimmed) == "" {
		t.Fatalf("LLM 标题为空: %q", res.Title)
	}
	if strings.Contains(res.Title, "【") || strings.Contains(res.Title, "】") {
		t.Fatalf("LLM 标题未清理装饰符号: %q", res.Title)
	}
	if res.Title != "部署 Go 服务" {
		t.Fatalf("LLM 标题清理后应为「部署 Go 服务」，实际 %q", res.Title)
	}
}

// TestLLMFailureFallsBack 验证 LLM 失败时回退到 fallback。
func TestLLMFailureFallsBack(t *testing.T) {
	gen := &sessiontitle.Generator{LLM: &mockLLM{err: &llm.LlmFailure{Kind: llm.FailOverload, Message: "overloaded"}}}
	res := gen.Generate(context.Background(), "请帮我部署一个 go 服务")
	if res.Source != sessiontitle.SourceFallback {
		t.Fatalf("LLM 失败应回退 fallback，实际 %v", res.Source)
	}
	if res.Title != "请帮我部署一个 go 服务" {
		t.Fatalf("回退标题应为首条消息前缀，实际 %q", res.Title)
	}
}

// TestLLMEmptyOutputFallsBack 验证 LLM 产出空串时回退到 fallback。
func TestLLMEmptyOutputFallsBack(t *testing.T) {
	gen := &sessiontitle.Generator{LLM: &mockLLM{text: "  \n  "}}
	res := gen.Generate(context.Background(), "帮我写测试用例")
	if res.Source != sessiontitle.SourceFallback {
		t.Fatalf("LLM 空输出应回退 fallback，实际 %v", res.Source)
	}
	if res.Title != "帮我写测试用例" {
		t.Fatalf("回退标题应为首条消息前缀，实际 %q", res.Title)
	}
}

// TestTitleEventFold 验证 session/title 事件写入后 FoldSessionTitle 能 latest-wins 读出。
func TestTitleEventFold(t *testing.T) {
	sl := session.NewSessionLog(brand.NewSessionID("title-s1"))
	// 先写一个旧标题，再写新标题 → fold 取最新。
	if _, err := sl.Append(session.SessionTitleData{Title: "旧标题"}); err != nil {
		t.Fatalf("追加旧标题事件失败: %v", err)
	}
	if _, err := sl.Append(session.SessionTitleData{Title: "新标题"}); err != nil {
		t.Fatalf("追加新标题事件失败: %v", err)
	}
	proj := session.FoldAllFromLog(sl)
	if !proj.Title.Present || proj.Title.Title != "新标题" {
		t.Fatalf("latest-wins fold 应为「新标题」，实际 %+v", proj.Title)
	}
}

// isValidUTF8 判断字符串是否为合法 UTF-8（用于校验截断未切坏多字节字符）。
func isValidUTF8(s string) bool {
	return utf8.ValidString(s)
}