// 本文件复刻官方 packages/llm/llm/src/assembler.ts 的核心算法，适配本项目简化版
// StreamChunk 协议（text/reasoning/tool-call/done）。
//
// Agent 循环一遍接收流式分片、一遍把原始分片写入日志，BlockAssembler 是唯一权威的
// "分片 → 助手消息"组装算法：连续 text/reasoning 增量合并，tool-call 在到达时固化为
// 工具使用块，最终输出严格保持流顺序。
package llm

// BlockAssembler 增量地把 StreamChunk 组装为 ContentBlock。
// 零值不可用，请用 NewBlockAssembler。
type BlockAssembler struct {
	curKind  StreamChunkKind // 当前正在累积的增量类型（"" 表示无打开缓冲）
	curBuf   string
	blocks   []ContentBlock
	finished bool
}

// NewBlockAssembler 创建空组装器。
func NewBlockAssembler() *BlockAssembler {
	return &BlockAssembler{}
}

// flush 把当前打开的 text/reasoning 缓冲固化为一个块。
func (a *BlockAssembler) flush() {
	if a.curKind == "" {
		return
	}
	if a.curBuf != "" {
		a.blocks = append(a.blocks, ContentBlock{
			Kind: BlockReasoning,
			Text: a.curBuf,
		})
		if a.curKind == ChunkText {
			a.blocks[len(a.blocks)-1].Kind = BlockText
		}
	}
	a.curKind = ""
	a.curBuf = ""
}

// Push 按流顺序喂入一个分片。
func (a *BlockAssembler) Push(c StreamChunk) {
	switch c.Kind {
	case ChunkText:
		if a.curKind != ChunkText {
			a.flush()
			a.curKind = ChunkText
		}
		a.curBuf += c.Text
	case ChunkReasoning:
		if a.curKind != ChunkReasoning {
			a.flush()
			a.curKind = ChunkReasoning
		}
		a.curBuf += c.Reasoning
	case ChunkToolCall:
		a.flush()
		if c.ToolCall != nil {
			a.blocks = append(a.blocks, ToolUse(c.ToolCall))
		}
	case ChunkDone:
		a.flush()
		a.finished = true
	}
}

// Blocks 返回截至目前组装好的块（拷贝，保持流顺序）；未结束的打开缓冲也会被组装。
func (a *BlockAssembler) Blocks() []ContentBlock {
	a.flush()
	out := make([]ContentBlock, len(a.blocks))
	copy(out, a.blocks)
	return out
}

// Message 用组装好的块构造一条助手消息。
func (a *BlockAssembler) Message() Message {
	return Message{Role: RoleAssistant, Content: a.Blocks()}
}

// Finished 是否已收到 done。
func (a *BlockAssembler) Finished() bool { return a.finished }
