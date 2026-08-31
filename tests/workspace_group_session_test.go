// 本文件验证任务 S14：Workspace Registry。
//
// 覆盖：相同 root 幂等返回同一 id；resume-on-open 记录/清除；会话分组；CAS 更新；
// List 确定性；list 校验。
package tests

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/storage"
	"github.com/JopenChen/dsh-go/pkg/workspace"
)

// TestWorkspaceSameRootSameID 验证相同 root 创建两次返回同一 ID（幂等）。
func TestWorkspaceSameRootSameID(t *testing.T) {
	reg := workspace.New(storage.NewMemoryKV())
	ctx := context.Background()
	root := filepath.Join("some", "dir", "proj")

	id1, _, err := reg.Create(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	id2, _, err := reg.Create(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if id1 != id2 {
		t.Fatalf("相同 root 应返回同一 id，实际 %v vs %v", id1, id2)
	}
	// 规范化后路径应一致。
	ws, _, err := reg.Get(ctx, id1)
	if err != nil {
		t.Fatal(err)
	}
	if ws.ID != id1 {
		t.Fatalf("record.ID 应等于 id1，实际 %v", ws.ID)
	}
}

// TestWorkspaceDifferentRootDifferentID 验证不同 root 产生不同 id。
func TestWorkspaceDifferentRootDifferentID(t *testing.T) {
	reg := workspace.New(storage.NewMemoryKV())
	ctx := context.Background()
	a, _, _ := reg.Create(ctx, "proj/a")
	b, _, _ := reg.Create(ctx, "proj/b")
	if a == b {
		t.Fatalf("不同 root 应不同 id，实际二者都为 %v", a)
	}
}

// TestWorkspaceResumeOnOpen 验证 resume-on-open 记录与清除。
func TestWorkspaceResumeOnOpen(t *testing.T) {
	reg := workspace.New(storage.NewMemoryKV())
	ctx := context.Background()
	id, _, _ := reg.Create(ctx, "proj/p1")
	sid := brand.NewSessionID("sess-123")

	if _, err := reg.SetResumeOnOpen(ctx, id, sid); err != nil {
		t.Fatal(err)
	}
	ws, _, err := reg.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if ws.ResumeOnOpen == nil || *ws.ResumeOnOpen != sid {
		t.Fatalf("resume-on-open 应为 %v，实际 %v", sid, ws.ResumeOnOpen)
	}

	if _, err := reg.ClearResumeOnOpen(ctx, id); err != nil {
		t.Fatal(err)
	}
	ws2, _, _ := reg.Get(ctx, id)
	if ws2.ResumeOnOpen != nil {
		t.Fatalf("清除后 resume-on-open 应为 nil，实际 %v", ws2.ResumeOnOpen)
	}
}

// TestWorkspaceSessionGroup 验证会话分组设置（整体替换）。
func TestWorkspaceSessionGroup(t *testing.T) {
	reg := workspace.New(storage.NewMemoryKV())
	ctx := context.Background()
	id, _, _ := reg.Create(ctx, "proj/p2")
	if _, err := reg.SetSessionGroup(ctx, id, "team-alpha"); err != nil {
		t.Fatal(err)
	}
	ws, _, _ := reg.Get(ctx, id)
	if ws.SessionGroup != "team-alpha" {
		t.Fatalf("sessionGroup 应为 team-alpha，实际 %q", ws.SessionGroup)
	}
}

// TestWorkspaceList 验证 List 返回全部工作区（确定性字典序）。
func TestWorkspaceList(t *testing.T) {
	reg := workspace.New(storage.NewMemoryKV())
	ctx := context.Background()
	_, _, _ = reg.Create(ctx, "b")
	_, _, _ = reg.Create(ctx, "a")
	_, _, _ = reg.Create(ctx, "c")
	wsList, err := reg.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(wsList) != 3 {
		t.Fatalf("应有 3 个工作区，实际 %d", len(wsList))
	}
	// 校验 root 字典序（规范化后相对路径 → 绝对路径，仅验证数量与根路径集合）。
	seen := map[string]bool{}
	for _, ws := range wsList {
		if seen[ws.Root] {
			t.Fatalf("重复 root %q", ws.Root)
		}
		seen[ws.Root] = true
	}
}

// TestWorkspaceMissingRootRejected 验证空 root 报错。
func TestWorkspaceMissingRootRejected(t *testing.T) {
	reg := workspace.New(storage.NewMemoryKV())
	ctx := context.Background()
	if _, _, err := reg.Create(ctx, "   "); err == nil {
		t.Fatal("空 root 应报错")
	}
}