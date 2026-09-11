// Package fileref 复刻官方 context/file-reference 的 @文件引用语法：提取光标处
// 正在输入的 @token，并把选中的路径格式化为 @path / @"path" 文本。
package fileref

import "regexp"

// Token 是光标处活动的 @token。
type Token struct {
	Prefix string // 被整体替换的 token
	Query  string // @ 之后的路径查询
	Quoted bool   // 是否引号路径
}

var (
	reQuoted = regexp.MustCompile(`(?:^|\s)(@"([^"]*))$`)
	rePlain  = regexp.MustCompile(`(?:^|\s)(@([^\s@]*))$`)
)

// ActiveAtToken 提取光标列之前的 @path 或 @"path" token；不在 @token 上时 ok=false。
func ActiveAtToken(line string, cursorCol int) (Token, bool) {
	if cursorCol < 0 || cursorCol > len(line) {
		return Token{}, false
	}
	before := line[:cursorCol]
	if m := reQuoted.FindStringSubmatch(before); m != nil {
		return Token{Prefix: m[1], Query: m[2], Quoted: true}, true
	}
	if m := rePlain.FindStringSubmatch(before); m != nil {
		return Token{Prefix: m[1], Query: m[2], Quoted: false}, true
	}
	return Token{}, false
}

var unsafePath = regexp.MustCompile(`[\x00-\x1f\x7f-\x9f"]`)
var hasSpace = regexp.MustCompile(`\s`)

// FormatMention 把选中路径格式化为插入文本。目录保留尾斜杠；含空白用引号；
// 遇到语法无法安全表示的路径返回空串。
func FormatMention(path string, isDir, preserveQuote bool) string {
	if isDir && len(path) > 0 && path[len(path)-1] != '/' {
		path += "/"
	}
	if unsafePath.MatchString(path) {
		return ""
	}
	quoted := preserveQuote || hasSpace.MatchString(path)
	if !quoted {
		return "@" + path
	}
	if isDir {
		return `@"` + path
	}
	return `@"` + path + `"`
}
