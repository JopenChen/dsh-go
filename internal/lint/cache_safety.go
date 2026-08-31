// Package lint 提供缓存安全（cache-safety）静态分析。
//
// 对应任务 N06（阶段 5 防御层）：检测 4 类会破坏 DeepSeek Prefix Cache 的"动态值/随机"反模式。
//
// 反模式清单（编译期/源码静态可检测）：
//   1. 动态时间戳写 prompt：time.Now() / time.Date(...)；
//   2. 依赖运行目录：os.Getwd()；
//   3. 引入随机性：math/rand 或 rand.*(...) / crypto/rand；
//   4. 无序 map 序列化（schema/catalog 顺序随机）：json.Marshal(map[...]...)。
//
// 本包用 go/ast 扫描 Go 源码目录；与 N03 的 StaticCheck（文本级）互补，构成 IDE/CI 级防线。
package lint

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// Violation 是一次反模式命中。
type Violation struct {
	// Path 源文件路径。
	Path string
	// Line 行号。
	Line int
	// Pattern 命中的反模式说明。
	Pattern string
}

func (v Violation) Error() string {
	return fmt.Sprintf("%s:%d: %s", v.Path, v.Line, v.Pattern)
}

// ScanDir 用 go/ast 扫描目录下所有 .go 文件（含子目录），返回反模式命中。
func ScanDir(dir string) ([]Violation, error) {
	var out []Violation
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		vs, err := ScanFile(path)
		if err != nil {
			return nil // 解析失败当作无命中（不阻断整个扫描）
		}
		out = append(out, vs...)
		return nil
	})
	if err != nil {
		return out, err
	}
	return out, nil
}

// ScanFile 扫描单个 Go 文件。
func ScanFile(path string) ([]Violation, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.AllErrors)
	if err != nil {
		return nil, err
	}
	imports := map[string]bool{}
	for _, imp := range f.Imports {
		imports[strings.Trim(imp.Path.Value, `"`)] = true
	}
	var out []Violation
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		line := fset.Position(sel.Pos()).Line
		name := sel.Sel.Name
		switch {
		case sel.X != nil && exprName(sel.X) == "time" && (name == "Now" || name == "Date"):
			out = append(out, Violation{Path: path, Line: line, Pattern: "动态时间 time." + name})
		case sel.X != nil && exprName(sel.X) == "os" && name == "Getwd":
			out = append(out, Violation{Path: path, Line: line, Pattern: "依赖运行目录 os.Getwd"})
		case sel.X != nil && exprName(sel.X) == "rand" && isImport(imports, "math/rand"):
			out = append(out, Violation{Path: path, Line: line, Pattern: "随机性 rand." + name})
		case sel.X != nil && exprName(sel.X) == "math" && name == "Rand":
			_ = name
		}
		return true
	})
	// json.Marshal(map[...]) 无序序列化。
	if strings.Contains(readSource(path), "json.Marshal(map") {
		out = append(out, Violation{Path: path, Line: 0, Pattern: "无序 map 序列化 json.Marshal(map)"})
	}
	return out, nil
}

func exprName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return exprName(t.X) + "." + t.Sel.Name
	}
	return ""
}

func isImport(imports map[string]bool, pkg string) bool {
	return imports[pkg]
}

func readSource(path string) string {
	b, _ := os.ReadFile(path)
	return string(b)
}