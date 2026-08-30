// 本文件对应任务 M44：SessionHeader 格式拒绝 & 事件类型白名单校验。
//
// 对齐上游：packages/core/session format version
//
// 设计要点：
//   - LoadSession 是外部加载会话的唯一入口：先校验 Header 版本（fail-closed，拒绝未知版本），
//     再校验每条事件类型必须属于 KNOWN 集合（fail-closed，拒绝未知事件类型）；
//   - 不同格式版本（VERSION）的会话文件互相拒绝，避免用错误解析逻辑损坏数据。
package session

import (
	"encoding/json"
	"fmt"
)

// KNOWN_SESSION_EVENT_TYPES 是受支持的全部事件类型集合（字典序，用于白名单校验）。
var KNOWN_SESSION_EVENT_TYPES = func() map[EventType]struct{} {
	m := map[EventType]struct{}{}
	for _, t := range AllEventTypes {
		m[t] = struct{}{}
	}
	return m
}()

// CorruptEventError 表示事件流中存在无法识别的类型（数据损坏或版本不兼容）。
type CorruptEventError struct {
	Seq  uint64
	Type EventType
}

// Error 实现 error 接口。
func (e *CorruptEventError) Error() string {
	return fmt.Sprintf("session: corrupt event seq %d with unknown type %q", e.Seq, e.Type)
}

// IsCorruptEventError 判断是否为损坏事件错误。
func IsCorruptEventError(err error) bool {
	_, ok := err.(*CorruptEventError)
	return ok
}

// LoadSession 加载会话头部并根据纳旧数据重建事件列表。
// 校验：
//   - header 版本必须等于 SessionFormatVersion（cross-version 拒绝）；
//   - 每条事件的 type 必须属于 KNOWN_SESSION_EVENT_TYPES。
//
// 返回事件列表，其中每条已通过 round-trip 反序列化验证。
func LoadSession(headerBytes []byte, eventLines ...[]byte) (*SessionHeader, []SessionEvent, error) {
	header, err := UnmarshalSessionHeader(headerBytes)
	if err != nil {
		return nil, nil, err
	}
	if err := header.Validate(); err != nil {
		return nil, nil, err
	}

	events := make([]SessionEvent, 0, len(eventLines))
	for _, line := range eventLines {
		// 先解析最小信封（seq + type），做白名单校验（fail-closed）
		var probe struct {
			Seq  uint64    `json:"seq"`
			Type EventType `json:"type"`
		}
		if err := json.Unmarshal(line, &probe); err != nil {
			return nil, nil, err
		}
		if _, known := KNOWN_SESSION_EVENT_TYPES[probe.Type]; !known {
			return nil, nil, &CorruptEventError{Seq: probe.Seq, Type: probe.Type}
		}
		// 再做完整 round-trip 反序列化
		var ev SessionEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			return nil, nil, err
		}
		events = append(events, ev)
	}
	return header, events, nil
}