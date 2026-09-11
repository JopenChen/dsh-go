// 本文件实现可写根目录的共享计算：canonicalPath + writableRoots。
//
// 对齐官方：packages/sandbox/sandbox/src/roots.ts
//
// 设计要点：
//   - canonicalPath 使用 filepath.EvalSymlinks 解析符号链接（macOS 上 /tmp 实际是 /private/tmp）；
//     解析失败时返回原路径（保守策略：路径不存在时匹配不到任何东西，不发明回退路径）；
//   - writableRoots 是沙箱后端和进程内 FS 防护栏共享的单一真相源：
//     workspace-write 模式返回 [workspaceRoot, /tmp, os.TempDir()] 的规范化去重列表；
//     read-only 和 danger-full-access 返回空列表；
//   - 这确保"FS 防护栏说可以写的目录"和"沙箱实际允许写的目录"永远一致，
//     消除"FS 说可以但沙箱拒绝"（或反过来）的不对称 bug。
package sandbox

import (
	"os"
	"path/filepath"
)

// CanonicalPath 解析路径到执行层实际比较的形式：
// 规范化（符号链接解析），因为 Seatbelt 过滤器和 FS 防护栏的包含检查
// 都匹配解析后的路径——macOS 上 /tmp 就是 /private/tmp，按拼写的授权会匹配不到任何东西。
//
// 解析失败时（路径或其前缀不存在/不可读）返回原路径：
// 不存在的根在它存在之前匹配不到任何东西——这是保守的结果；
// 发明回退路径会授予调用方从未命名过的路径。
func CanonicalPath(path string) string {
	if path == "" {
		return path
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		// EvalSymlinks 失败：路径（或前缀）缺失或不可读。
		return filepath.Clean(path)
	}
	abs, err := filepath.Abs(resolved)
	if err != nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(abs)
}

// WritableRoots 返回一次受约束执行可以写入的根目录——模式的含义作为规范化、去重的白名单。
//   - read-only：不允许任何写入，返回空列表；
//   - workspace-write：允许策略的 workspace 根、主机 /tmp、以及 per-user 平台临时目录
//     （os.TempDir()——mkstemp 系列工具的真实临时区域；省略它会拒绝模式承诺的东西）；
//   - danger-full-access：绕过沙箱，不需要可写根列表，返回空列表。
//
// 返回的根经过 CanonicalPath 规范化并去重。
func WritableRoots(policy SandboxExecutionPolicy) []string {
	if policy.Mode != ModeWorkspaceWrite {
		return nil
	}
	roots := []string{
		policy.WorkspaceRoot,
		"/tmp",
		os.TempDir(),
	}
	seen := map[string]bool{}
	var result []string
	for _, r := range roots {
		if r == "" {
			continue
		}
		canonical := CanonicalPath(r)
		if seen[canonical] {
			continue
		}
		seen[canonical] = true
		result = append(result, canonical)
	}
	return result
}
