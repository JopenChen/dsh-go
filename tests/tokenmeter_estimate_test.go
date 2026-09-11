// Package tests 的 tokenmeter 固定密度估算验收测试。
package tests

import (
	"testing"

	"github.com/JopenChen/dsh-go/pkg/llm"
	"github.com/JopenChen/dsh-go/pkg/tokenmeter"
)

func TestEstimateTextMessage(t *testing.T) {
	// 8 字符文本 → ceil(8/4)=2 + blockOverhead 4 + roleOverhead 4 = 10
	msg := llm.NewUserMessage("12345678")
	got := tokenmeter.EstimateMessage(msg)
	if got != 10 {
		t.Fatalf("expected 10 tokens, got %d", got)
	}
}

func TestEstimateContentText(t *testing.T) {
	blocks := []llm.ContentBlock{llm.Text("abcd")}
	// ceil(4/4)=1 + 4 = 5
	if got := tokenmeter.EstimateContent(blocks); got != 5 {
		t.Fatalf("expected 5, got %d", got)
	}
}

func TestEstimateToolUseBlock(t *testing.T) {
	blocks := []llm.ContentBlock{
		llm.ToolUse(&llm.ToolCall{Name: "ls", Input: map[string]any{"a": 1}}),
	}
	got := tokenmeter.EstimateContent(blocks)
	if got <= 0 {
		t.Fatalf("tool-use block should cost positive tokens, got %d", got)
	}
}

func TestEstimateMessagesSum(t *testing.T) {
	msgs := []llm.Message{
		llm.NewUserMessage("hello"),
		llm.NewAssistantText("hi"),
	}
	if got := tokenmeter.EstimateMessages(msgs); got <= 0 {
		t.Fatalf("sum should be positive, got %d", got)
	}
}

func TestEstimateEmpty(t *testing.T) {
	if got := tokenmeter.EstimateContent(nil); got != 0 {
		t.Fatalf("empty content should be 0, got %d", got)
	}
}
