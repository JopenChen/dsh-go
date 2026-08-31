// 本文件对应任务 N03（D2 纪律）：System Prompt 模板纯函数 + 静态检测。
//
// 对齐上游：packages/core/system-prompt/src/sections/*.ts
//
// 设计要点：
//   - 每个 Section 是**纯函数**：同一输入渲染 1000 次输出逐字节相同（不含 time.Now /
//     os.Getwd / rand / 动态 env）；
//   - StaticCheck 对 Section 文本做静态扫描，识别"会破坏 prefix cache"的动态值模式
//     （D2 反模式），0 violation 才算通过；
//   - SystemHash 对组装后的 system prompt 计算稳定哈希，用于跨轮比对（缓存友好验证），
//     并证明"同一配置下 system prompt 逐字节稳定"。
package sysprompt

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

// ============================================================================
// 反模式静态检测
// ============================================================================

// bannedPattern 是一条反模式（正则 + 说明）。
type bannedPattern struct {
	re   *regexp.Regexp
	note string
}

// bannedPatterns 是 D2 认可的"破坏缓存"动态值模式集（静态可检测的子集）。
var bannedPatterns = []bannedPattern{
	{re: regexp.MustCompile(`time\.Now\(\)`), note: "time.Now() 引入动态时间"},
	{re: regexp.MustCompile(`time\.Date\(`), note: "time.Date() 固定时刻"},
	{re: regexp.MustCompile(`os\.Getwd\(\)`), note: "os.Getwd() 依赖运行目录"},
	{re: regexp.MustCompile(`math/rand`), note: "引入随机数"},
	{re: regexp.MustCompile(`\brand\.\w+\(`), note: "rand.*() 随机调用"},
	{re: regexp.MustCompile(`os\.Getenv\("DSH_`), note: "动态读取 DSH_* env"},
	{re: regexp.MustCompile(`fmt\.Sprintf\(`), note: "fmt.Sprintf 动态拼串"},
}

// StaticCheck 扫描一段 section 文本，返回命中的反模式说明（空表示无违规）。
func StaticCheck(text string) []string {
	var violations []string
	for _, bp := range bannedPatterns {
		if bp.re.MatchString(text) {
			violations = append(violations, bp.note)
		}
	}
	return violations
}

// CheckSections 对组装器全部已注册 section 做静态检查，返回 (violations, forbidden).
// forbidden=true 表示存在任一违规（将破坏缓存）。
func CheckSections(a *Assembler) (violations []string, forbidden bool) {
	for _, s := range a.Sections() {
		for _, v := range StaticCheck(s.Text) {
			violations = append(violations, s.Name+": "+v)
		}
	}
	return violations, len(violations) > 0
}

// ============================================================================
// 稳定系统提示哈希
// ============================================================================

// SystemHash 计算组装后 system prompt 的稳定 sha256（proof of 纯函数/缓存稳定）。
func SystemHash(sys string) string {
	sum := sha256.Sum256([]byte(sys))
	return hex.EncodeToString(sum[:])
}

// AssembleHash 返回组装器当前 prompt 的稳定哈希。
func (a *Assembler) AssembleHash() string {
	return SystemHash(a.Assemble())
}

// IsPure 判断一段文本是否满足纯函数（无 反模式 + 不依赖运行态）。
func IsPure(sectionText string) bool {
	return len(StaticCheck(sectionText)) == 0
}

// StripBanned 供测试构造"干净"样本：把常见动态调用替换为静态占位（仅演示）。
func StripBanned(s string) string {
	return strings.NewReplacer(
		"time.Now()", "NOW_STATIC",
		"os.Getwd()", "CWD_STATIC",
		"math/rand", "STATIC_RAND",
	).Replace(s)
}