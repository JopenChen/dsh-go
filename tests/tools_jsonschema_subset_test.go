// 本文件对应任务 M48：DefineTool JSON Schema 强校验子集。
package tests

import (
	"bytes"
	"testing"

	"github.com/JopenChen/dsh-go/pkg/tools"
)

// TestSchemaCompileAndValidate 验证基础 schema 编译 + 校验通过/失败。
func TestSchemaCompileAndValidate(t *testing.T) {
	schema, err := tools.Compile([]byte(`{
		"type": "object",
		"properties": {
			"name": {"type": "string", "minLength": 1},
			"count": {"type": "integer", "minimum": 0},
			"tags": {"type": "array", "items": {"type": "string"}, "minItems": 1}
		},
		"required": ["name"]
	}`))
	if err != nil {
		t.Fatalf("Compile 失败: %v", err)
	}

	// 通过场景
	ok := map[string]any{"name": "hello", "count": 3, "tags": []any{"a", "b"}}
	if err := schema.Validate(ok); err != nil {
		t.Fatalf("合法值应通过: %v", err)
	}

	// 缺必填字段
	if err := schema.Validate(map[string]any{"count": 1}); err == nil {
		t.Fatal("缺必填字段应校验失败")
	}

	// 类型错误
	if err := schema.Validate(map[string]any{"name": 42}); err == nil {
		t.Fatal("类型错误应校验失败")
	}

	// minLength 违规
	if err := schema.Validate(map[string]any{"name": ""}); err == nil {
		t.Fatal("minLength 违规应校验失败")
	}

	// minItems 违规
	if err := schema.Validate(map[string]any{"name": "x", "tags": []any{}}); err == nil {
		t.Fatal("minItems 违规应校验失败")
	}
}

// TestSchemaAdditionalPropertiesFalse 验证 additionalProperties=false 严格模式。
func TestSchemaAdditionalPropertiesFalse(t *testing.T) {
	schema, err := tools.Compile([]byte(`{
		"type": "object",
		"properties": {"known": {"type": "string"}},
		"additionalProperties": false,
		"required": ["known"]
	}`))
	if err != nil {
		t.Fatalf("Compile 失败: %v", err)
	}

	if err := schema.Validate(map[string]any{"known": "v"}); err != nil {
		t.Fatalf("仅声明属性应通过: %v", err)
	}
	if err := schema.Validate(map[string]any{"known": "v", "unknown": 1}); err == nil {
		t.Fatal("未声明属性在 additionalProperties=false 下应失败")
	}
}

// TestSchemaEnumAndConst 验证 enum 与 const 语义。
func TestSchemaEnumAndConst(t *testing.T) {
	enumSchema, err := tools.Compile([]byte(`{"type": "string", "enum": ["low", "medium", "high"]}`))
	if err != nil {
		t.Fatalf("Compile enum 失败: %v", err)
	}
	if err := enumSchema.Validate("medium"); err != nil {
		t.Fatalf("enum 命中应通过: %v", err)
	}
	if err := enumSchema.Validate("ultra"); err == nil {
		t.Fatal("enum 未命中应失败")
	}

	constSchema, err := tools.Compile([]byte(`{"type": "string", "const": "fixed"}`))
	if err != nil {
		t.Fatalf("Compile const 失败: %v", err)
	}
	if err := constSchema.Validate("fixed"); err != nil {
		t.Fatalf("const 命中应通过: %v", err)
	}
	if err := constSchema.Validate("other"); err == nil {
		t.Fatal("const 未命中应失败")
	}
}

// TestSchemaOneOf 验证 oneOf 语义（任一子 schema 匹配即通过）。
func TestSchemaOneOf(t *testing.T) {
	schema, err := tools.Compile([]byte(`{
		"oneOf": [
			{"type": "string", "minLength": 5},
			{"type": "integer", "minimum": 100}
		]
	}`))
	if err != nil {
		t.Fatalf("Compile oneOf 失败: %v", err)
	}

	if err := schema.Validate("hello"); err != nil {
		t.Fatalf("字符串分支应通过: %v", err)
	}
	if err := schema.Validate(200); err != nil {
		t.Fatalf("整数分支应通过: %v", err)
	}
	if err := schema.Validate(50); err == nil {
		t.Fatal("两个分支都不匹配应失败")
	}
	if err := schema.Validate(true); err == nil {
		t.Fatal("类型都不匹配应失败")
	}
}

