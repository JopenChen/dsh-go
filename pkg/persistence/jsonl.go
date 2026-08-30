// Package persistence 提供会话持久化接缝：Flush Checkpoint + Batch Window + Crash Repair。
//
// 对齐上游：packages/session/session-persistence + storage/jsonl
//
// 设计要点：
//   - Persistence 接口（locate/load/inspect/append/list/snapshot）统一屏蔽具体后端；
//   - JSONLBackend 将 SessionEvent 逐行追加写入文件，支持 Flush Checkpoint；
//   - Append 采用 batch window：事件先落内存缓冲区，满足批窗口或显式 checkpoint 时统一 flush，
//     减少文件 IO；
//   - 崩溃恢复（Crash Repair）：加载时若发现孤儿 turn/end（在断点处只写了 turn/start 等
//     出现未配对），自动补写 turn/end{reason:interrupted} 关闭，并保留已写入的 chunk/tool/call 事件。
package persistence

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/session"
)

// ============================================================================
// Persistence 接口
// ============================================================================

// Persistence 是会话持久化统一接口。
type Persistence interface {
	// Locate 返回会话文件的持久化路径。
	Locate(ctx context.Context, id brand.SessionID) (string, error)
	// Load 加载会话头部与全部事件。
	Load(ctx context.Context, id brand.SessionID) (*session.SessionHeader, []session.SessionEvent, error)
	// Append 追加一条事件（batch window 缓冲）。
	Append(ctx context.Context, id brand.SessionID, ev session.SessionEvent) error
	// Flush 强制将缓冲事件写入磁盘（checkpoint）。
	Flush(ctx context.Context, id brand.SessionID) error
	// Snapshot 返回指定会话当前事件数。
	Snapshot(ctx context.Context, id brand.SessionID) (int, error)
	// List 列出全部已持久化会话 ID。
	List(ctx context.Context) ([]brand.SessionID, error)
}

// ============================================================================
// JSONLBackend 实现
// ============================================================================

// JSONLBackend 是基于 JSONL 文件的持久化后端。
type JSONLBackend struct {
	dir string
	// batchSize 批窗口大小：缓冲达到该数量即自动 flush。
	batchSize int
	mu        sync.Mutex
	// buf 按会话缓冲未落盘事件。
	buf map[brand.SessionID][]session.SessionEvent
}

// NewJSONL 创建 JSONL 持久化后端（目录不存在则创建）。
func NewJSONL(dir string, batchSize int) (*JSONLBackend, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if batchSize <= 0 {
		batchSize = 1 // 默认每条即时写
	}
	return &JSONLBackend{dir: dir, batchSize: batchSize, buf: map[brand.SessionID][]session.SessionEvent{}}, nil
}

// sessionDir 返回某会话的存储目录。
func (j *JSONLBackend) sessionDir(id brand.SessionID) string {
	return filepath.Join(j.dir, id.Raw())
}

// headerPath / dataPath 分别是会话头部与事件文件的路径。
func (j *JSONLBackend) headerPath(id brand.SessionID) string {
	return filepath.Join(j.sessionDir(id), "header.json")
}
func (j *JSONLBackend) dataPath(id brand.SessionID) string {
	return filepath.Join(j.sessionDir(id), "events.jsonl")
}

// Locate 实现 Persistence。
func (j *JSONLBackend) Locate(ctx context.Context, id brand.SessionID) (string, error) {
	return j.sessionDir(id), nil
}

