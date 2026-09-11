// uri.go 复刻官方 context/session-reference 的会话引用 URI：把不透明 session id
// 无损编码为 dsh-session: URI、解码并校验规范化，以及 Markdown mention 渲染。
package sessionquery

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
)

// Scheme 是会话快照引用的保留 URI scheme。
const Scheme = "dsh-session:"

var enc = base64.RawURLEncoding

var payloadShape = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// ErrInvalidURI 会话引用 URI 非法或不规范。
var ErrInvalidURI = errors.New("sessionquery: invalid session reference URI")

// EncodeURI 把 session id 无损编码为规范 URI。
func EncodeURI(sessionID string) string {
	raw, _ := json.Marshal(sessionID)
	return Scheme + enc.EncodeToString(raw)
}

// DecodeURI 解码并校验 URI 规范；非法返回 ErrInvalidURI。
func DecodeURI(uri string) (string, error) {
	if !strings.HasPrefix(uri, Scheme) {
		return "", ErrInvalidURI
	}
	payload := uri[len(Scheme):]
	if !payloadShape.MatchString(payload) {
		return "", ErrInvalidURI
	}
	raw, err := enc.DecodeString(payload)
	if err != nil {
		return "", ErrInvalidURI
	}
	var id string
	if err := json.Unmarshal(raw, &id); err != nil {
		return "", ErrInvalidURI
	}
	if EncodeURI(id) != uri {
		return "", ErrInvalidURI
	}
	return id, nil
}

var escapeLabel = regexp.MustCompile(`[\\\]]`)

// FormatMention 渲染携带规范 URI 的 Markdown mention；label 为空时用 id 本身。
func FormatMention(sessionID, label string) string {
	if label == "" {
		label = sessionID
	}
	label = escapeLabel.ReplaceAllStringFunc(label, func(m string) string { return `\` + m })
	return "@[" + label + "](" + EncodeURI(sessionID) + ")"
}
