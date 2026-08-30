// Package tests 存放 dsh-go 全工程跨包集成测试。
// 本文件对应任务 M01：Branded ID 类型封装。
package tests

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/JopenChen/dsh-go/pkg/brand"
)

// TestBrandedRoundTrip 验证具体品牌类型的构造与 String/IsZero/Equal 全链路。
func TestBrandedRoundTrip(t *testing.T) {
	// 构造品牌化 SessionID
	sid := brand.NewSessionID("sess_123456")
	if sid.IsZero() {
		t.Fatal("NewSessionID 构造后不应为零值")
	}
	if sid.String() != "sess_123456" {
		t.Fatalf("String() = %q, want %q", sid.String(), "sess_123456")
	}

	// 零值语义（注：不能写 if !brand.SessionID{}.IsZero()，Go 解析器会把复合字面量
	// 误判为块语法，属于解析器边角限制，故先声明变量再调用方法）
	var zero brand.SessionID
	if !zero.IsZero() {
		t.Fatal("零值 SessionID 应当 IsZero()==true")
	}

	// Equal 比较
	other := brand.NewSessionID("sess_123456")
	if !sid.Equal(other) {
		t.Fatal("相同字符串构造的 SessionID 应相等")
	}
	diff := brand.NewSessionID("sess_999")
	if sid.Equal(diff) {
		t.Fatal("不同字符串构造的 SessionID 不应相等")
	}
}

// TestBrandedParse 验证 Parse 对空串的拒绝行为。
func TestBrandedParse(t *testing.T) {
	if _, err := brand.ParseToolCallID(""); err == nil {
		t.Fatal("ParseToolCallID 空串应返回错误")
	}
	parsed, err := brand.ParseToolCallID("call_abc")
	if err != nil {
		t.Fatalf("ParseToolCallID 合法串失败: %v", err)
	}
	if parsed.String() != "call_abc" {
		t.Fatalf("Parse 结果 = %q, want %q", parsed.String(), "call_abc")
	}
}

// TestBrandedJSON 验证 MarshalJSON / UnmarshalJSON 往返一致。
func TestBrandedJSON(t *testing.T) {
	original := brand.NewJobID("job_42")

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	if string(data) != `"job_42"` {
		t.Fatalf("Marshal 输出 = %s, want \"job_42\"", string(data))
	}

	var restored brand.JobID
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if !restored.Equal(original) {
		t.Fatalf("JSON 往返后不等: %q vs %q", restored.String(), original.String())
	}

	// 非法 JSON 应报错而不是静默
	var bad brand.SkillID
	if err := json.Unmarshal([]byte(`{"a":1}`), &bad); err == nil {
		t.Fatal("非字符串 JSON 应反序列化失败")
	}
}

// TestBrandedTypeIsolation 验证品牌类型彼此隔离。
// 运行时证明：通过 fmt.Sprintf("%T") 输出底层类型名，不同品牌类型名必然不同
// （SessionID → brand.Branded[brand.sessionIDTag]，ToolCallID → brand.Branded[brand.toolCallIDTag]）。
// 编译期证明：任何把 SessionID 传给期望 ToolCallID 参数的位置都会直接编译失败。
func TestBrandedTypeIsolation(t *testing.T) {
	sid := brand.NewSessionID("s1")
	tcid := brand.NewToolCallID("t1")
	job := brand.NewJobID("j1")

	if typeName(sid) == typeName(tcid) {
		t.Fatal("SessionID 与 ToolCallID 的底层类型名不应相同")
	}
	if typeName(job) == typeName(tcid) {
		t.Fatal("JobID 与 ToolCallID 的底层类型名不应相同")
	}

	// 断言各类型的确切底层类型名（标签名不可见，但可通过 %T 区分）
	t.Logf("SessionID type = %s", typeName(sid))
	t.Logf("ToolCallID type = %s", typeName(tcid))
	t.Logf("JobID type = %s", typeName(job))
}

// typeName 返回任意值的运行时类型字符串，用于品牌隔离性测试。
func typeName(x any) string {
	return fmt.Sprintf("%T", x)
}

// TestBrandedSQL 验证 driver.Valuer / sql.Scanner 接口，供持久化层复用。
func TestBrandedSQL(t *testing.T) {
	sid := brand.NewSessionID("sess_sql")

	v, err := sid.Value()
	if err != nil {
		t.Fatalf("Value() 失败: %v", err)
	}
	if v != "sess_sql" {
		t.Fatalf("Value() = %v, want sess_sql", v)
	}

	var restored brand.SessionID
	if err := restored.Scan(v); err != nil {
		t.Fatalf("Scan(string) 失败: %v", err)
	}
	if !restored.Equal(sid) {
		t.Fatal("Scan 后应与原值相等")
	}

	// 从 []byte 扫描
	var fromBytes brand.SessionID
	if err := fromBytes.Scan([]byte("sess_sql")); err != nil {
		t.Fatalf("Scan([]byte) 失败: %v", err)
	}
	if !fromBytes.Equal(sid) {
		t.Fatal("Scan([]byte) 后应与原值相等")
	}

	// 从 nil 扫描 → 零值
	var fromNil brand.SessionID
	if err := fromNil.Scan(nil); err != nil {
		t.Fatalf("Scan(nil) 失败: %v", err)
	}
	if !fromNil.IsZero() {
		t.Fatal("Scan(nil) 后应为零值")
	}

	// 非法类型扫描应报错
	if err := fromNil.Scan(42); err == nil {
		t.Fatal("Scan(int) 应报错")
	}
}

// TestBytesBranded 验证 Bytes[T] 的构造、JSON Base64 往返与字节访问。
func TestBytesBranded(t *testing.T) {
	raw := []byte("hello world \x00 binary")
	bs := brand.NewBytes[brand.AttachmentID](raw)

	if bs.IsZero() {
		t.Fatal("NewBytes 构造后不应为零值")
	}
	if bs.Len() != len(raw) {
		t.Fatalf("Len() = %d, want %d", bs.Len(), len(raw))
	}
	if string(bs.Bytes()) != string(raw) {
		t.Fatal("Bytes() 应还原原始字节")
	}

	// JSON Base64 往返
	data, err := json.Marshal(bs)
	if err != nil {
		t.Fatalf("Bytes Marshal 失败: %v", err)
	}
	var restored brand.Bytes[brand.AttachmentID]
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Bytes Unmarshal 失败: %v", err)
	}
	if string(restored.Bytes()) != string(raw) {
		t.Fatal("Bytes JSON 往返后应还原原始字节")
	}

	// 零值
	if !brand.ZeroBytes[brand.AttachmentID]().IsZero() {
		t.Fatal("ZeroBytes 应 IsZero()==true")
	}
}
