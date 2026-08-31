// 一次性工具脚本：T01 测试骨架生成器。
//
// 读取 docs/TEST_CASES.md，解析全部 `TC-<任务ID>-<序号>` 表格行，
// 对状态字段包含"待实现"（未覆盖）的用例，在 tests/tc_skeletons_test.go
// 中生成可编译的占位测试骨架（默认 t.Skip，含中文注释记录用例意图）。
//
// 用途：T01「可执行测试骨架生成（328 条用例 → _test.go）」，TC 编号一一对应，
// 仅生成骨架，不回填真实业务逻辑。运行：`go run ./cmd/gen_tc_skeletons`
package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// tcRow 是从表格中解析出的单条用例。
type tcRow struct {
	ID     string
	Kind   string
	Name   string
	Status string
	File   string
}

// tcID 校验用例 ID 形如 TC-M01-01（任务ID 字母+数字，后跟 "-序号"）。
var tcID = regexp.MustCompile(`^TC-[A-Z]\d+-\d+$`)

func main() {
	root, _ := os.Getwd()
	docPath := filepath.Join(root, "docs", "TEST_CASES.md")
	outPath := filepath.Join(root, "tests", "tc_skeletons_test.go")

	f, err := os.Open(docPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "打开文档失败: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()

	var rows []tcRow
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(strings.TrimSpace(line), "|") {
			continue
		}
		// 拆字段：`| TC-M01-01 | 正向 | 名称 | 前置 | 步骤 | 预期 | 状态 | 关联文件 |`
		parts := strings.Split(line, "|")
		if len(parts) < 4 {
			continue
		}
		trim := func(s string) string { return strings.TrimSpace(s) }
		id := trim(parts[1])
		if !tcID.MatchString(id) {
			continue
		}
		var r tcRow
		r.ID = id
		if len(parts) >= 3 {
			r.Kind = trim(parts[2])
		}
		if len(parts) >= 4 {
			r.Name = trim(parts[3])
		}
		if len(parts) >= 8 {
			r.Status = trim(parts[7])
		}
		if len(parts) >= 9 {
			r.File = trim(parts[8])
		}
		rows = append(rows, r)
	}
	if err := sc.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "读取文档失败: %v\n", err)
		os.Exit(1)
	}

	// 过滤：仅保留"待实现"（未覆盖）用例，作为骨架。
	var pending []tcRow
	for _, r := range rows {
		if strings.Contains(r.Status, "待实现") {
			pending = append(pending, r)
		}
	}

	var sb strings.Builder
	sb.WriteString("// 本文件由 T01 骨架生成器自动生成（tests/tc_skeletons_test.go）。\n")
	sb.WriteString("// 来源：docs/TEST_CASES.md → 仅对「待实现」（未覆盖）用例生成可编译占位骨架。\n")
	sb.WriteString("// 每个用例 TC 编号一一对应；默认 t.Skip 占位，后续按 TEST_CASES.md 断言补全业务逻辑。\n")
	sb.WriteString("// 重新生成：`go run ./cmd/gen_tc_skeletons`。\n")
	sb.WriteString("package tests\n\n")
	sb.WriteString("import (\n\t\"testing\"\n)\n\n")

	for _, r := range pending {
		fn := "TestTC" + strings.ReplaceAll(r.ID[3:], "-", "_")
		sb.WriteString("// ")
		sb.WriteString(r.ID)
		sb.WriteString(" · 类型:")
		sb.WriteString(orDash(r.Kind))
		sb.WriteString(" · 名称:")
		sb.WriteString(orDash(r.Name))
		if r.File != "" && r.File != "—" && r.File != "-" {
			sb.WriteString(" · 关联文件:")
			sb.WriteString(r.File)
		}
		sb.WriteString("\n")
		sb.WriteString("// T01 骨架（待实现）：按 TEST_CASES.md 断言补全。\n")
		sb.WriteString("func ")
		sb.WriteString(fn)
		sb.WriteString("(t *testing.T) {\n")
		sb.WriteString("\tt.Skip(\"T01 骨架占位: ")
		sb.WriteString(r.ID)
		sb.WriteString("\")\n\t_ = t\n}\n\n")
	}

	if err := os.WriteFile(outPath, []byte(sb.String()), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "写文件失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("生成完成: %s（共 %d 条待实现骨架, 来源 %d 条 TC）\n", outPath, len(pending), len(rows))
}

func orDash(s string) string {
	if s == "" || s == "—" || s == "-" {
		return "-"
	}
	return s
}
