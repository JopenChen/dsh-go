// Package fs 提供文件系统（Filesystem）接缝。
//
// 对齐上游：packages/fs/fs + fs-local + fs-observation-policy（M35）
//
// 本文件实现：FsTarget/FsVersion/FsInfo/FsEditRequest/FsWriteIntent 类型 + LocalFS 提供者
// + 稳定错误码（FS_*）。观察策略见 policy.go。
//
// 设计要点：
//   - resolve() 把用户路径解析成稳定身份 FsTarget（targetKey 不透明，displayPath 供展示）；
//   - 版本令牌 FsVersion 由 stat 身份+新鲜度派生，作为 write/edit 的陈旧守卫；
//   - writeText/editText 的 expected 是可选的守卫：省略则为无约束覆盖，提供则按意图校验
//     （createIfAbsent / replaceIfVersion / 版本匹配）；
//   - 所有失败用稳定 FsErrorCode（FS_NOT_FOUND / FS_STALE_VERSION / FS_NOT_OBSERVED /
//     FS_AMBIGUOUS_EDIT / FS_EDIT_NOT_FOUND / FS_TOO_LARGE ...）。
package fs

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ============================================================================
// 类型
// ============================================================================

// FsTargetKey 是目标的不透明键（供陈旧守卫与查找）。
type FsTargetKey string

// FsVersion 是不透明文件版本令牌（write/edit 守卫用）。
type FsVersion string

// FsTarget 是后端解析后的稳定文件身份。
type FsTarget struct {
	// TargetKey 不透明键（本地后端用规范化绝对路径）。
	TargetKey FsTargetKey
	// DisplayPath 模型/UI 向展示路径。
	DisplayPath string
}

// FsInfo 是 stat 返回的元数据（不含内容）。
type FsInfo struct {
	// Version 当前新鲜度令牌。
	Version FsVersion `json:"version"`
	// Type 文件类型。
	Type string `json:"type"` // file / directory / other
	// Size 常规文件字节数。
	Size int64 `json:"size,omitempty"`
}

// FsEditRequest 是一次字面替换编辑请求。
type FsEditRequest struct {
	// OldString 要替换的非空字面文本。
	OldString string
	// NewString 替换文本；空串删除匹配文本。
	NewString string
	// ReplaceAll 替换所有匹配（而非要求恰好一个）。
	ReplaceAll bool
}

// FsWriteIntent 是带守卫的写意图。
type FsWriteIntent struct {
	// Kind：createIfAbsent 拒绝已存在；replaceIfVersion 按版本替换。
	Kind string
	// Version replaceIfVersion 期望的版本。
	Version FsVersion
}

// FsWriteOutcome 是一次整文件写的结果。
type FsWriteOutcome struct {
	Operation string // create / update
	Version   FsVersion
	Before    string
	After     string
}

// FsEditOutcome 是一次字面编辑的结果。
type FsEditOutcome struct {
	Version FsVersion
	Before  string
	After   string
}

// ============================================================================
// 稳定错误码
// ============================================================================

// FsErrorCode 是文件系统失败稳定码。
type FsErrorCode string

// 错误码枚举（对齐上游 FsErrorCode）。
const (
	FSErrNotFound       FsErrorCode = "FS_NOT_FOUND"
	FSErrNotDirectory   FsErrorCode = "FS_NOT_DIRECTORY"
	FSErrNotText        FsErrorCode = "FS_NOT_TEXT"
	FSErrNotRegularFile FsErrorCode = "FS_NOT_REGULAR_FILE"
	FSErrTooLarge       FsErrorCode = "FS_TOO_LARGE"
	FSErrPermission     FsErrorCode = "FS_PERMISSION_DENIED"
	FSErrSandboxDenied  FsErrorCode = "FS_SANDBOX_DENIED"
	FSErrIO             FsErrorCode = "FS_IO_ERROR"
	FSErrStaleVersion   FsErrorCode = "FS_STALE_VERSION"
	FSErrNotObserved    FsErrorCode = "FS_NOT_OBSERVED"
	FSErrAmbiguousEdit  FsErrorCode = "FS_AMBIGUOUS_EDIT"
	FSErrEditNotFound   FsErrorCode = "FS_EDIT_NOT_FOUND"
)

// FsError 是携带稳定码的文件系统错误。
type FsError struct {
	Code FsErrorCode
	Msg  string
}

func (e *FsError) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Msg) }

func fsErr(code FsErrorCode, format string, args ...any) error {
	return &FsError{Code: code, Msg: fmt.Sprintf(format, args...)}
}

// Code 返回错误码。
func Code(err error) FsErrorCode {
	if err == nil {
		return ""
	}
	if fe, ok := err.(*FsError); ok {
		return fe.Code
	}
	return FSErrIO
}

// ============================================================================
// 本地提供者
// ============================================================================

// LocalFS 是本地磁盘文件系统实现。
type LocalFS struct {
	// Cwd 相对路径解析基准（可选）。
	Cwd string
}

