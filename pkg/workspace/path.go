// path.go 复刻官方 util/workspace-path 的路径辅助：相对路径拼到工作区根、home
// 目录缩写为 ~、以及取路径最后一段用于显示。
package workspace

import (
	"path/filepath"
	"strings"
)

// ResolvePath 把相对 path 拼到工作区 root；绝对路径原样返回。root 为空时原样返回。
// 同时识别 POSIX（/ 开头）、Windows（盘符或 UNC）绝对路径，保证跨平台。
func ResolvePath(root, path string) string {
	if isAbsolutePath(path) {
		return path
	}
	if root == "" {
		return path
	}
	return filepath.Join(root, path)
}

// isAbsolutePath 跨平台判断绝对路径。
func isAbsolutePath(p string) bool {
	if filepath.IsAbs(p) {
		return true
	}
	// Windows 上 filepath.IsAbs 不认 POSIX 风格的 / 前缀，这里补上。
	if strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`) {
		return true
	}
	return false
}

// AbbreviateHome 把 home 及其子路径中的 home 前缀缩写为 ~；非 home 下原样返回。
func AbbreviateHome(path, home string) string {
	if home == "" {
		return path
	}
	root := strings.TrimRight(home, `/\`)
	if root == "" {
		return path
	}
	if path == root {
		return "~"
	}
	// 同时识别 POSIX 与 Windows 分隔符，保证跨平台。
	if strings.HasPrefix(path, root+"/") || strings.HasPrefix(path, root+`\`) {
		return "~" + strings.TrimPrefix(path, root)
	}
	return path
}

// TitleOf 取路径最后一段用于显示。
func TitleOf(path string) string {
	return filepath.Base(strings.TrimRight(path, `/\`))
}
