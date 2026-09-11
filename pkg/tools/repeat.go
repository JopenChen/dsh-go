// 本文件复刻官方 guard/repeat-tool-reminder 的纯逻辑部分：建议性连续重复调用检测。
// 它不否决、不改写调用，只在模型连续以相同参数重复同一工具达到阈值时给出提醒，
// 帮助打破"原地打转"。检测用的链键始终比较完整的规范化参数，避免属性顺序不同就
// 被误判为不同调用。
package tools

import (
	"encoding/json"
	"sort"
	"strings"
)

// DefaultRepeatThresholds 是默认触发阈值（温和 → 详细逐级升级）。
var DefaultRepeatThresholds = []int{3, 5, 8}

// canonicalValue 递归把值规范化：map 的 key 排序，slice 保持顺序。
func canonicalValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := map[string]any{}
		for _, k := range keys {
			out[k] = canonicalValue(t[k])
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i := range t {
			out[i] = canonicalValue(t[i])
		}
		return out
	default:
		return v
	}
}

// CanonicalizeArgs 返回参数的规范化字符串（深度 key 排序后序列化）。
func CanonicalizeArgs(input map[string]any) string {
	b, err := json.Marshal(canonicalValue(input))
	if err != nil {
		return ""
	}
	return string(b)
}

// RepeatState 是单个代理的连续重复链状态。
type RepeatState struct {
	lastKey string
	count   int
}

// Observe 记录一次调用，返回当前连续相同调用次数（不同调用重置为 1）。
func (r *RepeatState) Observe(toolName string, args map[string]any) int {
	key := toolName + "\x00" + CanonicalizeArgs(args)
	if key == r.lastKey {
		r.count++
	} else {
		r.lastKey = key
		r.count = 1
	}
	return r.count
}

// Reset 清空链（用户插话后调用：跨插话的重复不算循环）。
func (r *RepeatState) Reset() {
	r.lastKey = ""
	r.count = 0
}

// HitThreshold 判断次数是否命中某阈值。
func HitThreshold(count int, thresholds []int) bool {
	for _, th := range thresholds {
		if count == th {
			return true
		}
	}
	return false
}

// GentleReminder 是首个阈值的温和提醒。
const GentleReminder = "你在以完全相同的参数重复同一工具调用。请先仔细分析上一次结果，" +
	"若任务未完成，换一种方式或换一组参数，而不是重复调用。"

// DetailedReminder 生成后续阈值的详细提醒。
func DetailedReminder(toolName string, count int, canonical string, previewCap int) string {
	preview := canonical
	if previewCap > 0 && len(preview) > previewCap {
		preview = string([]rune(preview)[:previewCap]) + "…"
	}
	var b strings.Builder
	b.WriteString("检测到重复工具调用：\n")
	b.WriteString("- 工具：" + toolName + "\n")
	b.WriteString("- 连续次数：")
	b.WriteString(itoaRepeat(count))
	b.WriteString("\n- 参数：" + preview + "\n")
	b.WriteString("这些重复没有取得进展，请勿再用相同参数调用该工具。")
	return b.String()
}

func itoaRepeat(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
