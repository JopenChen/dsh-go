// 本文件对应任务 M03：Scope 分层注册表原语。
package tests

import (
	"strings"
	"testing"

	"github.com/JopenChen/dsh-go/pkg/scope"
)

// TestScopeNearestWins 验证 host + scope 叠加后读取优先级（nearest-scope-wins）。
func TestScopeNearestWins(t *testing.T) {
	// 场景：宿主级定义了工具 "bash"，会话级覆盖为限制版
	host := scope.NewLayer[string](scope.Key("host"))
	host.Register("bash", "host/bash:full", 0)

	session := scope.NewLayer[string](scope.Key("session:s1"))
	session.Register("bash", "session/bash:restricted", 0)

	layers := scope.NewScopedLayers[string]().
		Push(host). // host 最先
		Push(session)

	// 最近作用域（session）应覆盖 host
	got, ok := layers.Get("bash")
	if !ok {
		t.Fatal("bash 应可解析")
	}
	if got != "session/bash:restricted" {
		t.Fatalf("nearest-scope-wins 失效: got %q", got)
	}

	// 只解析 host 中独有的 key
	host.Register("ls", "host/ls", 0)
	got, ok = layers.Get("ls")
	if !ok || got != "host/ls" {
		t.Fatalf("host 独有 key 解析失败: got %q ok=%v", got, ok)
	}

	// 不存在的 key
	if _, ok := layers.Get("nope"); ok {
		t.Fatal("不存在的 key 不应解析成功")
	}
}

// TestScopeRankTiebreak 验证同层内 rank 大者胜、rank 相同后注册覆盖先注册。
func TestScopeRankTiebreak(t *testing.T) {
	layer := scope.NewLayer[int](scope.Key("session:s1"))

	// 相同 rank：后注册覆盖先注册
	layer.Register("toolA", 1, 0)
	layer.Register("toolA", 2, 0) // 后注册

	// 通过 ScopedLayers 解析
	layers := scope.NewScopedLayers[int]().Push(layer)
	if v, ok := layers.Get("toolA"); !ok || v != 2 {
		t.Fatalf("rank 相同应后注册覆盖: got %d ok=%v", v, ok)
	}

	// 更高 rank 覆盖后注册
	layer.Register("toolB", 10, 1)
	layer.Register("toolB", 20, 0) // 后注册但 rank 低
	if v, ok := layers.Get("toolB"); !ok || v != 10 {
		t.Fatalf("rank 大者胜: got %d ok=%v", v, ok)
	}
}

// TestScopeAnonymousNotConflict 验证匿名 entry 与 named entry 互不冲突。
func TestScopeAnonymousNotConflict(t *testing.T) {
	layer := scope.NewLayer[string](scope.Key("host"))
	// 具名条目
	layer.Register("bash", "named-bash", 0)
	// 匿名条目（name 为空）
	layer.Register("", "anonymous-default", 0)

	layers := scope.NewScopedLayers[string]().Push(layer)

	// 匿名条目不参与具名查找：Get("bash") 仍命中具名条目，匿名条目不影响
	if v, ok := layers.Get("bash"); !ok || v != "named-bash" {
		t.Fatalf("具名查找应命中具名条目: got %q ok=%v", v, ok)
	}
	// 用空串查找不应命中（匿名不参与 lookup）
	if _, ok := layers.Get(""); ok {
		t.Fatal("空串查找不应命中")
	}

	// Merge 后匿名条目应保留（与具名条目共存）
	merged := layers.Merge()
	hasAnon, hasNamed := false, false
	for _, e := range merged {
		if e.Name == "" {
			hasAnon = true
		}
		if e.Name == "bash" {
			hasNamed = true
		}
	}
	if !hasAnon || !hasNamed {
		t.Fatalf("匿名与具名条目应同时保留在 Merge 结果中: %+v", merged)
	}
}

// TestScopeMergeDeterministic 验证 Merge 输出确定性（多次调用逐项一致）。
func TestScopeMergeDeterministic(t *testing.T) {
	host := scope.NewLayer[string](scope.Key("host"))
	host.Register("zebra", "z", 0)
	host.Register("alpha", "a", 0)
	session := scope.NewLayer[string](scope.Key("session"))
	session.Register("mike", "m", 0)

	layers := scope.NewScopedLayers[string]().Push(host).Push(session)

	first := layers.Merge()
	second := layers.Merge()

	if len(first) != len(second) {
		t.Fatalf("两次 Merge 长度不一致")
	}
	for i := range first {
		if first[i].Name != second[i].Name || first[i].Value != second[i].Value {
			t.Fatalf("Merge 输出不确定: %+v vs %+v", first, second)
		}
	}

	// 具名条目应字典序排列
	var names []string
	for _, e := range first {
		if e.Name != "" {
			names = append(names, e.Name)
		}
	}
	joined := strings.Join(names, ",")
	if joined != "alpha,mike,zebra" {
		t.Fatalf("具名条目应字典序: %q", joined)
	}
}
