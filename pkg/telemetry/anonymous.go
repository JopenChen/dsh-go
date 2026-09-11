// anonymous.go 复刻官方 identity/anonymous-user-id：每个 Harness home 一个随机
// UUID，持久化为 .anonymous-user-id 单行；文件缺失或损坏则新建，写入失败仍返回
// 可用 ID（best-effort），进程内按路径 memo。
package telemetry

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/JopenChen/dsh-go/pkg/homepath"
	"github.com/JopenChen/dsh-go/pkg/uuid"
)

// AnonymousIDFileName 是存放匿名 ID 的文件名。
const AnonymousIDFileName = ".anonymous-user-id"

var (
	anonMu     sync.Mutex
	anonMemo   = map[string]string{}
	uuidPattern = func(s string) bool {
		parts := strings.Split(s, "-")
		if len(parts) != 5 {
			return false
		}
		lens := []int{8, 4, 4, 4, 12}
		for i, p := range parts {
			if len(p) != lens[i] {
				return false
			}
			for _, c := range p {
				if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
					return false
				}
			}
		}
		return true
	}
)

// GetOrCreateAnonymousID 返回 Harness home 下的匿名 ID，必要时创建并持久化。
func GetOrCreateAnonymousID(env map[string]string) (string, error) {
	home, err := homepath.Resolve("", env)
	if err != nil {
		return "", err
	}
	file := filepath.Join(home, AnonymousIDFileName)

	anonMu.Lock()
	defer anonMu.Unlock()
	if cached, ok := anonMemo[file]; ok {
		return cached, nil
	}

	if raw, err := os.ReadFile(file); err == nil {
		if v := strings.TrimSpace(string(raw)); uuidPattern(v) {
			anonMemo[file] = v
			return v, nil
		}
	}

	id, err := uuid.NewV4()
	if err != nil {
		return "", err
	}
	// best-effort 持久化：目录与写入失败都不阻断本次使用。
	if err := os.MkdirAll(home, 0o700); err == nil {
		_ = os.WriteFile(file, []byte(id+"\n"), 0o600)
	}
	anonMemo[file] = id
	return id, nil
}
