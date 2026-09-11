// Package uuid 提供 RFC 9562 v4 UUID 的生成，复刻官方 util/crypto 的 randomUUID：
// 从密码学随机源取 16 字节，固定 version/variant 位后格式化为 8-4-4-4-12。
package uuid

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// NewV4 生成一个随机 v4 UUID 字符串。
func NewV4() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	// version 4：byte 6 高半字节为 0100；variant 10：byte 8 高两比特为 10。
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b[:])
	return fmt.Sprintf("%s-%s-%s-%s-%s", h[0:8], h[8:12], h[12:16], h[16:20], h[20:32]), nil
}

// MustV4 生成 UUID，失败时 panic（仅用于不会失败的初始化场景）。
func MustV4() string {
	id, err := NewV4()
	if err != nil {
		panic(err)
	}
	return id
}
