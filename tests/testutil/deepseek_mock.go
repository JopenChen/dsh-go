// Package testutil 提供 N07 缓存命中率 E2E 的 Mock DeepSeek 工具。
package testutil

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
)

// MockChatRequest 是进入 mock 的请求（我们只关心 prompt 文本）。
type MockChatRequest struct {
	Messages []struct {
		Role    string `json:"role"`
		Content any    `json:"content"`
	} `json:"messages"`
}

// PrefixHash 是 prompt 前缀指纹，用于缓存命中判定。
type PrefixHash struct {
	// PromptText 拼接后的 prompt 文本。
	PromptText string
	// Tokens 估 token 数（chars/4）。
	Tokens int
}

// PrefixCacheSimulator 模拟 DeepSeek prefix cache：
//   - 记录每轮 prompt 文本；
//   - 命中 = 当前 prompt 与上一轮 prompt 的最长公共前缀占比（字符比）；
//   - 稳定循环命中率高；切 preset 后首轮低、随后攀升（与官方 KV cache 行为一致）。
type PrefixCacheSimulator struct {
	mu        sync.Mutex
	lastPrompt string
}

// NewPrefixCacheSimulator 创建模拟器。
func NewPrefixCacheSimulator() *PrefixCacheSimulator {
	return &PrefixCacheSimulator{}
}

// LongestCommonPrefix 返回两个字符串的最长公共前缀长度。
func LongestCommonPrefix(a, b string) int {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	return n
}

// Simulate 对一次请求计算命中/未命中所占 token。
// 返回 (hitTokens, missTokens)。命中按与上次 prompt 的公共前缀占比；失配则全 miss。
func (s *PrefixCacheSimulator) Simulate(prompt string) (hit, miss int) {
	tok := len(prompt) / 4
	if tok < 1 {
		tok = 1
	}
	s.mu.Lock()
	last := s.lastPrompt
	s.lastPrompt = prompt
	s.mu.Unlock()
	common := LongestCommonPrefix(prompt, last)
	hitFrac := float64(common) / float64(len(prompt))
	if len(prompt) == 0 {
		hitFrac = 0
	}
	hit = int(float64(tok) * hitFrac)
	miss = tok - hit
	return hit, miss
}

// NewDeepSeekMock 创建一个 DeepSeek 模拟服务器，返回每次请求解析出的 usage 缓存字段。
// hitRatio 由 PrefixCacheSimulator 每请求推导。
func NewDeepSeekMock() (*httptest.Server, *PrefixCacheSimulator) {
	sim := NewPrefixCacheSimulator()
	var mu sync.Mutex
	var reports []float64 // 命中率历史
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req MockChatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		// 拼 prompt 文本。
		promptText := ""
		for _, m := range req.Messages {
			switch c := m.Content.(type) {
			case string:
				promptText += c + "\n"
			case []any:
				for _, blk := range c {
					if s, ok := blk.(string); ok {
						promptText += s + "\n"
					}
				}
			}
		}
		hit, miss := sim.Simulate(promptText)
		ratio := float64(hit) / float64(hit+miss)
		if hit+miss == 0 {
			ratio = 0
		}
		mu.Lock()
		reports = append(reports, ratio)
		mu.Unlock()
		// 返回含 usage 的 SSE。
		body := `data: {"choices":[],"usage":{"prompt_tokens":` + itoa(hit+miss) +
			`,"completion_tokens":5,"prompt_cache_hit_tokens":` + itoa(hit) +
			`,"prompt_cache_miss_tokens":` + itoa(miss) + `}}` + "\n\ndata: [DONE]\n\n"
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(body))
	}))
	return srv, sim
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}