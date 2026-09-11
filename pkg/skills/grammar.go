// grammar.go 复刻官方 skill 的名称语法与模型面内容块渲染：kebab-case 名称校验，
// 以及统一的 <skill_content> 块（资源提示 + 指令正文），转义内嵌文本防止越出框架。
package skills

import (
	"regexp"
	"strings"
)

var skillName = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// IsName 判断是否合法 kebab-case skill 名称。
func IsName(name string) bool { return skillName.MatchString(name) }

func escapeText(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}

func escapeAttr(s string) string {
	r := strings.NewReplacer("&", "&amp;", `"`, "&quot;", "<", "&lt;")
	return r.Replace(s)
}

// RenderContent 渲染统一的 skill_content 块。provider 说明资源由谁管理，body 为
// 指令正文。
func RenderContent(name, provider, body string) string {
	var b strings.Builder
	b.WriteString(`<skill_content name="`)
	b.WriteString(escapeAttr(name))
	b.WriteString("\">\n<skill_resources>\n")
	b.WriteString("Resources for this skill are managed by provider \"")
	b.WriteString(escapeText(provider))
	b.WriteString("\".\nLoad referenced resources only as needed.\n</skill_resources>\n\n")
	b.WriteString("<skill_instructions>\n")
	b.WriteString(body)
	b.WriteString("\n</skill_instructions>\n</skill_content>")
	return b.String()
}
