package sysprompt

import "testing"

func TestChangeOnlyInjectorCleared(t *testing.T) {
	reg := NewContextRegistry()
	inj := NewChangeOnlyInjector(reg, "")

	// 首次为空：不注入。
	if _, ok := inj.MightInject(); ok {
		t.Fatal("empty first time must not inject")
	}
	// 出现内容：注入。
	reg.Register("a", 10, "hello")
	text, ok := inj.MightInject()
	if !ok || text != "hello" {
		t.Fatalf("expected hello, got %q ok=%v", text, ok)
	}
	// 内容消失：注入清空标记而非空串。
	reg.Unregister("a")
	text, ok = inj.MightInject()
	if !ok || text != ClearedContextText {
		t.Fatalf("expected cleared marker, got %q ok=%v", text, ok)
	}
}