// NewLocal 创建本地文件系统。
func NewLocal(cwd string) *LocalFS {
	return &LocalFS{Cwd: cwd}
}

// Resolve 把路径解析成稳定目标（相对路径按 Cwd 解析，跟随符号链接得规范绝对路径）。
func (l *LocalFS) Resolve(path string) (FsTarget, error) {
	abs := path
	if !filepath.IsAbs(abs) {
		if l.Cwd == "" {
			return FsTarget{}, fsErr(FSErrIO, "no cwd for relative path %q", path)
		}
		abs = filepath.Join(l.Cwd, path)
	}
	real, err := filepath.Abs(abs)
	if err != nil {
		return FsTarget{}, fsErr(FSErrIO, "resolve %q: %v", path, err)
	}
	// resolve 跟随符号链接（realpath 语义由 EvalSymlinks 实现，允许失败则回退 Clean）。
	if rl, err := filepath.EvalSymlinks(real); err == nil {
		real = filepath.Clean(rl)
	}
	return FsTarget{TargetKey: FsTargetKey(filepath.Clean(real)), DisplayPath: filepath.Clean(abs)}, nil
}

// ProcessPath 返回子进程可打开的目标规范绝对路径。
func (l *LocalFS) ProcessPath(t FsTarget) string { return string(t.TargetKey) }

// FileURL 返回 file: URI。
func (l *LocalFS) FileURL(t FsTarget) string {
	p := filepath.ToSlash(string(t.TargetKey))
	return "file://" + p
}

// Contains 判定 child 是 parent 或其后代（基于规范前缀）。
func (l *LocalFS) Contains(parent, child FsTarget) bool {
	p := string(parent.TargetKey)
	c := string(child.TargetKey)
	if c == p {
		return true
	}
	return strings.HasPrefix(c, p+string(filepath.Separator))
}

// versionOf 由文件 stat 身份+新鲜度派生版本令牌。
func versionOf(path string) (FsVersion, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	h.Write([]byte(path))
	var buf [16]byte
	binary.LittleEndian.PutUint64(buf[0:8], uint64(info.Size()))
	binary.LittleEndian.PutUint64(buf[8:16], uint64(info.ModTime().UnixNano()))
	h.Write(buf[:])
	return FsVersion(fmt.Sprintf("%x", h.Sum(nil))[:16]), nil
}

// Stat 返回目标元数据；目标不存在返回 (nil, FS_NOT_FOUND)。
func (l *LocalFS) Stat(t FsTarget) (*FsInfo, error) {
	path := string(t.TargetKey)
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fsErr(FSErrNotFound, "no such target %q", t.DisplayPath)
		}
		return nil, fsErr(FSErrIO, "stat %q: %v", path, err)
	}
	v, _ := versionOf(path)
	typ := "other"
	switch {
	case info.IsDir():
		typ = "directory"
	case info.Mode().IsRegular():
		typ = "file"
	}
	return &FsInfo{Version: v, Type: typ, Size: info.Size()}, nil
}

// ReadText 读取整文件 UTF-8 文本。
func (l *LocalFS) ReadText(t FsTarget) (string, error) {
	path := string(t.TargetKey)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fsErr(FSErrNotFound, "%q not found", t.DisplayPath)
		}
		return "", fsErr(FSErrIO, "read %q: %v", path, err)
	}
	// 简单校验可作 UTF-8（用 strings.ToValidUTF8 检测非法字节）。
	if strings.ContainsRune(string(data), '\uFFFD') && !utf8Valid(data) {
		return "", fsErr(FSErrNotText, "%q is not valid UTF-8 text", t.DisplayPath)
	}
	// LF 归一化（差异化基准）。
	return normalizeLF(string(data)), nil
}

// ReadBytes 读取原始字节，超过 maxBytes 报 FS_TOO_LARGE。
func (l *LocalFS) ReadBytes(t FsTarget, maxBytes int) ([]byte, error) {
	data, err := os.ReadFile(string(t.TargetKey))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fsErr(FSErrNotFound, "%q not found", t.DisplayPath)
		}
		return nil, fsErr(FSErrIO, "%v", err)
	}
	if len(data) > maxBytes {
		return nil, fsErr(FSErrTooLarge, "%q exceeds %d bytes", t.DisplayPath, maxBytes)
	}
	return data, nil
}

// ListDir 列出目录直接子项（稳定名序，仅元数据）。
func (l *LocalFS) ListDir(t FsTarget) ([]FsDirEntry, error) {
	entries, err := os.ReadDir(string(t.TargetKey))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fsErr(FSErrNotFound, "%q not found", t.DisplayPath)
		}
		return nil, fsErr(FSErrIO, "list %q: %v", t.DisplayPath, err)
	}
	out := make([]FsDirEntry, 0, len(entries))
	for _, e := range entries {
		typ := "file"
		if e.IsDir() {
			typ = "directory"
		}
		child, _ := l.Resolve(filepath.Join(string(t.TargetKey), e.Name()))
		out = append(out, FsDirEntry{Name: e.Name(), Type: typ, Target: child})
	}
	return out, nil
}

