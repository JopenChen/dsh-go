// pruner.go 复刻官方 compaction-tool-result-pruner 的纯文本修剪：超长工具结果
// 保留头部与尾部、中间替换为省略标记，按 rune（Unicode code point）切分，不拆
// 代理对；未超阈值时原样返回、changed=false。
package compaction

// PruneMarker 是中间被移除段的替换标记。
const PruneMarker = "\n... [内容已省略] ...\n"

// PruneText 修剪超长文本。runes 长度 <= threshold 时原样返回；否则保留前 head
// 个、后 tail 个 rune，中间放 PruneMarker。head/tail 非负，tail 超出时按可用量。
func PruneText(text string, threshold, head, tail int) (string, bool) {
	r := []rune(text)
	if len(r) <= threshold {
		return text, false
	}
	if head < 0 {
		head = 0
	}
	if tail < 0 {
		tail = 0
	}
	if head+tail > len(r) {
		// 首尾之和已覆盖全文，直接保留（避免重复/倒置）。
		return text, false
	}
	return string(r[:head]) + PruneMarker + string(r[len(r)-tail:]), true
}
