// Package tests 的工具展示中立词汇（M47）验收测试。
//
// 覆盖：
//   - 9 种内置卡片（tool-call: generic/terminal/diff + tool-result: generic/terminal/
//     diff/search(matches)/search(paths)/read/web(search)/web(fetch)）字段一一对应
//   - CardOf 判别
//   - 自定义工具可提供自定义 card 类型
package tests

import (
	"testing"

	"github.com/JopenChen/dsh-go/pkg/tools"
)

// customCard 是自定义结果的卡片类型（自定义 card 判别；不实现 tools.ToolResultView）。
type customCard struct {
	Card string `json:"card"`
	Note string `json:"note"`
}

// TestToolPresentationAllCallCards 验证 3 种调用卡片。
func TestToolPresentationAllCallCards(t *testing.T) {
	gen := tools.NewGenericCallView("Search Files")
	gen.Kind = tools.CallKindSearch
	cases := []struct {
		name string
		view tools.ToolCallView
		want string
	}{
		{"generic", gen, "generic"},
		{"terminal", tools.NewTerminalCallView("npm run build"), "terminal"},
		{"diff", tools.NewDiffCallView("Write foo.txt", []tools.FileDiff{{Path: "foo.txt", NewText: "hi"}}), "diff"},
	}
	for _, c := range cases {
		if got := tools.CardOf(c.view); got != c.want {
			t.Errorf("%s: CardOf = %s, want %s", c.name, got, c.want)
		}
	}
}

// TestToolPresentationAllResultCards 验证 6 种结果卡片（9 张含 search 两种 shape + web 两种 kind）。
func TestToolPresentationAllResultCards(t *testing.T) {
	cases := []struct {
		name string
		view tools.ToolResultView
		want string
	}{
		{"generic", tools.NewGenericResultView(), "generic"},
		{"terminal", tools.NewTerminalResultView("output"), "terminal"},
		{"diff", &tools.DiffResultView{Card: "diff", Diffs: []tools.FileDiff{{Path: "a", NewText: "b"}}}, "diff"},
		{"search-matches", tools.NewSearchMatchesView(nil, false, 1), "search"},
		{"search-paths", tools.NewSearchPathsView([]string{"a.go"}, false, 1), "search"},
		{"read", tools.NewReadResultView("/x/a.go", 1, nil, 2), "read"},
		{"web-search", tools.NewWebSearchResultView(nil, "a", false), "web"},
		{"web-fetch", tools.NewWebFetchResultView("https://example.com", 200, false), "web"},
	}
	for _, c := range cases {
		if got := tools.CardOf(c.view); got != c.want {
			t.Errorf("%s: CardOf = %s, want %s", c.name, got, c.want)
		}
	}
}

// TestToolPresentationSearchShapes 验证 search 两种 shape 字段。
func TestToolPresentationSearchShapes(t *testing.T) {
	m := tools.NewSearchMatchesView([]tools.SearchFileMatches{
		{Path: "a.go", Matches: []tools.SearchLineMatch{{LineNumber: 1, Line: "func main()"}}},
	}, true, 100)
	if m.Card != "search" || m.Shape != "matches" || !m.Truncated || m.Total != 100 {
		t.Fatalf("search-matches 字段错误: %+v", m)
	}
	p := tools.NewSearchPathsView([]string{"a.go", "b.go"}, false, 2)
	if p.Shape != "paths" || len(p.Paths) != 2 || p.Truncated {
		t.Fatalf("search-paths 字段错误: %+v", p)
	}
}

// TestToolPresentationWebKinds 验证 web 两种 kind。
func TestToolPresentationWebKinds(t *testing.T) {
	s := tools.NewWebSearchResultView([]tools.WebSource{{URL: "https://x", Title: "X"}}, "answer", false)
	if s.Kind != "search" || len(s.Sources) != 1 || s.Answer != "answer" {
		t.Fatalf("web-search 字段错误: %+v", s)
	}
	f := tools.NewWebFetchResultView("https://y", 200, false)
	if f.Kind != "fetch" || f.URL != "https://y" || f.StatusCode != 200 {
		t.Fatalf("web-fetch 字段错误: %+v", f)
	}
}

// TestToolPresentationCustomCard 验证自定义卡片类型（不封闭的判别）。
func TestToolPresentationCustomCard(t *testing.T) {
	var view any = customCard{Card: "mycard", Note: "custom"}
	if got := tools.CardOf(view); got != "other" {
		t.Fatalf("自定义卡片 CardOf 应回落 other, 实际 %s", got)
	}
}