// Package tests 的 N06（阶段 5 防御层）验收测试。
//
// 覆盖：
//   - internal/lint cache_safety：AST 扫描捕获 4 类反模式（time.Now/os.Getwd/rand/无序 map）
//   - 干净包 0 violations
//   - compileToJSONSchema 输出确定（两次 Marshal 逐字节相同 + required 字典序）
package tests

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JopenChen/dsh-go/internal/lint"
	"github.com/JopenChen/dsh-go/pkg/tools"
)

const badSource = `package p
import "time"
import "os"
var x = time.Now()
var y = os.Getwd()
`

const badRand = `package p
import "math/rand"
var r = rand.Intn(10)
`

const cleanSource = `package p
func f() string { return "static" }
`

// TestN06LintCatchesDynamicPatterns 验证扫描捕获 time.Now / os.Getwd。
func TestN06LintCatchesDynamicPatterns(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(badSource), 0o644); err != nil {
		t.Fatal(err)
	}
	vs, err := lint.ScanDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) < 2 {
		t.Fatalf("应捕获 time.Now 与 os.Getwd, 实际 %d 命中: %v", len(vs), vs)
	}
}

// TestN06LintCatchesRand 验证扫描捕获 math/rand。
func TestN06LintCatchesRand(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "r.go"), []byte(badRand), 0o644); err != nil {
		t.Fatal(err)
	}
	vs, _ := lint.ScanDir(dir)
	if len(vs) == 0 {
		t.Fatal("应捕获 math/rand 随机性反模式")
	}
}

// TestN06LintCleanPackage 验证干净包 0 violations。
func TestN06LintCleanPackage(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "clean.go"), []byte(cleanSource), 0o644); err != nil {
		t.Fatal(err)
	}
	vs, _ := lint.ScanDir(dir)
	if len(vs) != 0 {
		t.Fatalf("干净包应无违规, 实际 %v", vs)
	}
}

// TestN06SchemaDeterministicOrder 验证 compileToJSONSchema 两次 Marshal 逐字节相同（字典序）。
func TestN06SchemaDeterministicOrder(t *testing.T) {
	// 构造一个含属性/required 的 schema（无序构造 Properties/Required）。
	schema := &tools.JsonSchemaNode{
		Type: tools.TypeObject,
		Properties: map[string]*tools.JsonSchemaNode{
			"b": {Type: tools.TypeString},
			"a": {Type: tools.TypeInteger},
			"c": {Type: tools.TypeBoolean},
		},
		Required: []string{"c", "a"},
	}
	b1, err := schema.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 1000; i++ {
		b2, _ := schema.Marshal()
		if string(b1) != string(b2) {
			t.Fatalf("第 %d 次 Marshal 不一致（schema 顺序随机）", i)
		}
	}
	// properties/required 应字典序（a 在 c 前）。
	s := string(b1)
	if posOfStr(s, "\"a\"") > posOfStr(s, "\"c\"") {
		t.Fatalf("schema 应按字典序: %s", s)
	}
}

func posOfStr(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}