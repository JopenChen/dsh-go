// arguments.go 复刻官方 agent-loop 的 parseArguments：空输入映射为空对象，
// 合法 JSON 解析为对应值，非法 JSON 不报错而是原样保留为字符串（交给工具层
// 自行处理/报错），避免在调度边界因模型输出的格式问题直接中断整轮。
package tools

import "encoding/json"

// ParseArguments 解析模型给出的原始参数字符串。
func ParseArguments(raw string) any {
	if raw == "" {
		return map[string]any{}
	}
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return raw
	}
	return v
}
