// aborted.go 复刻官方 session-checkpoint-policy 的"派发前取消"语义：当请求在
// 真正进入工具体之前就已被取消时，返回一个带稳定错误码的 isError 结果，而不是
// 带着半截状态进入工具实现。
package tools

import "github.com/JopenChen/dsh-go/pkg/brand"

// AbortedBeforeDispatch 是"工具在派发前被取消"的稳定错误码。
const AbortedBeforeDispatch = "TOOL_ABORTED_BEFORE_DISPATCH"

// AbortedBeforeDispatchMessage 是模型可见的提示文本。
const AbortedBeforeDispatchMessage = "tool call aborted before dispatch"

// AbortedBeforeDispatchResult 构造派发前取消的结果。
func AbortedBeforeDispatchResult(callID brand.ToolCallID) *ToolCallResult {
	return &ToolCallResult{
		CallID:    callID,
		IsError:   true,
		Error:     AbortedBeforeDispatchMessage,
		ErrorCode: AbortedBeforeDispatch,
	}
}
