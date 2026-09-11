// Package instructions 复刻官方 context/agent-instructions 的指令文件发现纯逻辑：
// 从工作目录向上定位项目根、构建根到工作目录的祖先链、并按目录对内容去重。
package instructions

import (
	"os"
	"path/filepath"
	"strings"
)

// FindProjectRoot 从 cwd 逐级向上，返回第一个含任一 root marker（如 ".git"）的
// 目录；一直到文件系统根都没有则返回 cwd。
func FindProjectRoot(cwd string, markers []string) string {
	current, err := filepath.Abs(cwd)
	if err != nil {
		return cwd
	}
	for {
		for _, m := range markers {
			if _, err := os.Stat(filepath.Join(current, m)); err == nil {
				return current
			}
		}
		parent := filepath.Dir(current)
		if parent == current {
			abs, _ := filepath.Abs(cwd)
			return abs
		}
		current = parent
	}
}

// AncestorChain 返回从 root 到 cwd（含两端）的目录链，顺序由宽到窄。cwd 不在
// root 之下时退化为只含 root。
func AncestorChain(root, cwd string) []string {
	r, _ := filepath.Abs(root)
	c, _ := filepath.Abs(cwd)
	var chain []string
	current := c
	for current != r {
		chain = append(chain, current)
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	chain = append(chain, r)
	// 反转
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain
}

// LoadedFile 是读到内容的指令候选。
type LoadedFile struct {
	DisplayPath string
	Content     string
}

// DedupByDirectory 对同一目录内 trim 后内容相同的候选去重，保留发现顺序最早者；
// 不同目录即使内容相同也保留。
func DedupByDirectory(files []LoadedFile) []LoadedFile {
	seen := map[string]map[string]struct{}{}
	var kept []LoadedFile
	for _, f := range files {
		dir := filepath.Dir(f.DisplayPath)
		digest := strings.TrimSpace(f.Content)
		set, ok := seen[dir]
		if !ok {
			set = map[string]struct{}{}
			seen[dir] = set
		}
		if _, dup := set[digest]; dup {
			continue
		}
		set[digest] = struct{}{}
		kept = append(kept, f)
	}
	return kept
}
