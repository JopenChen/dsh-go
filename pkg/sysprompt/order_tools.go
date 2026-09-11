// order_tools.go 复刻官方 system-prompt 的 orderTools：按配置顺序排列工具，
// 未在配置中列出的工具在 ToolOrderRest 标记处按名称插入；配置必须恰好包含一次
// rest 标记、不得重复、不得引用不存在的工具名。
package sysprompt

import (
	"errors"
	"sort"
)

// ToolOrderRest 是"其余工具"的保留标记。
const ToolOrderRest = "<unlisted-tools>"

// OrderTools 按 configured 指定的顺序排列 names。
func OrderTools(names, configured []string) ([]string, error) {
	if configured == nil {
		out := append([]string(nil), names...)
		sort.Strings(out)
		return out, nil
	}
	seen := map[string]bool{}
	for _, c := range configured {
		if c == ToolOrderRest {
			if seen[ToolOrderRest] {
				return nil, errors.New("rest marker appears more than once")
			}
		} else if seen[c] {
			return nil, errors.New("duplicate tool order entry: " + c)
		}
		seen[c] = true
	}
	if !seen[ToolOrderRest] {
		return nil, errors.New("tool order must contain the rest marker")
	}
	known := map[string]bool{}
	for _, n := range names {
		known[n] = true
	}
	for c := range seen {
		if c != ToolOrderRest && !known[c] {
			return nil, errors.New("tool order lists unknown tool: " + c)
		}
	}
	listed := map[string]bool{}
	var rest []string
	for _, n := range names {
		if seen[n] {
			listed[n] = true
		} else {
			rest = append(rest, n)
		}
	}
	sort.Strings(rest)
	out := make([]string, 0, len(names))
	for _, c := range configured {
		if c == ToolOrderRest {
			out = append(out, rest...)
		} else if listed[c] {
			out = append(out, c)
		}
	}
	return out, nil
}
