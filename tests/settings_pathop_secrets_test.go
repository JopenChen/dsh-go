// 本文件对应任务 M38：Settings 接缝 + pathop + CAS + secrets。
package tests

import (
	"strings"
	"testing"

	"github.com/JopenChen/dsh-go/pkg/settings"
)

// TestSettingsPathOpSecrets 验证 secret 字段 describe(redactSecrets:true) 只给操作位、value 脱敏。
func TestSettingsPathOpSecrets(t *testing.T) {
	ns := settings.NewNamespace("dsh")
	ns.MarkSecret("credentials.api_key")
	ns.MarkSecret("credentials.secret")

	s := settings.NewSettingsScope(ns)
	_ = s.ApplyHost("sandbox.mode", "read-only")
	_ = s.ApplyHost("credentials.api_key", "sk-secret-123")

	// 更新 session 层：覆盖 sandbox.mode + 设置 credentials.secret
	err := s.Update(0, map[settings.Path]settings.PathOp{
		"sandbox.mode":        settings.NewSetOp("workspace-write"),
		"credentials.secret":  settings.NewSetOp("supersecret"),
		"unused.path":         settings.NewUnsetOp(),
	})
	if err != nil {
		t.Fatalf("Update 失败: %v", err)
	}

	// describe(redactSecrets:true)：secret 只给 op，不给 value
	desc := s.Describe(true)
	apiKey := findPath(desc, "credentials.api_key")
	secret := findPath(desc, "credentials.secret")
	if apiKey == nil || secret == nil {
		t.Fatalf("secret 路径应存在: %+v", desc)
	}
	if apiKey.Value != nil {
		t.Fatalf("secret api_key 应脱敏: %+v", apiKey)
	}
	if secret.Value != nil {
		t.Fatalf("secret secret 应脱敏: %+v", secret)
	}
	if !apiKey.Secret || !secret.Secret {
		t.Fatal("secret 标记应为 true")
	}

	// describe(redactSecrets:false)：可见明文
	descOpen := s.Describe(false)
	if p := findPath(descOpen, "credentials.secret"); p == nil || p.Value != "supersecret" {
		t.Fatalf("不脱敏时应可见明文: %+v", p)
	}
}

// findPath 在 describe 结果中查找路径。
func findPath(list []settings.DescribedSetting, p settings.Path) *settings.DescribedSetting {
	for i := range list {
		if list[i].Path == p {
			return &list[i]
		}
	}
	return nil
}

// TestSettingsCASConcurrent 验证并发两次 replace 后写 expectedRevision 不符触发 CAS 错误。
func TestSettingsCASConcurrent(t *testing.T) {
	ns := settings.NewNamespace("dsh")
	s := settings.NewSettingsScope(ns)

	// 第一次 replace（version 0 → 1）
	if _, err := 0, func() error {
		return s.Replace(0, map[settings.Path]any{"a": 1})
	}(); err != nil {
		t.Fatalf("首次 replace 失败: %v", err)
	}
	rev1 := s.Revision()
	if rev1 != 1 {
		t.Fatalf("首次 replace 后 revision = %d, want 1", rev1)
	}

	// 模拟并发：拿到 rev1 后再有另一次 replace 成功（revision → 2）
	if err := s.Replace(rev1, map[settings.Path]any{"b": 2}); err != nil {
		t.Fatalf("第二次 replace 失败: %v", err)
	}
	if s.Revision() != 2 {
		t.Fatalf("第二次 replace 后 revision = %d, want 2", s.Revision())
	}

	// 现在用过期的 rev1 再写 → CAS 冲突
	err := s.Replace(rev1, map[settings.Path]any{"c": 3})
	if !settings.IsRevisionMismatch(err) {
		t.Fatalf("过期修订号写入应触发 CAS 错误, 实际 %v", err)
	}
	var mismatch *settings.ErrRevisionMismatch
	if !asRevMismatch(err, &mismatch) {
		t.Fatalf("错误类型应为 *ErrRevisionMismatch: %T", err)
	}
	if mismatch.Expected != rev1 || mismatch.Actual != 2 {
		t.Fatalf("CAS 字段异常: %+v", mismatch)
	}
}

// asRevMismatch 便捷类型断言。
func asRevMismatch(err error, target **settings.ErrRevisionMismatch) bool {
	e, ok := err.(*settings.ErrRevisionMismatch)
	if ok {
		*target = e
	}
	return ok
}

// TestSettingsNearestWins 验证 host 默认 + session 覆盖（nearest-wins）。
func TestSettingsNearestWins(t *testing.T) {
	ns := settings.NewNamespace("dsh")
	s := settings.NewSettingsScope(ns)
	_ = s.ApplyHost("sandbox.mode", "read-only")

	// 未覆盖前读到 host 值
	if v, ok := s.Get("sandbox.mode"); !ok || !strings.Contains(string(v), "read-only") {
		t.Fatalf("未覆盖时应读到 host 默认: %q ok=%v", v, ok)
	}

	// session 覆盖
	if err := s.Update(0, map[settings.Path]settings.PathOp{
		"sandbox.mode": settings.NewSetOp("danger-full-access"),
	}); err != nil {
		t.Fatalf("Update 失败: %v", err)
	}
	if v, ok := s.Get("sandbox.mode"); !ok || !strings.Contains(string(v), "danger-full-access") {
		t.Fatalf("session 覆盖后应读到新值: %q ok=%v", v, ok)
	}

	// describe 中 op 为 set（session 层）
	desc := s.Describe(true)
	if p := findPath(desc, "sandbox.mode"); p == nil || p.Op != "set" {
		t.Fatalf("session 覆盖后 op 应为 set: %+v", p)
	}
}
