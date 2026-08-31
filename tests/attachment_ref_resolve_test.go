// Package tests 的附件图片引用（M29）验收测试。
//
// 覆盖：
//   - 保存 → 返回 content-addressed 引用（不透明 AttachmentID）
//   - 通过 durable 引用跨会话解析不失效（对象共享 + 摘要校验）
//   - 会话"压缩"场景（仅保留引用）后再解析仍字节一致
//   - 媒体类型 admission 校验 + 摘要篡改拒绝
package tests

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JopenChen/dsh-go/pkg/attachment"
	"github.com/JopenChen/dsh-go/pkg/brand"
)

// pngBytes 是最小合法 PNG 文件字节（PNG 魔术头 + 后置 IEND），仅用于媒体类型门槛校验。
func pngBytes() []byte {
	return []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D}
}

// TestAttachmentSaveAndResolve 验证保存 → 引用 → 解析字节一致。
func TestAttachmentSaveAndResolve(t *testing.T) {
	store := attachment.NewStore("")
	data := pngBytes()
	ref, err := store.SaveImage(attachment.SaveImageAttachment{Data: data, MediaType: attachment.MediaPNG, Name: "a.png"})
	if err != nil {
		t.Fatalf("SaveImage 失败: %v", err)
	}
	if ref.AttachmentID == (attachment.AttachmentID{}) {
		t.Fatal("应返回 content-addressed AttachmentID")
	}
	if !strings.HasPrefix(ref.AttachmentID.Raw(), "sha256:") {
		t.Fatalf("AttachmentID 应为 sha256:<digest>, 实际 %q", ref.AttachmentID.Raw())
	}
	if ref.Bytes != len(data) {
		t.Fatalf("Bytes 应记录长度 %d, 实际 %d", len(data), ref.Bytes)
	}
	stored, err := attachment.ResolveReference(store, ref)
	if err != nil {
		t.Fatalf("ResolveReference 失败: %v", err)
	}
	if !bytes.Equal(stored.Data, data) {
		t.Fatal("解析字节与保存字节不一致")
	}
}

// TestAttachmentDurableAcrossCompactionAndSession 验证 durable 引用跨会话/压缩解析不失效。
func TestAttachmentDurableAcrossCompactionAndSession(t *testing.T) {
	// 共享的 content-addressed 存储（retention-neutral：多个会话共享对象）。
	dir := t.TempDir()
	store := attachment.NewStore(dir)
	data := pngBytes()

	// 会话 A 保存图片。
	refA, err := store.SaveImage(attachment.SaveImageAttachment{Data: data, MediaType: attachment.MediaPNG})
	if err != nil {
		t.Fatal(err)
	}

	// 会话 B（跨会话）只拿到引用 refA（模拟跨会话引用）。
	refB := attachment.ImageAttachmentRef{AttachmentID: refA.AttachmentID, MediaType: refA.MediaType, Bytes: refA.Bytes}
	stored, err := attachment.ResolveReference(store, refB)
	if err != nil {
		t.Fatalf("跨会话 durable 引用解析失败: %v", err)
	}
	if !bytes.Equal(stored.Data, data) {
		t.Fatal("跨会话解析字节不一致")
	}

	// 会话压缩场景：把 store 丢弃，仅依赖宿主文件 durable 路径重新解析。
	refOnly := refA
	fileBacked, err := attachment.ResolveFileBacked(dir, refOnly)
	if err != nil {
		t.Fatalf("宿主文件 durable 路径解析失败: %v", err)
	}
	if !bytes.Equal(fileBacked.Data, data) {
		t.Fatal("宿主文件解析字节不一致")
	}
}

// TestAttachmentMediaTypeAdmission 验证媒体类型 admission 校验。
func TestAttachmentMediaTypeAdmission(t *testing.T) {
	store := attachment.NewStore("")
	// 声明 PNG 但字节不是 PNG → 拒绝。
	if _, err := store.SaveImage(attachment.SaveImageAttachment{Data: []byte("not an image"), MediaType: attachment.MediaPNG}); err == nil {
		t.Fatal("字节与声明媒体类型不符应被拒绝")
	}
	// 合法 PNG → 接受。
	if _, err := store.SaveImage(attachment.SaveImageAttachment{Data: pngBytes(), MediaType: attachment.MediaPNG}); err != nil {
		t.Fatalf("合法 PNG 应被接受: %v", err)
	}
}

// TestAttachmentReadUnknownID 验证读取未知/伪造引用时报错（权威读取拒绝）。
func TestAttachmentReadUnknownID(t *testing.T) {
	store := attachment.NewStore("")
	_, err := store.ReadImage(attachment.ImageAttachmentRef{
		AttachmentID: brand.NewAttachmentID("sha256:deadbeef"),
		MediaType:    attachment.MediaPNG,
	})
	if err == nil {
		t.Fatal("未知对象应报错")
	}
}

// TestAttachmentHostFileDigestCatch 验证宿主文件被篡改后 digest 不匹配拒绝。
func TestAttachmentHostFileDigestCatch(t *testing.T) {
	dir := t.TempDir()
	store := attachment.NewStore(dir)
	data := pngBytes()
	ref, err := store.SaveImage(attachment.SaveImageAttachment{Data: data, MediaType: attachment.MediaPNG})
	if err != nil {
		t.Fatal(err)
	}
	// 篡改宿主文件内容（改为非法 PNG 字节），使其 digest 与引用不匹配。
	if err := os.WriteFile(filepath.Join(dir, ref.AttachmentID.Raw()), []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := attachment.ResolveFileBacked(dir, ref); err == nil {
		t.Fatal("篡改后的宿主文件应因 digest 不匹配被拒绝")
	}
}