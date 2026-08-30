// Package tools 提供工具定义与执行的通用接缝。
//
// 本文件对应任务 M48：DefineTool JSON Schema 强校验子集。
//
// 对齐上游：packages/core/tools DefineTool Schema
//
// 设计动机：
//   - Agent 的工具调用参数、subagent / workflow 的结构化输出都需要一套严格的 JSON Schema
//     校验子集，保证 LLM 生成的参数在进入 execute 前就被验证；
//   - 与原版 1:1 对齐的关键语义：oneOf / additionalProperties=false / items / enum /
//     const / default / examples / number·string·array 约束；
//   - 不支持的 schema 关键字必须在 define 时报错（fail-closed），而不是静默忽略，
//     否则模型侧与执行侧会对参数语义产生不一致理解。
package tools

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"sort"
	"strings"
)

// SchemaType 是 JSON Schema 支持的类型名。
type SchemaType string

// 支持的 JSON Schema 类型枚举。
const (
	TypeString  SchemaType = "string"
	TypeNumber  SchemaType = "number"
	TypeInteger SchemaType = "integer"
	TypeBoolean SchemaType = "boolean"
	TypeArray   SchemaType = "array"
	TypeObject  SchemaType = "object"
	TypeNull    SchemaType = "null"
)

// JsonSchemaNode 是 DefineTool 支持的 JSON Schema 强校验子集。
// 字段与上游 packages/core/tools 的 DefineTool Schema 1:1 对应。
type JsonSchemaNode struct {
	// Type 声明节点类型。
	Type SchemaType `json:"type,omitempty"`

	// Description 提供给模型阅读的字段说明。
	Description string `json:"description,omitempty"`

	// Properties 仅 object 使用：属性名 → 子 schema。
	Properties map[string]*JsonSchemaNode `json:"properties,omitempty"`

	// Required 仅 object 使用：必填属性名列表。
	Required []string `json:"required,omitempty"`

	// AdditionalProperties 仅 object 使用：
	//   - nil：允许任意额外属性（默认）；
	//   - false：拒绝任何未声明的属性（严格模式，与原版语义一致）。
	AdditionalProperties *bool `json:"additionalProperties,omitempty"`

	// Items 仅 array 使用：元素子 schema。
	Items *JsonSchemaNode `json:"items,omitempty"`

	// Enum 限制取值必须属于该列表（与 const 互斥，若都设置则 Enum 优先）。
	Enum []any `json:"enum,omitempty"`

	// Const 限制取值必须严格等于该常量。
	Const any `json:"const,omitempty"`

	// Default 提供模型可参考的默认值（不参与校验）。
	Default any `json:"default,omitempty"`

	// Examples 提供模型可参考的示例（不参与校验）。
	Examples []any `json:"examples,omitempty"`

	// OneOf 允许值匹配其中任一子 schema。
	OneOf []*JsonSchemaNode `json:"oneOf,omitempty"`

	// --- number/integer 约束 ---
	Minimum          *float64 `json:"minimum,omitempty"`
	Maximum          *float64 `json:"maximum,omitempty"`
	ExclusiveMinimum *bool    `json:"exclusiveMinimum,omitempty"`
	ExclusiveMaximum *bool    `json:"exclusiveMaximum,omitempty"`

	// --- string 约束 ---
	MinLength *int   `json:"minLength,omitempty"`
	MaxLength *int   `json:"maxLength,omitempty"`
	Pattern   string `json:"pattern,omitempty"`

	// --- array 约束 ---
	MinItems *int `json:"minItems,omitempty"`
	MaxItems *int `json:"maxItems,omitempty"`
}

