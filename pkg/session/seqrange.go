// seqrange.go 复刻官方 session 的序号区间无损编码：把严格递增的来源序号序列中
// 连续 3 个及以上的段压缩成 [from,to] 闭区间，其余作为单点；解码时无损展开。
// 用于 JSONL 存储里 sourceEventSeqs 数组的紧凑表示。
package session

import "errors"

// SeqRange 是一个闭区间；From == To 时表示单个序号。
type SeqRange struct {
	From int
	To   int
}

func strictlyIncreasing(values []int) bool {
	for i := 1; i < len(values); i++ {
		if values[i] <= values[i-1] {
			return false
		}
	}
	return true
}

// EncodeSeqRanges 把严格递增序号序列编码为区间列表；非严格递增时退化为单点序列。
func EncodeSeqRanges(values []int) []SeqRange {
	if !strictlyIncreasing(values) {
		out := make([]SeqRange, len(values))
		for i, v := range values {
			out[i] = SeqRange{v, v}
		}
		return out
	}
	var out []SeqRange
	for start := 0; start < len(values); {
		end := start
		for end+1 < len(values) && values[end+1] == values[end]+1 {
			end++
		}
		if end-start >= 2 {
			out = append(out, SeqRange{values[start], values[end]})
		} else {
			for i := start; i <= end; i++ {
				out = append(out, SeqRange{values[i], values[i]})
			}
		}
		start = end + 1
	}
	return out
}

// DecodeSeqRanges 把区间列表无损展开为序号序列，maxEntries 为最大条目数（<=0 不限）。
func DecodeSeqRanges(ranges []SeqRange, maxEntries int) ([]int, error) {
	var out []int
	hadRange := false
	for _, r := range ranges {
		if r.From < 0 || r.To < r.From {
			return nil, errors.New("invalid seq range")
		}
		if r.To > r.From {
			hadRange = true
		}
		for v := r.From; v <= r.To; v++ {
			if maxEntries > 0 && len(out) >= maxEntries {
				return nil, errors.New("seq ranges exceed max entries")
			}
			out = append(out, v)
		}
	}
	if hadRange && !strictlyIncreasing(out) {
		return nil, errors.New("decoded seq ranges must be strictly increasing")
	}
	return out, nil
}
