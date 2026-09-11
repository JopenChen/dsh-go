// 本文件实现本地后端的 profile 参数构建：bwrap / landlock / seatbelt。
//
// 对齐官方：packages/sandbox/sandbox-local/src/profiles.ts
//
// 设计要点：
//   - BwrapProfileArgs：Linux Bubblewrap，--ro-bind / / 挂载只读根，
//     workspace-write 时额外 --tmpfs /tmp + --bind workspaceRoot；
//   - LandlockProfileArgs：Linux Landlock 内核安全模块，白名单授权，
//     readOnly=[/], readWrite=[/dev/null]（必需），workspace-write 时
//     额外添加 /tmp 和 workspaceRoot；
//   - SeatbeltProfileArgs：macOS sandbox-exec，SBPL（Sandbox Profile Language）脚本，
//     (version 1)(allow default)(deny file-write*)(allow file-write* (literal /dev/null))，
//     workspace-write 时额外 (allow file-write* (subpath root))；
//   - 三个函数都只返回 profile 参数（runner 之后、分隔符 -- 之前的部分），
//     由 LocalProvider 组装完整 argv。
package sandbox

import (
	"fmt"
	"strings"
)

// BwrapProfileArgs 为指定文件效果策略构建 bwrap profile 参数。
// 返回 runner 之后、分隔符 -- 之前的参数列表。
//
// 基础参数（所有模式）：
//   - --ro-bind / /：根目录以只读方式挂载
//   - --dev /dev：提供 /dev
//   - --unshare-pid：隔离 PID 命名空间
//   - --proc /proc：挂载 /proc
//   - --die-with-parent：父进程死亡时杀掉沙箱
//
// workspace-write 模式额外添加：
//   - --tmpfs /tmp：/tmp 为可写临时文件系统
//   - --bind workspaceRoot workspaceRoot：工作区可读写挂载
func BwrapProfileArgs(policy SandboxPolicy) []string {
	args := []string{
		"--ro-bind", "/", "/",
		"--dev", "/dev",
		"--unshare-pid",
		"--proc", "/proc",
		"--die-with-parent",
	}
	if policy.Mode == ConfinedWorkspaceWrite {
		args = append(args, "--tmpfs", "/tmp")
		args = append(args, "--bind", policy.WorkspaceRoot, policy.WorkspaceRoot)
	}
	return args
}

// LandlockProfileArgs 为指定文件效果策略构建 Landlock launcher 授权参数。
// 返回 launcher 授权参数（readOnly / readWrite 标志）。
//
// read-only 模式：
//   - readOnly: [/]
//   - readWrite: [/dev/null]（必需，否则很多程序无法正常运行）
//
// workspace-write 模式：
//   - readOnly: [/]
//   - readWrite: [/dev/null, /tmp, workspaceRoot]
func LandlockProfileArgs(policy SandboxPolicy) []string {
	readWrite := []string{"/dev/null"}
	if policy.Mode == ConfinedWorkspaceWrite {
		readWrite = append(readWrite, "/tmp", policy.WorkspaceRoot)
	}
	args := []string{}
	// readOnly
	args = append(args, "--read-only", "/")
	// readWrite
	for _, rw := range readWrite {
		args = append(args, "--read-write", rw)
	}
	return args
}

// SeatbeltProfileArgs 为指定文件效果策略构建 macOS sandbox-exec 参数。
// 返回 [-p, sbplScript]。
//
// SBPL 脚本基础规则：
//   - (version 1)
//   - (allow default)：默认允许
//   - (deny file-write*)：拒绝所有文件写入
//   - (allow file-write* (literal /dev/null))：允许写入 /dev/null
//
// workspace-write 模式额外添加：
//   - (allow file-write* (subpath root))：允许写入工作区根目录及其子路径
func SeatbeltProfileArgs(policy SandboxPolicy) []string {
	forms := []string{
		"(version 1)",
		"(allow default)",
		"(deny file-write*)",
		fmt.Sprintf("(allow file-write* (literal %s))", sbplString("/dev/null")),
	}
	if policy.Mode == ConfinedWorkspaceWrite {
		roots := WritableRoots(policy.SandboxExecutionPolicy)
		if len(roots) > 0 {
			var subpaths []string
			for _, root := range roots {
				subpaths = append(subpaths, fmt.Sprintf("(subpath %s)", sbplString(root)))
			}
			forms = append(forms, fmt.Sprintf("(allow file-write* %s)", strings.Join(subpaths, " ")))
		}
	}
	return []string{"-p", strings.Join(forms, " ")}
}

// sbplString 将路径转义为 SBPL 字符串字面量。
// 对齐官方 profiles.ts 中的 sbplString 函数：
// 反斜杠转义为 \\，双引号转义为 \"。
func sbplString(path string) string {
	escaped := strings.ReplaceAll(path, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return `"` + escaped + `"`
}