// supportedKeys 是 Compile 允许出现的 JSON Schema 关键字集合。
// 任何其它关键字（如 $ref / allOf / anyOf / not / format / nullable 等）一律拒绝。
var supportedKeys = map[string]struct{}{
	"type": {}, "description": {}, "properties": {}, "required": {},
	"additionalProperties": {}, "items": {}, "enum": {}, "const": {},
	"default": {}, "examples": {}, "oneOf": {},
	"minimum": {}, "maximum": {}, "exclusiveMinimum": {}, "exclusiveMaximum": {},
	"minLength": {}, "maxLength": {}, "pattern": {},
	"minItems": {}, "maxItems": {},
}

// Compile 从 JSON 字节解析 JsonSchemaNode，并拒绝不支持的 schema 关键字。
// 这是 define 工具时的唯一入口，保证 fail-closed：遇到未知关键字直接报错。
func Compile(raw []byte) (*JsonSchemaNode, error) {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()

	var node JsonSchemaNode
	if err := dec.Decode(&node); err != nil {
		return nil, fmt.Errorf("schema: compile failed: %w", err)
	}
	// DisallowUnknownFields 对嵌套 map 不起作用，需手动递归校验未知 key
	if err := validateSupportedKeys(raw); err != nil {
		return nil, err
	}
	// 语义自检：type 为 object 时必须带 properties；oneOf 至少 1 项等
	if err := node.assertStructural(); err != nil {
		return nil, err
	}
	return &node, nil
}

