// Package homepath 复刻官方 util/home-paths 的用户数据根解析：按"显式配置 >
// DSH_HOME 环境变量 > ~/.dsh"的优先级确定唯一根，并支持 ~ 展开与符号化显示。
package homepath

import (
	"os"
	"path/filepath"
	"strings"
)

// HomeDirName 是默认 Harness home 目录名。
const HomeDirName = ".dsh"

// EnvName 是覆盖默认 home 的环境变量名。
const EnvName = "DSH_HOME"

// ExpandHome 展开开头的 ~、~/、~\\ 为操作系统 home；不支持的前缀原样返回。
func ExpandHome(path string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if path == "~" {
		return home, nil
	}
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		return filepath.Join(home, path[2:]), nil
	}
	return path, nil
}

// Resolve 按优先级解析 Harness home。configured 非空优先；否则用 env 中非空白
// 的 DSH_HOME；都没有则退回 ~/.dsh。
func Resolve(configured string, env map[string]string) (string, error) {
	var selected string
	switch {
	case strings.TrimSpace(configured) != "":
		selected = configured
	case strings.TrimSpace(env[EnvName]) != "":
		selected = env[EnvName]
	default:
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		selected = filepath.Join(home, HomeDirName)
	}
	expanded, err := ExpandHome(selected)
	if err != nil {
		return "", err
	}
	return filepath.Abs(expanded)
}

// Display 把解析后的 home 符号化：默认 home 显示为 ~/.dsh，自定义的显示为 $DSH_HOME。
func Display(resolvedHome string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "$" + EnvName
	}
	def, _ := filepath.Abs(filepath.Join(home, HomeDirName))
	if filepath.Clean(resolvedHome) == def {
		return "~/" + HomeDirName
	}
	return "$" + EnvName
}
