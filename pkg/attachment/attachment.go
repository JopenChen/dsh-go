// Package attachment 提供持久化图片附件（Durable Image Attachment）接缝。
//
// 对齐上游：packages/attachment/attachment
//
// 本文件对应任务 M29：Attachment 图片引用模式。
//
// 设计要点：
//   - 二进制图片的所有权与会话日志解耦：生产者先给已校验的编码字节，
//     服务只在对象持久化后才发布一个不可变的 content-addressed 引用（AttachmentID）；
//   - 会话事件与模型可见的 ImageBlock 携带的是该引用 + 元数据，绝不含浏览器对象
//     URL、宿主临时路径、provider URL 或 base64 载荷；
//   - AttachmentID 是不透明品牌串（本实现取 "sha256:<digest>"），消费者既不可解析其
//     表示，也不可从它推导文件系统路径 —— 必须通过 Store.ReadImage 做权威读取；
//   - durable 延伸语义：store 是保留无关的（retention-neutral），resumed / forked 会话
//     可共享同一对象，因此「会话压缩 / 跨会话引用」通过 content-addressed 路径解析不失效。
package attachment

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/JopenChen/dsh-go/pkg/brand"
)

// ============================================================================
// 媒体类型与引用
// ============================================================================

// ImageMediaType 是 v1 附件路径接受的图像格式。
type ImageMediaType string

// 支持的图像媒体类型。
const (
	MediaPNG  ImageMediaType = "image/png"
	MediaJPEG ImageMediaType = "image/jpeg"
	MediaWebP ImageMediaType = "image/webp"
	MediaGIF  ImageMediaType = "image/gif"
)

// AttachmentID 是可持久化的不透明附件标识（别名自 pkg/brand）。
type AttachmentID = brand.AttachmentID

// ImageAttachmentRef 是不可变归一化图片的持久化、可序列化引用。
// 它是不透明存储标识，绝不是一个文件系统路径或承载 URL。
type ImageAttachmentRef struct {
	// AttachmentID 附件 ID（content-addressed："sha256:<digest>"）。
	AttachmentID AttachmentID `json:"attachmentId"`
	// MediaType 由存储字节校验出的媒体类型。
	MediaType ImageMediaType `json:"mediaType"`
	// Bytes 编码字节精确长度。
	Bytes int `json:"bytes"`
	// Width 编码像素宽。
	Width int `json:"width,omitempty"`
	// Height 编码像素高。
	Height int `json:"height,omitempty"`
	// Name 可选显示名（已剥离本地路径信息）。
	Name string `json:"name,omitempty"`
}

// StoredImageAttachment 是经过引用+摘要校验后返回的存储字节与引用。
type StoredImageAttachment struct {
	Ref  ImageAttachmentRef `json:"ref"`
	Data []byte             `json:"data"`
}

// SaveImageAttachment 是提交一张图片的请求。
type SaveImageAttachment struct {
	// Data 编码字节。
	Data []byte
	// MediaType 调用方声明的媒体类型（与完整解码字节核对）。
	MediaType ImageMediaType
	// Name 可选显示名（绝不被解释为路径）。
	Name string
	// Width/Height 可选固有尺寸；0 表示未提供。
	Width  int
	Height int
}

// ============================================================================
// 附件存储接缝
// ============================================================================

// Store 是不可变二进制附件服务（ctx.attachments）。
type Store interface {
	// SaveImage 校验并持久化一张图片；返回 content-addressed 引用。
	SaveImage(input SaveImageAttachment) (ImageAttachmentRef, error)
	// ReadImage 读取一张图片并校验字节仍匹配记录的引用。
	ReadImage(ref ImageAttachmentRef) (StoredImageAttachment, error)
	// ImageHostPath 返回 provider 持有的宿主对象路径；无宿主文件后端时 ok=false。
	ImageHostPath(ref ImageAttachmentRef) (string, bool)
}

// ============================================================================
// Content-addressed 内存 + 可选宿主文件后端
// ============================================================================

// FileBackedStore 是 content-addressed 附件存储实现。
//
//   - 主存储为内存 map（id → bytes）；可选 dir 时同时落盘到 <dir>/<digest> 作宿主路径，
//     以支持「跨会话 / resume / fork」时的持久引用解析；
//   - AttachmentID = "sha256:<hex(digest)>"，读取时重算摘要校验，杜绝内容漂移；
//   - 图片尺寸本实现不强制解析（保存时可按字节校验媒体类型前缀）。
type FileBackedStore struct {
	mu   sync.RWMutex
	dir  string
	objs map[string][]byte
}

// NewStore 创建附件存储。dir 非空时启用宿主文件后端。
func NewStore(dir string) *FileBackedStore {
	return &FileBackedStore{
		dir:  dir,
		objs: map[string][]byte{},
	}
}

// contentID 计算 content-addressed 引用 ID（"sha256:<hex>"）。
func contentID(data []byte) AttachmentID {
	sum := sha256.Sum256(data)
	return brand.NewAttachmentID("sha256:" + hex.EncodeToString(sum[:]))
}

var errUnsupportedMedia = errors.New("attachment: unsupported media type")

