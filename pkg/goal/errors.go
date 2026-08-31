// 本文件对应 code-review 修复点 R06：goal 对齐官方稳定失败分类与阻塞原因。
//
// 对照上游：D:\workspace\python_workspace\deepseek-harness\packages\goal\goal\src\
//   - types.ts  `GoalPhase = 'active'|'paused'|'blocked'|'complete'`；`GoalBlockReason{code,message}`；
//   - domain.ts `GoalErrorCode` 9 个稳定机器路由错误码。
//
// 设计：
//   - ErrorCode 是稳定机器路由码（机器绝不做字符串匹配 message）；
//   - GoalError 持 Code + Message + Cause（Go 1.13 error 链，errors.Is/As 可路由）；
//   - GoalBlockReason{Code,Message} 与官方 blocked 语义一致（blocked 时必须带）。
package goal

import "fmt"

// ErrorCode 是 goal 域稳定机器路由错误码（对应官方 GoalErrorCode）。
type ErrorCode string

// 稳定错误码（官方 9 种逐一对齐）。
const (
	ErrorAgentNotLive        ErrorCode = "GOAL_AGENT_NOT_LIVE"
	ErrorNotFound            ErrorCode = "GOAL_NOT_FOUND"
	ErrorAlreadyExists       ErrorCode = "GOAL_ALREADY_EXISTS"
	ErrorStaleRevision       ErrorCode = "GOAL_STALE_REVISION"
	ErrorInvalidObjective    ErrorCode = "GOAL_INVALID_OBJECTIVE"
	ErrorInvalidMaxRounds    ErrorCode = "GOAL_INVALID_MAX_ROUNDS"
	ErrorInvalidBlockReason  ErrorCode = "GOAL_INVALID_BLOCK_REASON"
	ErrorInvalidEdit         ErrorCode = "GOAL_INVALID_EDIT"
	ErrorInvalidTransition   ErrorCode = "GOAL_INVALID_TRANSITION"
)

// GoalError 是带稳定 Code 的 goal 域错误。
type GoalError struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
	Cause   error     `json:"-"`
}

// Error 实现 error 接口。
func (e *GoalError) Error() string {
	return fmt.Sprintf("goal error [%s]: %s", e.Code, e.Message)
}

// Unwrap 支持 errors.Is/errors.As 沿 Cause 下钻。
func (e *GoalError) Unwrap() error { return e.Cause }

// NewGoalError 构造一个带稳定 Code 的 GoalError。
func NewGoalError(code ErrorCode, message string, cause error) *GoalError {
	return &GoalError{Code: code, Message: message, Cause: cause}
}

// FromError 把任意 error 归类为 GoalError；已是 GoalError 则原样返回；其它归 unknown。
func FromError(err error) *GoalError {
	if err == nil {
		return nil
	}
	if ge, ok := err.(*GoalError); ok {
		return ge
	}
	return NewGoalError(ErrorCode("unknown"), err.Error(), err)
}

// GoalBlockReason 是阻塞目标的稳定原因（官方 GoalBlockReason）。
type GoalBlockReason struct {
	// Code 稳定的 lower-kebab-case 分类。
	Code string `json:"code"`
	// Message 非空的面向人/模型的说明。
	Message string `json:"message"`
}

// NewBlockReason 构造阻塞原因，返回是否合法（code/message 非空）。
func NewBlockReason(code, message string) (GoalBlockReason, bool) {
	if code == "" || message == "" {
		return GoalBlockReason{}, false
	}
	return GoalBlockReason{Code: code, Message: message}, true
}