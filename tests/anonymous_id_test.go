// Package tests 的 telemetry 匿名 ID 验收测试。
package tests

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JopenChen/dsh-go/pkg/telemetry"
)

func TestAnonymousIDCreateAndPersist(t *testing.T) {
	dir := t.TempDir()
	env := map[string]string{"DSH_HOME": dir}

	id1, err := telemetry.GetOrCreateAnonymousID(env)
	if err != nil {
		t.Fatal(err)
	}
	// 文件应已持久化。
	raw, err := os.ReadFile(filepath.Join(dir, telemetry.AnonymousIDFileName))
	if err != nil {
		t.Fatalf("not persisted: %v", err)
	}
	if string(raw) != id1+"\n" {
		t.Fatalf("persisted = %q", raw)
	}
	// 第二次读取应得到同一 ID。
	id2, _ := telemetry.GetOrCreateAnonymousID(env)
	if id1 != id2 {
		t.Fatalf("id must be stable: %q vs %q", id1, id2)
	}
}

func TestAnonymousIDCorruptRebuild(t *testing.T) {
	dir := t.TempDir()
	env := map[string]string{"DSH_HOME": dir}
	if err := os.WriteFile(filepath.Join(dir, telemetry.AnonymousIDFileName), []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	id, err := telemetry.GetOrCreateAnonymousID(env)
	if err != nil {
		t.Fatal(err)
	}
	if id == "garbage" || len(id) < 32 {
		t.Fatalf("corrupt file must be rebuilt, got %q", id)
	}
}
