// publicname.go 复刻官方 mcp-client/tools 的公共名推导：MCP 工具的稳定身份是
// (server, raw)，模型面公共名为 mcp__<server>__<raw>；当字符替换或截断到函数名
// 约束（64 字符、[A-Za-z0-9_-]）时追加身份哈希，避免不同身份塌缩为同名。
package mcp

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
)

// MaxPublicNameLength 是函数名长度上限。
const MaxPublicNameLength = 64

const hashLength = 12

var invalidNameChars = regexp.MustCompile(`[^A-Za-z0-9_-]`)

// PublicToolName 由 (serverName, rawName) 确定性推导模型面公共名。
func PublicToolName(serverName, rawName string) string {
	joined := "mcp__" + serverName + "__" + rawName
	normalized := invalidNameChars.ReplaceAllString(joined, "_")
	if normalized == joined && len(normalized) <= MaxPublicNameLength {
		return normalized
	}
	sum := sha256.Sum256([]byte(serverName + "\x00" + rawName))
	hash := hex.EncodeToString(sum[:])[:hashLength]
	keep := MaxPublicNameLength - hashLength - 1
	if keep > len(normalized) {
		keep = len(normalized)
	}
	return normalized[:keep] + "_" + hash
}