// validateSupportedKeys 递归扫描原始 JSON，找出所有出现在对象里的 key，
// 若不在 supportedKeys 集合中则报错。
func validateSupportedKeys(raw []byte) error {
	var root any
	if err := json.Unmarshal(raw, &root); err != nil {
		return fmt.Errorf("schema: invalid json: %w", err)
	}
	var walk func(v any) error
	walk = func(v any) error {
		switch t := v.(type) {
		case map[string]any:
			for k, sub := range t {
				if _, ok := supportedKeys[k]; !ok {
					return fmt.Errorf("schema: unsupported keyword %q (fail-closed, 不支持该关键字)", k)
				}
				if k == "properties" {
					// properties 的值是 map[属性名]schema：键名是属性名而非关键字，
					// 只递归校验每个属性子 schema。
					if props, ok := sub.(map[string]any); ok {
						for _, subSchema := range props {
							if err := walk(subSchema); err != nil {
								return err
							}
						}
					}
					continue
				}
				if err := walk(sub); err != nil {
					return err
				}
			}
		case []any:
			for _, item := range t {
				if err := walk(item); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(root)
}

// assertStructural 检查 schema 内部结构合法性。
func (n *JsonSchemaNode) assertStructural() error {
	if n.OneOf != nil && len(n.OneOf) == 0 {
		return fmt.Errorf("schema: oneOf must contain at least one subschema")
	}
	if n.Type == TypeObject && n.Properties == nil && n.AdditionalProperties == nil {
		return fmt.Errorf("schema: object type must declare properties or additionalProperties=false")
	}
	if n.Type == TypeArray && n.Items == nil {
		return fmt.Errorf("schema: array type must declare items")
	}
	for _, name := range n.Required {
		if _, ok := n.Properties[name]; !ok && n.Type == TypeObject {
			return fmt.Errorf("schema: required property %q not declared in properties", name)
		}
	}
	return nil
}

// Validate 校验一个 Go 值是否符合该 schema。返回 nil 表示通过。
func (n *JsonSchemaNode) Validate(v any) error {
	// oneOf：任一子 schema 匹配即通过
	if len(n.OneOf) > 0 {
		var matched int
		for _, sub := range n.OneOf {
			if err := sub.Validate(v); err == nil {
				matched++
			}
		}
		if matched == 0 {
			return fmt.Errorf("schema: value %v does not match any oneOf subschema", v)
		}
		return nil
	}

	// enum：值必须属于枚举列表
	if len(n.Enum) > 0 {
		for _, e := range n.Enum {
			if reflect.DeepEqual(e, v) {
				return n.validateTyped(v)
			}
		}
		return fmt.Errorf("schema: value %v not in enum %v", v, n.Enum)
	}
	// const：值必须严格相等
	if n.Const != nil {
		if !reflect.DeepEqual(n.Const, v) {
			return fmt.Errorf("schema: value %v != const %v", v, n.Const)
		}
		return n.validateTyped(v)
	}

	return n.validateTyped(v)
}

// validateTyped 执行类型与约束校验。
func (n *JsonSchemaNode) validateTyped(v any) error {
	switch n.Type {
	case "":
		// 未声明 type：仅做 enum/const/oneOf 约束校验（已在上层处理）
		return nil
	case TypeNull:
		if v != nil {
			return fmt.Errorf("schema: expected null, got %v", v)
		}
		return nil
	case TypeString:
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("schema: expected string, got %T", v)
		}
		if n.MinLength != nil && len([]rune(s)) < *n.MinLength {
			return fmt.Errorf("schema: string length %d < minLength %d", len([]rune(s)), *n.MinLength)
		}
		if n.MaxLength != nil && len([]rune(s)) > *n.MaxLength {
			return fmt.Errorf("schema: string length %d > maxLength %d", len([]rune(s)), *n.MaxLength)
		}
		if n.Pattern != "" {
			re, err := regexp.Compile(n.Pattern)
			if err != nil {
				return fmt.Errorf("schema: invalid pattern %q: %w", n.Pattern, err)
			}
			if !re.MatchString(s) {
				return fmt.Errorf("schema: string %q does not match pattern %q", s, n.Pattern)
			}
		}
		return nil
	case TypeNumber, TypeInteger:
		f, ok := toFloat(v)
		if !ok {
			return fmt.Errorf("schema: expected number, got %T", v)
		}
		if n.Type == TypeInteger {
			if math.Trunc(f) != f {
				return fmt.Errorf("schema: expected integer, got %v", v)
			}
		}
		if n.Minimum != nil {
			if n.ExclusiveMinimum != nil && *n.ExclusiveMinimum && f <= *n.Minimum {
				return fmt.Errorf("schema: value %v must be > %v", f, *n.Minimum)
			}
			if f < *n.Minimum {
				return fmt.Errorf("schema: value %v < minimum %v", f, *n.Minimum)
			}
		}
		if n.Maximum != nil {
			if n.ExclusiveMaximum != nil && *n.ExclusiveMaximum && f >= *n.Maximum {
				return fmt.Errorf("schema: value %v must be < %v", f, *n.Maximum)
			}
			if f > *n.Maximum {
				return fmt.Errorf("schema: value %v > maximum %v", f, *n.Maximum)
			}
		}
		return nil
	case TypeBoolean:
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("schema: expected boolean, got %T", v)
		}
		return nil
	case TypeArray:
		arr, ok := v.([]any)
		if !ok {
			// 兼容原生切片（测试直接传 []int 等）
			rv := reflect.ValueOf(v)
			if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
				return fmt.Errorf("schema: expected array, got %T", v)
			}
			arr = make([]any, rv.Len())
			for i := 0; i < rv.Len(); i++ {
				arr[i] = rv.Index(i).Interface()
			}
		}
		if n.MinItems != nil && len(arr) < *n.MinItems {
			return fmt.Errorf("schema: array length %d < minItems %d", len(arr), *n.MinItems)
		}
		if n.MaxItems != nil && len(arr) > *n.MaxItems {
			return fmt.Errorf("schema: array length %d > maxItems %d", len(arr), *n.MaxItems)
		}
		if n.Items != nil {
			for i, item := range arr {
				if err := n.Items.Validate(item); err != nil {
					return fmt.Errorf("schema: items[%d]: %w", i, err)
				}
			}
		}
		return nil
	case TypeObject:
		obj, ok := v.(map[string]any)
		if !ok {
			// 兼容结构体：反射转 map 后再校验
			obj = structToMap(v)
			if obj == nil {
				return fmt.Errorf("schema: expected object, got %T", v)
			}
		}
		// 必填检查
		for _, name := range n.Required {
			if _, exists := obj[name]; !exists {
				return fmt.Errorf("schema: missing required property %q", name)
			}
		}
		// 严格模式：拒绝未声明属性
		if n.AdditionalProperties != nil && !*n.AdditionalProperties {
			for key := range obj {
				if _, declared := n.Properties[key]; !declared {
					return fmt.Errorf("schema: unexpected property %q (additionalProperties=false)", key)
				}
			}
		}
		// 逐属性校验
		for key, sub := range n.Properties {
			if val, exists := obj[key]; exists {
				if err := sub.Validate(val); err != nil {
					return fmt.Errorf("schema: property %q: %w", key, err)
				}
			}
		}
		return nil
	default:
		return fmt.Errorf("schema: unsupported type %q", n.Type)
	}
}

