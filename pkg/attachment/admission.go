// admission.go 复刻官方 attachment/admission 的上传准入：解码 base64 载荷时拒绝
// 非规范形式（解码后再编码必须与原串一致），防止同一字节的多种编码绕过校验。
package attachment

import (
	"encoding/base64"
	"errors"
)

// ErrInvalidBase64 载荷为空或不是规范 base64。
var ErrInvalidBase64 = errors.New("attachment: image upload is not canonical base64")

// DecodeCanonicalBase64 解码并校验规范 base64；非规范返回 ErrInvalidBase64。
func DecodeCanonicalBase64(data string) ([]byte, error) {
	if data == "" {
		return nil, ErrInvalidBase64
	}
	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return nil, ErrInvalidBase64
	}
	if base64.StdEncoding.EncodeToString(raw) != data {
		return nil, ErrInvalidBase64
	}
	return raw, nil
}
