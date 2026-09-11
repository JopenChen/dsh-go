// normalize.go 复刻官方 session-title/normalize：把不可信标题文本清洗为单行
// （剥离转义/控制字符/方向控制、空白归一），并按 UTF-8 字节预算截断而不拆字符。
package sessiontitle

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// CleanTitle 移除转义与控制类字符，并把连续空白压成单个空格，返回单行文本。
func CleanTitle(input string) string {
	var b strings.Builder
	b.Grow(len(input))
	prevSpace := false
	leading := true
	runes := []rune(input)
	for i := 0; i < len(runes); {
		r := runes[i]
		// 先整体跳过 ANSI 转义序列（ESC 起头）。
		if r == 0x1b {
			i = skipAnsi(runes, i)
			continue
		}
		switch {
		case isTitleControl(r):
			i++ // 丢弃控制类字符。
			continue
		case unicode.IsSpace(r):
			if leading || prevSpace {
				i++
				continue
			}
			b.WriteRune(' ')
			prevSpace = true
			i++
			continue
		}
		b.WriteRune(r)
		prevSpace = false
		leading = false
		i++
	}
	return strings.TrimRight(b.String(), " ")
}

// skipAnsi 返回从 idx（ESC）起整个转义序列之后的下标。
func skipAnsi(runes []rune, idx int) int {
	if idx+1 >= len(runes) {
		return len(runes)
	}
	switch runes[idx+1] {
	case '[': // CSI：读到 0x40–0x7e 终止字节。
		j := idx + 2
		for j < len(runes) && !(runes[j] >= 0x40 && runes[j] <= 0x7e) {
			j++
		}
		return j + 1
	case ']': // OSC：以 BEL 或 ST 结束。
		j := idx + 2
		for j < len(runes) {
			if runes[j] == 0x07 {
				return j + 1
			}
			if runes[j] == 0x1b && j+1 < len(runes) && runes[j+1] == '\\' {
				return j + 2
			}
			j++
		}
		return j
	default: // 两字节转义族。
		return idx + 2
	}
}

// isTitleControl 判断是否为标题中应剔除的控制/方向/不可见字符。
func isTitleControl(r rune) bool {
	if r == '\t' || r == '\n' || r == '\r' {
		return false // 交由空白归一处理
	}
	if r <= 0x1f || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
		return true // C0/C1 控制
	}
	switch {
	case r == 0x200b, r == 0x200e, r == 0x200f: // 零宽/方向
		return true
	case r >= 0x202a && r <= 0x202e: // 方向格式
		return true
	case r >= 0x2060 && r <= 0x2064: // 不可见连接/格式
		return true
	case r >= 0x2066 && r <= 0x206f: // 方向隔离
		return true
	case r == 0xfeff: // BOM/零宽不换行空格
		return true
	}
	return false
}

// TruncateUTF8 按 UTF-8 字节预算返回最长的完整 rune 前缀，不拆分字符。
func TruncateUTF8(input string, maxBytes int) string {
	if maxBytes <= 0 || len(input) <= maxBytes {
		if maxBytes <= 0 {
			return ""
		}
		return input
	}
	used := 0
	for _, r := range input {
		size := utf8.RuneLen(r)
		if used+size > maxBytes {
			break
		}
		used += size
	}
	return input[:used]
}

// Normalize 清洗标题并按字节预算截断。
func Normalize(input string, maxBytes int) string {
	return strings.TrimRight(TruncateUTF8(CleanTitle(input), maxBytes), " ")
}