// toFloat 将任意数值类型转为 float64。
func toFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case int32:
		return float64(t), true
	case json.Number:
		f, err := t.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

// structToMap 将结构体反射为 map[string]any，用于校验直接传入结构体的情况。
func structToMap(v any) map[string]any {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return nil
	}
	out := map[string]any{}
	rt := rv.Type()
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		name := field.Name
		if tag, ok := field.Tag.Lookup("json"); ok {
			if parts := strings.Split(tag, ","); len(parts) > 0 && parts[0] != "" {
				name = parts[0]
			}
		}
		out[name] = rv.Field(i).Interface()
	}
	return out
}

// Infer 从任意 Go 值推断 JsonSchemaNode（对应上游 DefineTool 的 INFER 能力）。
// 支持基础类型 / 切片 / map / 结构体。无法推断的类型返回错误。
func Infer(v any) (*JsonSchemaNode, error) {
	node, err := infer(reflect.ValueOf(v), 0)
	if err != nil {
		return nil, err
	}
	return node, nil
}

// infer 递归推断 schema，depth 用于防止循环引用。
func infer(rv reflect.Value, depth int) (*JsonSchemaNode, error) {
	if depth > 32 {
		return nil, fmt.Errorf("schema: inference depth exceeded (possible recursive type)")
	}
	if !rv.IsValid() {
		return &JsonSchemaNode{Type: TypeNull}, nil
	}
	if rv.Kind() == reflect.Ptr || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return &JsonSchemaNode{Type: TypeNull}, nil
		}
		return infer(rv.Elem(), depth+1)
	}

	switch rv.Kind() {
	case reflect.String:
		return &JsonSchemaNode{Type: TypeString}, nil
	case reflect.Bool:
		return &JsonSchemaNode{Type: TypeBoolean}, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return &JsonSchemaNode{Type: TypeInteger}, nil
	case reflect.Float32, reflect.Float64:
		return &JsonSchemaNode{Type: TypeNumber}, nil
	case reflect.Slice, reflect.Array:
		// 优先从元素类型推断（即使切片为空也能得到正确的元素 schema），
		// 其次回退到首个元素值。
		items := &JsonSchemaNode{Type: TypeNull}
		if rv.Kind() == reflect.Array && rv.Len() > 0 {
			it, err := infer(rv.Index(0), depth+1)
			if err != nil {
				return nil, err
			}
			items = it
		} else if rv.Kind() == reflect.Slice {
			elemType := rv.Type().Elem()
			it, err := infer(reflect.New(elemType).Elem(), depth+1)
			if err != nil {
				return nil, err
			}
			items = it
		}
		return &JsonSchemaNode{Type: TypeArray, Items: items}, nil
	case reflect.Map:
		props := map[string]*JsonSchemaNode{}
		iter := rv.MapRange()
		for iter.Next() {
			key := iter.Key()
			if key.Kind() != reflect.String {
				return nil, fmt.Errorf("schema: cannot infer schema from non-string map key %v", key)
			}
			sub, err := infer(iter.Value(), depth+1)
			if err != nil {
				return nil, err
			}
			props[key.String()] = sub
		}
		return &JsonSchemaNode{Type: TypeObject, Properties: props}, nil
	case reflect.Struct:
		props := map[string]*JsonSchemaNode{}
		var required []string
		rt := rv.Type()
		for i := 0; i < rt.NumField(); i++ {
			field := rt.Field(i)
			if !field.IsExported() {
				continue
			}
			name := field.Name
			optional := false
			if tag, ok := field.Tag.Lookup("json"); ok {
				parts := strings.Split(tag, ",")
				if len(parts) > 0 && parts[0] != "" {
					name = parts[0]
				}
				for _, p := range parts[1:] {
					if p == "omitempty" {
						optional = true
					}
				}
			}
			sub, err := infer(rv.Field(i), depth+1)
			if err != nil {
				return nil, err
			}
			props[name] = sub
			if !optional {
				required = append(required, name)
			}
		}
		return &JsonSchemaNode{Type: TypeObject, Properties: props, Required: required}, nil
	default:
		return nil, fmt.Errorf("schema: cannot infer schema from kind %v", rv.Kind())
	}
}