// FsDirEntry 是目录直接子项。
type FsDirEntry struct {
	Name   string
	Type   string
	Target FsTarget
	Size   int64
}

// WriteText 原子创建/替换文件；expected 提供守卫。
func (l *LocalFS) WriteText(t FsTarget, content string, expected *FsWriteIntent) (FsWriteOutcome, error) {
	path := string(t.TargetKey)
	_, statErr := os.Stat(path)
	exists := statErr == nil
	before := ""
	if exists {
		b, _ := os.ReadFile(path)
		before = normalizeLF(string(b))
	}

	// 应用守卫。
	if expected != nil {
		switch expected.Kind {
		case "createIfAbsent":
			if exists {
				return FsWriteOutcome{}, fsErr(FSErrNotObserved, "%q already exists for createIfAbsent", t.DisplayPath)
			}
		case "replaceIfVersion":
			if !exists {
				return FsWriteOutcome{}, fsErr(FSErrStaleVersion, "%q absent, cannot replaceIfVersion", t.DisplayPath)
			}
			cur, _ := versionOf(path)
			if cur != expected.Version {
				return FsWriteOutcome{}, fsErr(FSErrStaleVersion, "%q version mismatch", t.DisplayPath)
			}
		}
	}

	after := normalizeLF(content)
	// 原子写：写入临时文件后重命名。
	if err := atomicWrite(path, after); err != nil {
		return FsWriteOutcome{}, fsErr(FSErrIO, "write %q: %v", t.DisplayPath, err)
	}
	v, _ := versionOf(path)
	op := "update"
	if !exists {
		op = "create"
	}
	return FsWriteOutcome{Operation: op, Version: v, Before: before, After: after}, nil
}

// EditText 原子做字面替换编辑；expected 提供版本守卫（先验版本再匹配）。
func (l *LocalFS) EditText(t FsTarget, edit FsEditRequest, expected *FsVersion) (FsEditOutcome, error) {
	path := string(t.TargetKey)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return FsEditOutcome{}, fsErr(FSErrStaleVersion, "%q absent", t.DisplayPath)
		}
		return FsEditOutcome{}, fsErr(FSErrIO, "%v", err)
	}
	// 版本守卫优先于字面匹配：陈旧先报 FS_STALE_VERSION。
	cur, _ := versionOf(path)
	if expected != nil && cur != *expected {
		return FsEditOutcome{}, fsErr(FSErrStaleVersion, "%q version mismatch (observed %s, now %s)", t.DisplayPath, *expected, cur)
	}
	before, _ := os.ReadFile(path)
	bf := normalizeLF(string(before))

	var after string
	switch {
	case edit.ReplaceAll:
		after = strings.ReplaceAll(bf, edit.OldString, edit.NewString)
		if after == bf {
			return FsEditOutcome{}, fsErr(FSErrEditNotFound, "%q has no match for %q", t.DisplayPath, edit.OldString)
		}
	default:
		idx := strings.Index(bf, edit.OldString)
		if idx < 0 {
			return FsEditOutcome{}, fsErr(FSErrEditNotFound, "%q has no match for %q", t.DisplayPath, edit.OldString)
		}
		if !edit.ReplaceAll && countOccurrences(bf, edit.OldString) != 1 {
			return FsEditOutcome{}, fsErr(FSErrAmbiguousEdit, "%q has multiple matches for %q; set replaceAll", t.DisplayPath, edit.OldString)
		}
		after = strings.Replace(bf, edit.OldString, edit.NewString, 1)
	}

	// 原子发布。
	if err := atomicWrite(path, after); err != nil {
		return FsEditOutcome{}, fsErr(FSErrIO, "edit %q: %v", t.DisplayPath, err)
	}
	v, _ := versionOf(path)
	return FsEditOutcome{Version: v, Before: bf, After: after}, nil
}

// ObservedVersion 返回目标当前版本（供观察策略只读记录）。
func (l *LocalFS) ObservedVersion(t FsTarget) (FsVersion, bool) {
	if _, err := os.Stat(string(t.TargetKey)); err != nil {
		return "", false
	}
	v, _ := versionOf(string(t.TargetKey))
	return v, true
}

// atomicWrite 临时文件 + rename 原子发布。
func atomicWrite(path, content string) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".dsh-write-*")
	if err != nil {
		return err
	}
	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func countOccurrences(s, sub string) int {
	if sub == "" {
		return 0
	}
	return strings.Count(s, sub)
}

// normalizeLF 把换行规整为 LF（差异基准）。
func normalizeLF(s string) string {
	return strings.ReplaceAll(s, "\r\n", "\n")
}

func utf8Valid(b []byte) bool {
	return !strings.ContainsRune(string(b), '\uFFFD')
}