// TestSchemaRejectUnsupportedKeys 验证不支持的 schema 关键字 fail-closed 报错。
func TestSchemaRejectUnsupportedKeys(t *testing.T) {
	// $ref 是常见但不支持的关键字
	if _, err := tools.Compile([]byte(`{"$ref": "#/definitions/x"}`)); err == nil {
		t.Fatal("$ref 应被拒绝")
	}
	// 嵌套位置的不支持关键字也应被拒绝
	if _, err := tools.Compile([]byte(`{"type":"object","properties":{"a":{"allOf":[{"type":"string"}]}}}`)); err == nil {
		t.Fatal("嵌套 allOf 应被拒绝")
	}
	// 非法 JSON
	if _, err := tools.Compile([]byte(`{invalid`)); err == nil {
		t.Fatal("非法 JSON 应报错")
	}
	// 结构自检：array 必须带 items
	if _, err := tools.Compile([]byte(`{"type":"array"}`)); err == nil {
		t.Fatal("array 缺 items 应报错")
	}
}

// TestSchemaDefaultExamples 验证 default / examples 解析保留（不参与校验）。
func TestSchemaDefaultExamples(t *testing.T) {
	schema, err := tools.Compile([]byte(`{
		"type": "string",
		"default": "dft",
		"examples": ["e1", "e2"]
	}`))
	if err != nil {
		t.Fatalf("Compile 失败: %v", err)
	}
	if schema.Default != "dft" {
		t.Fatalf("default 未保留: %v", schema.Default)
	}
	if len(schema.Examples) != 2 {
		t.Fatalf("examples 未保留: %v", schema.Examples)
	}
}

// TestSchemaInfer 验证 INFER（从 Go 值推断 schema）。
func TestSchemaInfer(t *testing.T) {
	type inner struct {
		ID    int    `json:"id"`
		Label string `json:"label,omitempty"`
	}
	type payload struct {
		Name  string  `json:"name"`
		Score float64 `json:"score"`
		Items []inner `json:"items"`
	}

	schema, err := tools.Infer(payload{})
	if err != nil {
		t.Fatalf("Infer 失败: %v", err)
	}
	if schema.Type != tools.TypeObject {
		t.Fatalf("顶层应为 object: %v", schema.Type)
	}
	// required 只包含无 omitempty 的顶层字段（name/score/items，label 在 inner 中带 omitempty）
	if len(schema.Required) != 3 {
		t.Fatalf("顶层 required 应含 name/score/items: %v", schema.Required)
	}
	// 内层 inner 的 label 带 omitempty，不应出现在内层 required 中
	innerSchema := schema.Properties["items"].Items
	if len(innerSchema.Required) != 1 || innerSchema.Required[0] != "id" {
		t.Fatalf("内层 required 应只含 id（label 有 omitempty）: %v", innerSchema.Required)
	}
	if schema.Properties["items"].Type != tools.TypeArray {
		t.Fatalf("items 应为 array: %v", schema.Properties["items"].Type)
	}
	if schema.Properties["items"].Items.Properties["id"].Type != tools.TypeInteger {
		t.Fatal("内层 id 应为 integer")
	}

	// 推断结果可直接用于校验
	val := map[string]any{
		"name":  "x",
		"score": 1.5,
		"items": []any{map[string]any{"id": 1}},
	}
	if err := schema.Validate(val); err != nil {
		t.Fatalf("推断 schema 校验应通过: %v", err)
	}
}

// TestSchemaMarshalDeterministic 验证 Marshal 输出确定性 + 字典序。
func TestSchemaMarshalDeterministic(t *testing.T) {
	schema, err := tools.Compile([]byte(`{
		"type": "object",
		"properties": {
			"zeta": {"type": "string"},
			"alpha": {"type": "string"}
		},
		"required": ["alpha", "zeta"]
	}`))
	if err != nil {
		t.Fatalf("Compile 失败: %v", err)
	}

	b1, err := schema.Marshal()
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	b2, err := schema.Marshal()
	if err != nil {
		t.Fatalf("Marshal 二次失败: %v", err)
	}
	if string(b1) != string(b2) {
		t.Fatal("Marshal 输出不确定")
	}

	// properties 与 required 应字典序：直接检查序列化字节中 alpha 出现在 zeta 之前。
	//（encoding/json 对 map 输出本身就是字典序，此处做字节级断言，避免依赖 Go map 迭代顺序。）
	if bytes.Index(b1, []byte("alpha")) == -1 || bytes.Index(b1, []byte("zeta")) == -1 {
		t.Fatalf("Marshal 输出缺少 alpha/zeta: %s", b1)
	}
	if bytes.Index(b1, []byte("alpha")) > bytes.Index(b1, []byte("zeta")) {
		t.Fatalf("properties 应字典序(alpha 在 zeta 前): %s", b1)
	}
	// 再验证字典序稳定（多轮均一致）。
	b3, _ := schema.Marshal()
	if !bytes.Equal(b1, b3) {
		t.Fatalf("Marshal 输出不确定:\n%s\nvs\n%s", b1, b3)
	}
}
