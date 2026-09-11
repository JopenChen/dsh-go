// Package tests 的 llm BlockAssembler 验收测试。
package tests

import (
	"testing"

	"github.com/JopenChen/dsh-go/pkg/llm"
)

func TestBlockAssemblerMergesTextDeltas(t *testing.T) {
	a := llm.NewBlockAssembler()
	a.Push(llm.StreamChunk{Kind: llm.ChunkText, Text: "Hello "})
	a.Push(llm.StreamChunk{Kind: llm.ChunkText, Text: "World"})
	a.Push(llm.StreamChunk{Kind: llm.ChunkDone})
	blocks := a.Blocks()
	if len(blocks) != 1 || blocks[0].Text != "Hello World" {
		t.Fatalf("expected one merged text block, got %+v", blocks)
	}
}

func TestBlockAssemblerReasoningThenText(t *testing.T) {
	a := llm.NewBlockAssembler()
	a.Push(llm.StreamChunk{Kind: llm.ChunkReasoning, Reasoning: "think"})
	a.Push(llm.StreamChunk{Kind: llm.ChunkText, Text: "answer"})
	a.Push(llm.StreamChunk{Kind: llm.ChunkDone})
	blocks := a.Blocks()
	if len(blocks) != 2 {
		t.Fatalf("expected reasoning+text blocks, got %+v", blocks)
	}
	if blocks[0].Kind != llm.BlockReasoning || blocks[1].Kind != llm.BlockText {
		t.Fatalf("block kinds wrong: %+v", blocks)
	}
}

func TestBlockAssemblerToolCallOrder(t *testing.T) {
	a := llm.NewBlockAssembler()
	a.Push(llm.StreamChunk{Kind: llm.ChunkText, Text: "let me check"})
	a.Push(llm.StreamChunk{Kind: llm.ChunkToolCall, ToolCall: &llm.ToolCall{ID: "c1", Name: "ls"}})
	a.Push(llm.StreamChunk{Kind: llm.ChunkDone})
	blocks := a.Blocks()
	if len(blocks) != 2 || blocks[0].Kind != llm.BlockText || blocks[1].Kind != llm.BlockToolUse {
		t.Fatalf("expected text then tool-use, got %+v", blocks)
	}
	if blocks[1].ToolCall.Name != "ls" {
		t.Fatalf("tool call name wrong: %+v", blocks[1].ToolCall)
	}
}

func TestBlockAssemblerMessage(t *testing.T) {
	a := llm.NewBlockAssembler()
	a.Push(llm.StreamChunk{Kind: llm.ChunkText, Text: "hi"})
	a.Push(llm.StreamChunk{Kind: llm.ChunkDone})
	msg := a.Message()
	if msg.Role != llm.RoleAssistant || len(msg.Content) != 1 {
		t.Fatalf("assistant message wrong: %+v", msg)
	}
}

func TestBlockAssemblerEmpty(t *testing.T) {
	a := llm.NewBlockAssembler()
	if len(a.Blocks()) != 0 {
		t.Fatal("empty assembler should yield no blocks")
	}
}
