// Package commands 提供 slash 命令（Commands）系统。
//
// 对齐上游：packages/interaction/commands
//
// 设计要点：
//   - CommandDefinition 描述一个 slash 命令（如 /plan off）；
//   - 命令入口通过 ctx.commands register/list/execute 路由；
//   - 执行 `#command/run` 与 `#command/done` 事件（而非当成普通 user/message）；
//   - 例如 `/plan off` 直接写入 plan/mode(off) 事件，而不是作为对话消息。
package commands

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/registry"
	"github.com/JopenChen/dsh-go/pkg/session"
)

// CommandHandler 是命令处理函数：接收参数字符串与事件日志，写事件并返回输出。
type CommandHandler func(ctx context.Context, args string, sl *session.SessionLog) (output string, err error)

// CommandDefinition 是一个 slash 命令定义。
type CommandDefinition struct {
	// Name 命令名（不含 "/" 前缀，如 "plan"）。
	Name string
	// Description 命令描述（帮助展示）。
	Description string
	// Handler 执行函数。
	Handler CommandHandler
}

// Registry 是命令注册中心（H07：基于可冻结共享注册表，Freeze 后读路径无锁）。
type Registry struct {
	cmds *registry.Freezable[string, *CommandDefinition]
}

// NewRegistry 创建命令注册中心，并注册内置 plan/goal 命令。
func NewRegistry() *Registry {
	r := &Registry{cmds: registry.NewFreezable[string, *CommandDefinition]()}
	_ = r.cmds.Put("plan", &CommandDefinition{Name: "plan", Description: "切换计划模式 (on/off)", Handler: handlePlan})
	_ = r.cmds.Put("goal", &CommandDefinition{Name: "goal", Description: "设置目标", Handler: handleGoal})
	return r
}

// Register 注册一个命令；冻结后返回 ErrFrozen。
func (r *Registry) Register(def *CommandDefinition) error {
	if def == nil || def.Name == "" {
		return fmt.Errorf("commands: name required")
	}
	return r.cmds.Put(def.Name, def)
}

// Freeze 冻结命令注册表（不可逆）：此后读路径无锁走只读快照，写返回 ErrFrozen。
func (r *Registry) Freeze() { r.cmds.Freeze() }

// IsFrozen 返回注册表是否已冻结。
func (r *Registry) IsFrozen() bool { return r.cmds.IsFrozen() }

// List 返回全部命令名（字典序）。
func (r *Registry) List() []string {
	names := r.cmds.Keys()
	sort.Strings(names)
	return names
}

// Get 按名取命令（冻结后无锁读快照）。
func (r *Registry) Get(name string) (*CommandDefinition, bool) {
	return r.cmds.Get(name)
}

// Dispatch 解析 "/name args" 并执行对应命令（不存在返回 ErrUnknownCommand）。
func (r *Registry) Dispatch(ctx context.Context, raw string, sl *session.SessionLog) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '/' {
		return "", fmt.Errorf("commands: not a command: %q", raw)
	}
	line := trimmed[1:]
	parts := strings.SplitN(line, " ", 2)
	name := strings.TrimSpace(parts[0])
	args := ""
	if len(parts) == 2 {
		args = strings.TrimSpace(parts[1])
	}

	def, ok := r.Get(name)
	if !ok {
		return "", ErrUnknownCommand(name)
	}

	// 写 command/run 事件
	_, _ = sl.Append(session.CommandRunData{Command: "/" + name, Args: args})

	// 执行处理函数
	output, err := def.Handler(ctx, args, sl)
	if err != nil {
		return "", err
	}

	// 写 command/done 事件
	_, _ = sl.Append(session.CommandDoneData{Command: "/" + name})
	return output, nil
}

// ErrUnknownCommand 表示命令不存在。
type ErrUnknownCommand string

func (e ErrUnknownCommand) Error() string { return fmt.Sprintf("commands: unknown command %q", string(e)) }

// handlePlan 处理 /plan：写入 plan/mode(off|on) 事件而非 user/message。
func handlePlan(ctx context.Context, args string, sl *session.SessionLog) (string, error) {
	mode := "off"
	if args == "on" {
		mode = "on"
	}
	if _, err := sl.Append(session.PlanModeData{Mode: mode}); err != nil {
		return "", err
	}
	return "plan mode = " + mode, nil
}

// handleGoal 处理 /goal：写入 goal/change 事件（CAS revision 从 fold 派生）。
func handleGoal(ctx context.Context, args string, sl *session.SessionLog) (string, error) {
	fold := session.FoldGoalChange(sl.Events())
	revision := fold.Revision + 1
	desc := args
	if desc == "" {
		desc = "(空目标)"
	}
	if _, err := sl.Append(session.GoalChangeData{
		GoalID:    brand.NewSessionID("g_" + itoa64(revision)).Raw(),
		Phase:     "active",
		Description: desc,
		MaxRounds: 5,
		Revision:  revision,
	}); err != nil {
		return "", err
	}
	return "goal set: " + desc, nil
}

// itoa64 最小 uint64 转字符串。
func itoa64(n uint64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}