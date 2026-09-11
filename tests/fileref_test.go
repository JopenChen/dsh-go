// Package tests 的 fileref @引用语法验收测试。
package tests

import (
	"testing"

	"github.com/JopenChen/dsh-go/pkg/fileref"
)

func TestActiveAtTokenPlain(t *testing.T) {
	line := "打开 @src/app"
	tok, ok := fileref.ActiveAtToken(line, len(line))
	if !ok || tok.Query != "src/app" || tok.Quoted {
		t.Fatalf("plain token wrong: %+v ok=%v", tok, ok)
	}
}

func TestActiveAtTokenQuoted(t *testing.T) {
	line := `打开 @"my dir/a`
	tok, ok := fileref.ActiveAtToken(line, len(line))
	if !ok || tok.Query != "my dir/a" || !tok.Quoted {
		t.Fatalf("quoted token wrong: %+v ok=%v", tok, ok)
	}
}

func TestActiveAtTokenEmailNotTrigger(t *testing.T) {
	line := "a@b.com"
	if _, ok := fileref.ActiveAtToken(line, len(line)); ok {
		t.Fatal("@ inside a token must not trigger")
	}
}

func TestFormatMention(t *testing.T) {
	if got := fileref.FormatMention("src/a.go", false, false); got != "@src/a.go" {
		t.Fatalf("plain mention = %q", got)
	}
	if got := fileref.FormatMention("my dir/a.go", false, false); got != `@"my dir/a.go"` {
		t.Fatalf("space mention = %q", got)
	}
	if got := fileref.FormatMention("dir", true, false); got != "@dir/" {
		t.Fatalf("dir mention = %q want @dir/", got)
	}
	if got := fileref.FormatMention("my dir", true, false); got != `@"my dir/` {
		t.Fatalf("quoted dir mention = %q", got)
	}
}