// Marshal 输出标准 JSON Schema 文本（properties/required/enum 均按字典序，保证确定性）。
func (n *JsonSchemaNode) Marshal() ([]byte, error) {
	// 深度复制并排序 map 键，保证输出确定性
	ordered := n.orderMap()
	return json.MarshalIndent(ordered, "", "  ")
}

// orderMap 将内部结构转为 map 并按 properties/required/enum 排序后序列化。
func (n *JsonSchemaNode) orderMap() map[string]any {
	out := map[string]any{}
	if n.Type != "" {
		out["type"] = n.Type
	}
	if n.Description != "" {
		out["description"] = n.Description
	}
	if len(n.Properties) > 0 {
		props := map[string]any{}
		keys := make([]string, 0, len(n.Properties))
		for k := range n.Properties {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			props[k] = n.Properties[k].orderMap()
		}
		out["properties"] = props
	}
	if len(n.Required) > 0 {
		req := append([]string(nil), n.Required...)
		sort.Strings(req)
		out["required"] = req
	}
	if n.AdditionalProperties != nil {
		out["additionalProperties"] = *n.AdditionalProperties
	}
	if n.Items != nil {
		out["items"] = n.Items.orderMap()
	}
	if len(n.Enum) > 0 {
		out["enum"] = n.Enum
	}
	if n.Const != nil {
		out["const"] = n.Const
	}
	if n.Default != nil {
		out["default"] = n.Default
	}
	if len(n.Examples) > 0 {
		out["examples"] = n.Examples
	}
	if len(n.OneOf) > 0 {
		oneOf := make([]any, 0, len(n.OneOf))
		for _, sub := range n.OneOf {
			oneOf = append(oneOf, sub.orderMap())
		}
		out["oneOf"] = oneOf
	}
	if n.Minimum != nil {
		out["minimum"] = *n.Minimum
	}
	if n.Maximum != nil {
		out["maximum"] = *n.Maximum
	}
	if n.ExclusiveMinimum != nil {
		out["exclusiveMinimum"] = *n.ExclusiveMinimum
	}
	if n.ExclusiveMaximum != nil {
		out["exclusiveMaximum"] = *n.ExclusiveMaximum
	}
	if n.MinLength != nil {
		out["minLength"] = *n.MinLength
	}
	if n.MaxLength != nil {
		out["maxLength"] = *n.MaxLength
	}
	if n.Pattern != "" {
		out["pattern"] = n.Pattern
	}
	if n.MinItems != nil {
		out["minItems"] = *n.MinItems
	}
	if n.MaxItems != nil {
		out["maxItems"] = *n.MaxItems
	}
	return out
}
