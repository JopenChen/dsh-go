// Package tests 的文件系统接缝（M35）验收测试。
//
// 覆盖：
//   - obs-policy：未观察目标拒绝裸写（FS_NOT_OBSERVED）；读后写/编辑走版本守卫
//   - 并发两个 fs_edit 同一行 → 第二个因 FsVersion mismatch 报 FS_STALE_VERSION
//   - 原子写/读/列表/EditNotFound/Ambiguous
package tests

import (
	"strings"
	"testing"

	"github.com/JopenChen/dsh-go/pkg/fs"
)

// TestFsObsPolicyRejectsUnobservedWrite 验证 obs-policy 下未观察目标拒绝裸写。
func TestFsObsPolicyRejectsUnobservedWrite(t *testing.T) {
	root := t.TempDir()
	local := fs.NewLocal(root)
	obs := fs.NewObservationPolicy()
	svc := fs.NewPolicyFS(local, obs)

	target, _ := local.Resolve("a.txt")
	// 未观察 → 策略给 createIfAbsent；但目标首次不存在应允许 create。
	// 这里先创建文件，再让策略认为「未观察」到另一目标则不能覆盖已被策略误判的状态。
	// 明确场景：文件已存在且当前 owner 从未观察 → decideWrite 返回 createIfAbsent，
	// 但文件已存在 → FS_NOT_OBSERVED。
	_ = target
	existing, _ := local.Resolve("existing.txt")
	if _, err := svc.Write("owner1", existing, "hello"); err != nil {
		t.Fatalf("首个写(createIfAbsent, 文件不存在)应成功: %v", err)
	}
	// 再次写同一目标但「当前 owner 未观察该版本」→ 现在的 write 决策仍 createIfAbsent，
	// 但文件已存在 → FS_NOT_OBSERVED。
	_, err := svc.Write("owner1", existing, "hello2")
	if fs.Code(err) != fs.FSErrNotObserved {
		t.Fatalf("未观察版本 + 文件已存在应 FS_NOT_OBSERVED, 实际 %v", err)
	}
}

// TestFsReadThenWriteGuarded 验证先读后写（reflect 观察 → replaceIfVersion 成功）。
func TestFsReadThenWriteGuarded(t *testing.T) {
	root := t.TempDir()
	local := fs.NewLocal(root)
	obs := fs.NewObservationPolicy()
	svc := fs.NewPolicyFS(local, obs)

	target, _ := local.Resolve("g.txt")
	// 先创建（createIfAbsent 允许首次）。
	if _, err := svc.Write("o", target, "v1"); err != nil {
		t.Fatal(err)
	}
	// 读取并登记观察（present at version）。
	svc.ReflectObservation("o", target)
	// 基于该版本 replaceIfVersion 覆盖成功。
	res, err := svc.Write("o", target, "v2")
	if err != nil {
		t.Fatalf("观察后 replaceIfVersion 应成功: %v", err)
	}
	if res.Operation != "update" {
		t.Fatalf("应为 update, 实际 %s", res.Operation)
	}
	got, _ := local.ReadText(target)
	if got != "v2" {
		t.Fatalf("内容应为 v2, 实际 %q", got)
	}
}

// TestFsConcurrentEditStaleVersion 验证并发两个 fs_edit 同一行 → 第二个报 FS_STALE_VERSION。
func TestFsConcurrentEditStaleVersion(t *testing.T) {
	root := t.TempDir()
	local := fs.NewLocal(root)
	obs := fs.NewObservationPolicy()
	svc := fs.NewPolicyFS(local, obs)

	target, _ := local.Resolve("c.txt")
	if _, err := svc.Write("o", target, "aaa\nbbb\n"); err != nil {
		t.Fatal(err)
	}
	svc.ReflectObservation("o", target)

	// 第一个 edit 成功提升版本。
	if _, err := svc.Edit("o", target, fs.FsEditRequest{OldString: "aaa", NewString: "AAA"}); err != nil {
		t.Fatalf("第一个 edit 应成功: %v", err)
	}
	// 第二个 edit 用同一观察版本（已陈旧）→ FS_STALE_VERSION。
	edit := fs.FsEditRequest{OldString: "bbb", NewString: "BBB"}
	if _, err := svc.Edit("o", target, edit); err == nil {
		t.Fatal("陈旧版本编辑应报错")
	} else if fs.Code(err) != fs.FSErrStaleVersion {
		t.Fatalf("应 FS_STALE_VERSION, 实际 %v", err)
	}
}

// TestFsEditNotFoundAndAmbiguous 验证字面编辑不匹配/多匹配错误。
func TestFsEditNotFoundAndAmbiguous(t *testing.T) {
	local := fs.NewLocal(t.TempDir())
	target, _ := local.Resolve("e.txt")
	if _, err := local.WriteText(target, "x y x\n", nil); err != nil {
		t.Fatal(err)
	}
	// 多匹配未设 replaceAll → AmbiguousEdit。
	if _, err := local.EditText(target, fs.FsEditRequest{OldString: "x", NewString: "z"}, nil); fs.Code(err) != fs.FSErrAmbiguousEdit {
		t.Fatalf("多匹配应 AmbiguousEdit, 实际 %v", err)
	}
	// 找不到 → EditNotFound；replaceAll 则成功。
	if _, err := local.EditText(target, fs.FsEditRequest{OldString: "q", NewString: "z"}, nil); fs.Code(err) != fs.FSErrEditNotFound {
		t.Fatalf("无匹配应 EditNotFound, 实际 %v", err)
	}
	if _, err := local.EditText(target, fs.FsEditRequest{OldString: "x", NewString: "z", ReplaceAll: true}, nil); err != nil {
		t.Fatalf("replaceAll 应成功: %v", err)
	}
}

// TestFsReadBytesTooLarge 验证超过上限报 FS_TOO_LARGE。
func TestFsReadBytesTooLarge(t *testing.T) {
	local := fs.NewLocal(t.TempDir())
	target, _ := local.Resolve("big.txt")
	if _, err := local.WriteText(target, strings.Repeat("a", 4096), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := local.ReadBytes(target, 100); fs.Code(err) != fs.FSErrTooLarge {
		t.Fatalf("超上限应 FS_TOO_LARGE, 实际 %v", err)
	}
}

// TestFsVersionStability 验证同样内容+时间派生版本稳定（防抖）。
func TestFsVersionStability(t *testing.T) {
	local := fs.NewLocal(t.TempDir())
	target, _ := local.Resolve("v.txt")
	if _, err := local.WriteText(target, "stable", nil); err != nil {
		t.Fatal(err)
	}
	v1, _ := local.ObservedVersion(target)
	v2, _ := local.ObservedVersion(target)
	if v1 != v2 {
		t.Fatalf("同状态两次版本应一致: %s vs %s", v1, v2)
	}
}