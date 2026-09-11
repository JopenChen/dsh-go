// 本文件实现会话沙箱模式覆盖的持久化：sandbox/mode 事件 + fold + set。
//
// 对齐官方：packages/sandbox/sandbox-policy/src/session-mode.ts
//
// 设计要点：
//   - 会话模式切换通过 sandbox/mode 事件持久化到事件日志（log-only，不进模型 transcript）；
//   - effective = fold(events) ?? 部署默认：取最后一个 sandbox/mode 事件的 mode；
//   - 重启后通过 replay 恢复，两个会话永远看不到彼此的状态（无外部配置存储）；
//   - source: 'delegation' 标记子代理委派时植入的覆盖；
//   - 执行层 SandboxPolicyService.Resolve() 遵循同一 fold，升级授权优先级最低。
package sandbox

import (
	"github.com/JopenChen/dsh-go/pkg/session"
)

// ModeSource 是 sandbox/mode 事件的来源标记。
type ModeSource string

const (
	// ModeSourceDelegation 标记子代理委派时植入的覆盖。
	ModeSourceDelegation ModeSource = "delegation"
)

// SandboxModes 是全部合法沙箱模式的常量数组（用于选项广告与运行时校验）。
var SandboxModes = []SandboxMode{
	ModeReadOnly,
	ModeWorkspaceWrite,
	ModeDangerFullAccess,
}

// EffectiveSandboxMode 从事件列表 fold 出会话的沙箱模式覆盖：
// 从后往前找最后一个 sandbox/mode 事件，返回其 mode；没有则返回 ok=false。
// 纯 fold 函数——resume 不需要 catch-up 机制，因为重放日志就是状态本身。
func EffectiveSandboxMode(events []session.SessionEvent) (SandboxMode, bool) {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type != session.EventSandboxMode {
			continue
		}
		data, ok := events[i].Data.(session.SandboxModeData)
		if !ok {
			continue
		}
		return SandboxMode(data.Mode), true
	}
	return "", false
}

// SessionAppender 是可追加事件的会话抽象（SessionLog 实现此接口）。
type SessionAppender interface {
	Append(data session.EventData) (uint64, error)
}

// SetSandboxMode 是会话沙箱模式覆盖的**唯一写路径**：
// 向会话 append 一条 sandbox/mode 事件——切换本身就是事件，不做任何带外状态修改。
// 在下一次受约束调用（bash 或 fs）时生效——消费者每次读取都 fold。
func SetSandboxMode(sess SessionAppender, mode SandboxMode) error {
	_, err := sess.Append(session.SandboxModeData{Mode: string(mode)})
	return err
}

// SetSandboxModeWithSource 同 SetSandboxMode，但携带 source 标记（如 'delegation'）。
func SetSandboxModeWithSource(sess SessionAppender, mode SandboxMode, source ModeSource) error {
	_, err := sess.Append(session.SandboxModeData{Mode: string(mode), Source: string(source)})
	return err
}
