// interpolate.go 复刻官方 system-prompt 的严格变量插值：把文本中完整的
// {{name}} 组替换为 variables 中的值，变量名须为 [a-z][a-z0-9_]*，未知或无值
// 变量报错，替换进去的值不再二次扫描；没有后续 }} 的孤立 {{ 作为字面文本。
package sysprompt

import (
	"errors"
	"regexp"
	"strings"
)

var variableName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// Interpolate 对 text 做严格变量插值。
func Interpolate(text string, variables map[string]string) (string, error) {
	var b strings.Builder
	last := 0
	for {
		open := strings.Index(text[last:], "{{")
		if open < 0 {
			b.WriteString(text[last:])
			break
		}
		open += last
		// 从 open 起找第一个 "}}"。
		relClose := strings.Index(text[open:], "}}")
		if relClose < 0 {
			// 没有闭合：剩余作为字面文本。
			b.WriteString(text[last:])
			break
		}
		closeEnd := open + relClose + 2
		name := text[open+2 : open+relClose]
		// open 与 }} 之间若还含 {{，属于畸形嵌套。
		if strings.Contains(name, "{{") {
			return "", errors.New("malformed variable reference near " + text[open:min(closeEnd, len(text))])
		}
		if !variableName.MatchString(name) {
			return "", errors.New("malformed variable name: " + name)
		}
		value, ok := variables[name]
		if !ok {
			return "", errors.New("unknown variable: " + name)
		}
		b.WriteString(text[last:open])
		b.WriteString(value) // 替换值不再扫描
		last = closeEnd
	}
	return b.String(), nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
