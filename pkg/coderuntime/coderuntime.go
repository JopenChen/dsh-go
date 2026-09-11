// Package coderuntime 定义代码执行缝（Code Execution Seam）的协议层抽象。
//
// 对齐上游 packages/code-runtime：PTC 模式让模型写一段程序（TS/Python），程序通过
// 注入的命名空间（如 tools）调用宿主能力。CodeRuntime 就是"执行这段程序"的缝。
//
// 重要取舍：上游唯一发布的后端是 worker-thread（Node 工作线程执行 TypeScript）。
// Go 进程内没有 JS 引擎，因此本包只固化**协议契约**（请求/结果/失败分类 + 接口），
// 不提供具体语言引擎。具体后端由使用方实现 CodeRuntime 接口——可以是外部子进程、
// 嵌入式解释器，或远程执行服务。这样 PTC 的上层语义（run_code 桥、子调度记录）
// 可以完整复刻，而执行后端保持可替换。
package coderuntime

import "context"

// BindingFunction 是暴露给程序的一个宿主异步函数。
// 运行时可能跨序列化边界桥接调用，因此入参参与解析值必须是无损 JSON。
// 该函数的拒绝会在程序内表现为对应调用的 rejection。
type BindingFunction func(ctx context.Context, args any) (any, error)

// BindingNamespace 是运行时暴露给程序的一个命名全局对象（如 tools）。
type BindingNamespace struct {
	// Global 程序看到的全局标识（可移植标识符 [A-Za-z_][A-Za-z0-9_]*）。
	Global string
	// Functions 成员函数，键为程序调用的确切名字。
	Functions map[string]BindingFunction
	// ErrorClass 可选：该命名空间的类型化错误类名（程序内成员拒绝成为其实例）。
	ErrorClass string
}

// FailureKind 是一次运行的失败分类（正交、彼此独立）。
type FailureKind string

// 失败分类枚举，对齐上游 CodeRunFailure.kind。
const (
	// FailException 程序抛出或解析/转换失败。
	FailException FailureKind = "exception"
	// FailTimeout 实现拥有的预算到期。
	FailTimeout FailureKind = "timeout"
	// FailAbort 请求的取消信号触发。
	FailAbort FailureKind = "abort"
	// FailWorkerExit 执行基质未结算即死亡（如 OOM）。
	FailWorkerExit FailureKind = "worker-exit"
	// FailInvalidOutput 完成值不是无损 JSON。
	FailInvalidOutput FailureKind = "invalid-output"
	// FailOutputLimit 序列化外层 logs/value/诊断超过上限。
	FailOutputLimit FailureKind = "output-limit"
)

// RunFailure 是一次运行的失败描述。
type RunFailure struct {
	// Kind 失败类别。
	Kind FailureKind
	// Message 可读细节，适合回喂模型自我纠正。
	Message string
}

// RunRequest 是一次运行：程序源 + 运行时作用的一切。
type RunRequest struct {
	// Program 程序源，以运行时语言编写，作为 async 函数体运行（顶层 await/return 可用）。
	Program string
	// Bindings 暴露给程序的宿主命名空间。
	Bindings []BindingNamespace
	// Ctx 取消运行：硬停止（即便在循环中），结果为 FailAbort。
	Ctx context.Context
}

// RunResult 是一次运行的结果。错误是结果的字段，而不是 Run 的 Go error——
// 报告一个失败的程序是调用方的职责，不是异常路径。
type RunResult struct {
	// Value 程序完成值（顶层 return），失败或无值时为 nil。
	Value any
	// Logs 程序按顺序输出的文本。
	Logs []string
	// Failure 非 nil 表示运行失败。
	Failure *RunFailure
}

// Failed 便捷判定是否失败。
func (r *RunResult) Failed() bool { return r.Failure != nil }

// Runtime 是代码执行缝接口：具体后端实现它。
type Runtime interface {
	// Language 源语言（小写标识，如 "typescript"/"python"），信息性而非门禁。
	Language() string
	// Isolation 执行基质（如 "worker-thread"/"process"/"container"），信息性。
	Isolation() string
	// Run 执行一次程序并捕获输出；仅当缝契约被误用（而非程序失败）时返回 Go error。
	Run(req *RunRequest) (*RunResult, error)
}
