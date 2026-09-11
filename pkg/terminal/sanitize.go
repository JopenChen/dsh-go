// sanitize.go 复刻官方 terminal/terminal-bash 的清洗逻辑：把 PTY 原始输出中的
// ANSI CSI/OSC 转义序列剥离，并归一化回车换行，得到可安全展示的纯文本。
package terminal

import "strings"

// StripANSI 移除字符串中的 ANSI 转义序列（CSI、OSC、两字节转义族）。
func StripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] != 0x1b { // ESC
			b.WriteByte(s[i])
			i++
			continue
		}
		if i+1 >= len(s) { // 末尾孤立 ESC，丢弃。
			break
		}
		switch s[i+1] {
		case '[': // CSI：ESC[ 后读到最终字节（0x40–0x7e）。
			j := i + 2
			for j < len(s) && !(s[j] >= 0x40 && s[j] <= 0x7e) {
				j++
			}
			i = j + 1
		case ']': // OSC：以 BEL 或 ST(ESC\\) 结束。
			j := i + 2
			for j < len(s) {
				if s[j] == 0x07 { // BEL
					j++
					break
				}
				if s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\' {
					j += 2
					break
				}
				j++
			}
			i = j
		default: // 两字节转义族（如 ESC 7 / ESC 8）。
			i += 2
		}
	}
	return b.String()
}

// NormalizeText 归一化终端文本：CRLF 与孤立 CR 转为 LF，并移除 BEL。
func NormalizeText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.ReplaceAll(s, "\x07", "")
	return s
}

// Sanitize 一步完成转义剥离与文本归一化。
func Sanitize(s string) string {
	return NormalizeText(StripANSI(s))
}