// SaveHeader 写入（或覆盖）会话头部。
func (j *JSONLBackend) SaveHeader(ctx context.Context, h *session.SessionHeader) error {
	dir := j.sessionDir(h.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := h.Marshal()
	if err != nil {
		return err
	}
	return os.WriteFile(j.headerPath(h.ID), data, 0o644)
}

// Append 实现 Persistence：事件进入批缓冲，达到窗口则 flush。
func (j *JSONLBackend) Append(ctx context.Context, id brand.SessionID, ev session.SessionEvent) error {
	j.mu.Lock()
	j.buf[id] = append(j.buf[id], ev)
	need := len(j.buf[id]) >= j.batchSize
	j.mu.Unlock()
	if need {
		return j.Flush(ctx, id)
	}
	return nil
}

// Flush 实现 Persistence：将某会话缓冲事件写入文件。
func (j *JSONLBackend) Flush(ctx context.Context, id brand.SessionID) error {
	j.mu.Lock()
	events := j.buf[id]
	delete(j.buf, id)
	j.mu.Unlock()

	if len(events) == 0 {
		return nil
	}

	dir := j.sessionDir(id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(j.dataPath(id), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	for _, ev := range events {
		b, err := json.Marshal(ev)
		if err != nil {
			return err
		}
		if _, err := w.Write(append(b, '\n')); err != nil {
			return err
		}
	}
	return w.Flush()
}

// FlushAll 强制 flush 全部会话缓冲（进程退出前调用）。
func (j *JSONLBackend) FlushAll(ctx context.Context) error {
	j.mu.Lock()
	ids := make([]brand.SessionID, 0, len(j.buf))
	for id := range j.buf {
		ids = append(ids, id)
	}
	j.mu.Unlock()
	for _, id := range ids {
		if err := j.Flush(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

// Load 实现 Persistence：读取头部 + 事件行，并执行崩溃修复。
func (j *JSONLBackend) Load(ctx context.Context, id brand.SessionID) (*session.SessionHeader, []session.SessionEvent, error) {
	// 读头部
	hData, err := os.ReadFile(j.headerPath(id))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, fmt.Errorf("persistence: session %q not found", id.Raw())
		}
		return nil, nil, err
	}
	header, err := session.UnmarshalSessionHeader(hData)
	if err != nil {
		return nil, nil, err
	}

	// 读事件行
	f, err := os.Open(j.dataPath(id))
	if err != nil {
		if os.IsNotExist(err) {
			return header, nil, nil
		}
		return nil, nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	var events []session.SessionEvent
	for scanner.Scan() {
		var ev session.SessionEvent
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			return nil, nil, fmt.Errorf("persistence: corrupt line: %w", err)
		}
		events = append(events, ev)
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, err
	}

	// 崩溃修复：若末尾存在未关闭 turn，补写 interrupted
	if repaired := repairOrphanTurn(&events); repaired > 0 {
		if err := j.rewrite(header, events); err != nil {
			return nil, nil, err
		}
	}
	return header, events, nil
}

// rewrite 重写会话文件（崩溃修复后落盘）。
func (j *JSONLBackend) rewrite(header *session.SessionHeader, events []session.SessionEvent) error {
	dir := j.sessionDir(header.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// 写头部
	hData, err := header.Marshal()
	if err != nil {
		return err
	}
	if err := os.WriteFile(j.headerPath(header.ID), hData, 0o644); err != nil {
		return err
	}
	// 写事件（覆盖）
	f, err := os.Create(j.dataPath(header.ID))
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	for _, ev := range events {
		b, err := json.Marshal(ev)
		if err != nil {
			return err
		}
		if _, err := w.Write(append(b, '\n')); err != nil {
			return err
		}
	}
	return w.Flush()
}

// Snapshot 实现 Persistence：返回会话事件数。
func (j *JSONLBackend) Snapshot(ctx context.Context, id brand.SessionID) (int, error) {
	_, events, err := j.Load(ctx, id)
	if err != nil {
		return 0, err
	}
	return len(events), nil
}

// List 实现 Persistence：列出全部会话目录。
func (j *JSONLBackend) List(ctx context.Context) ([]brand.SessionID, error) {
	entries, err := os.ReadDir(j.dir)
	if err != nil {
		return nil, err
	}
	var out []brand.SessionID
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, brand.NewSessionID(e.Name()))
		}
	}
	return out, nil
}

// repairOrphanTurn 检测末尾未关闭 turn 并补写 interrupted。返回补写数量。
func repairOrphanTurn(events *[]session.SessionEvent) int {
	if len(*events) == 0 {
		return 0
	}
	// 计算最终 turn 状态
	turnOpen := false
	for _, ev := range *events {
		switch ev.Type {
		case session.EventTurnStart:
			turnOpen = true
		case session.EventTurnEnd:
			turnOpen = false
		}
	}
	if !turnOpen {
		return 0
	}
	// 补一条 turn/end{reason:interrupted}
	last := (*events)[len(*events)-1]
	repaired := session.SessionEvent{
		Seq:  last.Seq + 1,
		Time: last.Time,
		Type: session.EventTurnEnd,
		Data: session.TurnEndData{Reason: session.ReasonInterrupted},
	}
	*events = append(*events, repaired)
	return 1
}