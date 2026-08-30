// 本文件实现 filekv 后端：基于文件系统的键值存储。
package storage

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// FileKV 是基于文件系统的 Backend 实现。
// 每个 key 对应 <root>/<key> 文件，内容为 {"version":N,"data":"<base64>"}。
type FileKV struct {
	root string
}

// NewFileKV 创建文件后端，root 为存储根目录（不存在则创建）。
func NewFileKV(root string) (*FileKV, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	return &FileKV{root: root}, nil
}

// fileRecord 是磁盘上的单条记录。
type fileRecord struct {
	Version uint64 `json:"version"`
	Data    string `json:"data"` // base64
}

// keyPath 将 key 映射为安全文件路径（防目录穿越）。
func (f *FileKV) keyPath(key string) string {
	// 将 key 中的分隔符替换，防止 "../" 穿越
	safe := strings.ReplaceAll(key, "..", "_")
	return filepath.Join(f.root, filepath.FromSlash(safe))
}

// Get 实现 Backend。
func (f *FileKV) Get(ctx context.Context, key string) ([]byte, uint64, error) {
	path := f.keyPath(key)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, &ErrKeyNotFound{Key: key}
		}
		return nil, 0, err
	}
	var rec fileRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return nil, 0, err
	}
	data, err := base64.StdEncoding.DecodeString(rec.Data)
	if err != nil {
		return nil, 0, err
	}
	return data, rec.Version, nil
}

// Put 实现 Backend（CAS 语义）。
func (f *FileKV) Put(ctx context.Context, key string, data []byte, expectedVersion uint64) (uint64, error) {
	path := f.keyPath(key)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return 0, err
	}

	current, currentVersion, err := f.Get(ctx, key)
	if err != nil {
		if !IsKeyNotFound(err) {
			return 0, err
		}
		// key 不存在：仅允许 expectedVersion=0（新建）
		if expectedVersion != 0 {
			return 0, &ErrCASMismatch{Key: key, ExpectedVersion: expectedVersion, ActualVersion: 0}
		}
	} else {
		_ = current
		if currentVersion != expectedVersion {
			return 0, &ErrCASMismatch{Key: key, ExpectedVersion: expectedVersion, ActualVersion: currentVersion}
		}
	}

	rec := fileRecord{Version: expectedVersion + 1, Data: base64.StdEncoding.EncodeToString(data)}
	raw, err := json.Marshal(rec)
	if err != nil {
		return 0, err
	}
	// 原子写：先写临时文件再重命名，避免半写状态
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return 0, err
	}
	if err := os.Rename(tmp, path); err != nil {
		return 0, err
	}
	return expectedVersion + 1, nil
}

// Delete 实现 Backend。
func (f *FileKV) Delete(ctx context.Context, key string) error {
	path := f.keyPath(key)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// List 实现 Backend：返回根目录下所有 key（相对路径，保持 key 分隔符）。
func (f *FileKV) List(ctx context.Context, prefix string) ([]string, error) {
	var keys []string
	err := filepath.Walk(f.root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".tmp") {
			return nil
		}
		rel, err := filepath.Rel(f.root, path)
		if err != nil {
			return err
		}
		key := filepath.ToSlash(rel)
		if prefix == "" || strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
		return nil
	})
	return keys, err
}