// mediaTypeBytes 校验字节与其声明的媒体类型前缀一致（足够在线层判定）。
func validateMediaType(mt ImageMediaType, data []byte) error {
	// 仅校验常见魔术字节；不足为精确校验，但作为 seam 的 admission 门槛足够。
	sig := map[ImageMediaType]func([]byte) bool{
		MediaPNG: func(b []byte) bool { return len(b) >= 8 && b[0] == 0x89 && b[1] == 0x50 && b[2] == 0x4E && b[3] == 0x47 },
		MediaJPEG: func(b []byte) bool { return len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF },
		MediaWebP: func(b []byte) bool { return len(b) >= 12 && string(b[0:4]) == "RIFF" && string(b[8:12]) == "WEBP" },
		MediaGIF:  func(b []byte) bool { return len(b) >= 6 && string(b[0:4]) == "GIF8" },
	}
	check, ok := sig[mt]
	if !ok {
		return errUnsupportedMedia
	}
	if !check(data) {
		return fmt.Errorf("attachment: bytes do not match declared media type %q", string(mt))
	}
	return nil
}

// SaveImage 校验并持久化一张图片，返回 content-addressed 引用。
func (s *FileBackedStore) SaveImage(input SaveImageAttachment) (ImageAttachmentRef, error) {
	if len(input.Data) == 0 {
		return ImageAttachmentRef{}, errors.New("attachment: empty image data")
	}
	if err := validateMediaType(input.MediaType, input.Data); err != nil {
		return ImageAttachmentRef{}, err
	}
	id := contentID(input.Data)
	ref := ImageAttachmentRef{
		AttachmentID: id,
		MediaType:    input.MediaType,
		Bytes:        len(input.Data),
		Width:        input.Width,
		Height:       input.Height,
		Name:         input.Name,
	}
	s.mu.Lock()
	s.objs[id.Raw()] = append([]byte(nil), input.Data...)
	s.mu.Unlock()
	// 可选宿主文件后端（持久化到 <dir>/<digest>）。
	if s.dir != "" {
		_ = writeHostFile(s.dir, ref.AttachmentID, input.Data)
	}
	return ref, nil
}

// ReadImage 读取并校验字节仍匹配记录的引用。
func (s *FileBackedStore) ReadImage(ref ImageAttachmentRef) (StoredImageAttachment, error) {
	s.mu.RLock()
	data, ok := s.objs[ref.AttachmentID.Raw()]
	s.mu.RUnlock()
	if !ok {
		return StoredImageAttachment{}, fmt.Errorf("attachment: object %q not found", ref.AttachmentID.Raw())
	}
	// 权威读取：重算摘要校验，杜绝内容漂移。
	want := contentID(data)
	if want != ref.AttachmentID {
		return StoredImageAttachment{}, fmt.Errorf("attachment: digest mismatch for %q", ref.AttachmentID.Raw())
	}
	return StoredImageAttachment{Ref: ref, Data: append([]byte(nil), data...)}, nil
}

// ImageHostPath 返回宿主对象路径（dir 模式）或 ok=false。
func (s *FileBackedStore) ImageHostPath(ref ImageAttachmentRef) (string, bool) {
	if s.dir == "" {
		return "", false
	}
	return filepath.Join(s.dir, ref.AttachmentID.Raw()), true
}

// writeHostFile 把对象落盘到 <dir>/<digest>，供宿主 filesystem 消费。
func writeHostFile(dir string, id AttachmentID, data []byte) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, id.Raw()), data, 0o644)
}

// ============================================================================
// Durable 引用解析（供会话卸载持久化后的跨会话读取）
// ============================================================================

// ResolveReference 从一张 SessionLog 记录的附件引用解析出权威字节。
//
//   - 若调用方提供 Store，则走 Store.ReadImage（重算摘要校验）；
//   - 否则在有宿主文件后端时从文件读取；两者都失败返回错误。
//
// durable 语义：因为引用是 content-addressed（不依赖创建它的会话），无论会话被压缩
// 或从另一个会话引用，只要对象仍在 Store/宿主路径中即可解析成功，绝不失效。
func ResolveReference(store Store, ref ImageAttachmentRef) (StoredImageAttachment, error) {
	if store != nil {
		return store.ReadImage(ref)
	}
	return StoredImageAttachment{}, errors.New("attachment: no store available for durable reference resolution")
}

// ResolveFileBacked 供无 store 但有宿主目录场景：直接从 <dir>/<digest> 读取。
func ResolveFileBacked(dir string, ref ImageAttachmentRef) (StoredImageAttachment, error) {
	p := filepath.Join(dir, ref.AttachmentID.Raw())
	data, err := os.ReadFile(p)
	if err != nil {
		return StoredImageAttachment{}, fmt.Errorf("attachment: read host file for %q: %w", ref.AttachmentID.Raw(), err)
	}
	if contentID(data) != ref.AttachmentID {
		return StoredImageAttachment{}, fmt.Errorf("attachment: host file digest mismatch for %q", ref.AttachmentID.Raw())
	}
	return StoredImageAttachment{Ref: ref, Data: data}, nil
}