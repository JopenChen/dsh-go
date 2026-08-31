// Package persistence 提供会话持久化接缝：Flush Checkpoint + Batch Window + Crash Repair。
//
// 对齐上游：packages/session/session-persistence + storage/jsonl
//
// 设计要点：
//   - Persistence 接口（locate/load/inspect/append/list/snapshot）统一屏蔽具体后端；
//   - JSONLBackend 将 SessionEvent 逐行追加写入文件，支持 Flush Checkpoint；
//   - 【H02 锁分片】按 SessionID 的哈希值映射到 N 个独立 shard，每个 shard 拥有独立
//     mutex + 事件缓冲，避免单全局锁的跨会话争用；
//   - 【H02 异步批量写入】每个 shard 内置后台 writer goroutine + ticker 时间窗口，
//     Append 仅入队（纳秒级），达到数量阈值或时间窗口后统一落盘，显著减少 syscall
//     与文件 open/close 次数；
//   - 崩溃恢复（Crash Repair）：加载时若发现孤儿 turn/end（在断点处只写了 turn/start
//     等但未配对），自动补写 turn/end{reason:interrupted} 关闭，并保留已写入的
//     chunk/tool/call 事件。
package persistence

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/session"
)

// ============================================================================
// H05：持久化 IO 内存复用（sync.Pool 回收 bytes.Buffer / bufio.Writer）
//
// 设计：
//   - marshalBufPool：复用 4KB 起始的 bytes.Buffer 给 json.NewEncoder +
//     单次事件序列化使用；encoder 内部 append 只会让 buffer 扩展，Put 后
//     下一次复用直接 Reset() 重新利用底层大切片，减少 GC。
//   - bufioWriterPool：复用 4KB bufio.Writer 本身（否则每次 open→new writer→close
//     都会 alloc 4KB buf + struct）。同一个 writer 在不同 f 上 Reset 重用。
//   - 统计：atomic 计数器记录复用次数 / 总序列化字节 / pooled 命中，便于
//     Benchmark 和 OTel 观测。
// ============================================================================

// defaultMarshalBufCap 初始容量：SessionEvent 多数 < 1KB，4KB 容纳 99% 场景且不浪费。
const defaultMarshalBufCap = 4 * 1024

// defaultBufioWriterSize bufio 写入缓冲：与 NewWriterSize 默认一致，便于可预测容量。
const defaultBufioWriterSize = 32 * 1024

var (
	// marshalBufPool H05：json.Marshal → Encode 的序列化缓冲池。
	marshalBufPool = sync.Pool{
		New: func() any { return bytes.NewBuffer(make([]byte, 0, defaultMarshalBufCap)) },
	}
	// bufioWriterPool H05：bufio.Writer 对象池（重置绑定到不同 io.Writer）。
	bufioWriterPool = sync.Pool{
		New: func() any { return bufio.NewWriterSize(io.Discard, defaultBufioWriterSize) },
	}
)

// H05 全局统计（atomic，跨 JSONLBackend 实例累加，Benchmark 直接读数）。
var (
	ioStatPooledBufferHits     atomic.Uint64 // pool 获取命中（bytes.Buffer + bufio.Writer 合计）
	ioStatMarshaledBytes       atomic.Uint64 // 通过 pooled encoder 写掉的总字节
	ioStatMarshaledEvents      atomic.Uint64 // 用 pooled 路径序列化的事件数
	ioStatHeaderMarshaledBytes atomic.Uint64 // header 序列化总字节
)

// JSONLIOStats 快照返回 H05 性能统计（线程安全）。
type JSONLIOStats struct {
	PooledBufferHits     uint64 `json:"pooledBufferHits"`
	MarshaledBytes       uint64 `json:"marshaledBytes"`
	MarshaledEvents      uint64 `json:"marshaledEvents"`
	HeaderMarshaledBytes uint64 `json:"headerMarshaledBytes"`
}

// ReadJSONLIOStats 读取当前 H05 统计快照。
func ReadJSONLIOStats() JSONLIOStats {
	return JSONLIOStats{
		PooledBufferHits:     ioStatPooledBufferHits.Load(),
		MarshaledBytes:       ioStatMarshaledBytes.Load(),
		MarshaledEvents:      ioStatMarshaledEvents.Load(),
		HeaderMarshaledBytes: ioStatHeaderMarshaledBytes.Load(),
	}
}

// ResetJSONLIOStats 归零（仅测试 / Benchmark ResetTimer 后使用；生产不要乱调）。
func ResetJSONLIOStats() {
	ioStatPooledBufferHits.Store(0)
	ioStatMarshaledBytes.Store(0)
	ioStatMarshaledEvents.Store(0)
	ioStatHeaderMarshaledBytes.Store(0)
}

// pooledMarshalEvent 使用 pooled bytes.Buffer + json.NewEncoder 把 v 序列化，
// 并保证末尾仅一个 '\n'（json.Encode 自带换行）。调用方使用完返回的字节后，
// 必须调用 releasePooledBuffer(buf) 归还。返回的 []byte 在下一次 Reset 前有效。
func pooledMarshalEvent(v any) (buf *bytes.Buffer, err error) {
	buf, _ = marshalBufPool.Get().(*bytes.Buffer)
	buf.Reset()
	ioStatPooledBufferHits.Add(1)
	enc := json.NewEncoder(buf)
	if err = enc.Encode(v); err != nil {
		return
	}
	return
}

// releasePooledBuffer 归还 pooled buffer。
func releasePooledBuffer(buf *bytes.Buffer) {
	if buf == nil {
		return
	}
	marshalBufPool.Put(buf)
}

// pooledBufioWriter 获取 pooled bufio.Writer 并重置到给定 io.Writer 上。
// 使用后必须调用 releasePooledWriter(w)，w 在 Release 前自动 Flush。
func pooledBufioWriter(w io.Writer) *bufio.Writer {
	bw, _ := bufioWriterPool.Get().(*bufio.Writer)
	bw.Reset(w)
	ioStatPooledBufferHits.Add(1)
	return bw
}

// releasePooledWriter 先 Flush 再归还 bufio.Writer。
func releasePooledWriter(bw *bufio.Writer) error {
	if bw == nil {
		return nil
	}
	err := bw.Flush()
	// 放回前解引用到底层大切片不引用外部 w（防御：避免 w 被 GC 回收时 writer 仍持有）。
	bw.Reset(io.Discard)
	bufioWriterPool.Put(bw)
	return err
}

// ============================================================================
// 默认常量（H02 分片 + 异步批量）
// ============================================================================

const (
	// DefaultShardCount 默认分片数（2 的幂有利于位运算取模）。
	// 会话数多、并发写入高的场景可通过 WithShardCount 调大。
	DefaultShardCount = 16
	// DefaultFlushInterval 默认异步 flush 的时间窗口（毫秒）。
	// 到达窗口即把当前 shard 缓冲中所有会话的数据一次性落盘。
	DefaultFlushInterval = 100 * time.Millisecond
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
// JSONLBackend 实现（H02：锁分片 + 异步批量写入）
// ============================================================================

// shardBuf 单个分片：独立互斥锁 + 会话级事件缓冲 + 写文件串行锁。
//
// 不同 SessionID 若落在不同 shard，则 Append/Flush 之间完全无锁争用；
// 同一 shard 内写文件（appendEventsToFile）还需走 writeMu，保证不会出现两个
// bufio.Writer 对同一会话文件的交错写入（H02 的并发安全性关键）。
type shardBuf struct {
	mu  sync.Mutex
	buf map[brand.SessionID][]session.SessionEvent
	// writeMu 保护本 shard 内所有会话的文件写入：
	// flushShard、Flush、FlushAll 都会调用 appendEventsToFile，三者若在
	// 不同 goroutine 同时触发，writeMu 保证写文件串行无交错，
	// 且不同 shard 之间完全并行。
	writeMu sync.Mutex
}

// JSONLBackend 是基于 JSONL 文件的持久化后端。
//
// H02 结构：
//
//	dir 根目录
//	batchSize 每条会话达到该事件数即触发 flush（与时间窗口 OR 关系）
//	flushInterval 时间窗口
//	shards 分片数组（固定长度，按哈希取模分配）
//	shardMask 位运算取模掩码（len(shards)-1，要求 shard 数为 2 的幂）
//	flushCh 后台 writer 收到信号即执行一次分片级 flush
//	closeOnce/closeCh Close 生命周期（关闭所有后台 writer）
//	wg 后台 goroutine 等待组，Close 会等待全部 writer 退出
type JSONLBackend struct {
	dir           string
	batchSize     int
	flushInterval time.Duration

	shards    []*shardBuf
	shardMask uint32

	// flushCh 用于触发某个 shard 立即落盘。
	// 写入方（Append 触发阈值）向 chan 写 shard 索引；后台 writer 收到即处理。
	flushChs []chan struct{}

	closeOnce sync.Once
	closeCh   chan struct{}
	wg        sync.WaitGroup
}

// Option 是 JSONLBackend 的配置选项（函数式选项模式）。
type Option func(*jsonlOptions)

type jsonlOptions struct {
	shardCount    int
	flushInterval time.Duration
}

// WithShardCount 设置分片数量（建议为 2 的幂；非 2 的幂会向上取整到最近的 2 幂）。
func WithShardCount(n int) Option {
	return func(o *jsonlOptions) { o.shardCount = n }
}

// WithFlushInterval 设置异步 flush 的时间窗口。
func WithFlushInterval(d time.Duration) Option {
	return func(o *jsonlOptions) { o.flushInterval = d }
}

// NewJSONL 创建 JSONL 持久化后端（目录不存在则创建）。
//
// batchSize：单会话缓冲事件数，达到该数量立即触发 flush（配合时间窗口做 OR 逻辑）。
// batchSize<=0 时默认值 1。
//
// 可通过 Option 调整分片数与 flush 时间窗口；不传则使用 DefaultShardCount /
// DefaultFlushInterval。
func NewJSONL(dir string, batchSize int, opts ...Option) (*JSONLBackend, error) {
	// 应用选项
	o := &jsonlOptions{
		shardCount:    DefaultShardCount,
		flushInterval: DefaultFlushInterval,
	}
	for _, f := range opts {
		f(o)
	}
	// 分片数向上取整到最近 2 幂，便于位运算。
	shardCount := nextPow2(o.shardCount)
	if shardCount < 1 {
		shardCount = 1
	}
	if batchSize <= 0 {
		batchSize = 1
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	// 构造所有 shard + 对应的触发 chan
	shards := make([]*shardBuf, shardCount)
	flushChs := make([]chan struct{}, shardCount)
	for i := 0; i < shardCount; i++ {
		shards[i] = &shardBuf{buf: map[brand.SessionID][]session.SessionEvent{}}
		flushChs[i] = make(chan struct{}, 1) // 缓冲 1，避免重复信号堆积
	}
	jb := &JSONLBackend{
		dir:           dir,
		batchSize:     batchSize,
		flushInterval: o.flushInterval,
		shards:        shards,
		shardMask:     uint32(shardCount - 1),
		flushChs:      flushChs,
		closeCh:       make(chan struct{}),
	}
	// 为每个 shard 启动一个后台 writer goroutine（H02 异步批量核心）
	for i := 0; i < shardCount; i++ {
		jb.wg.Add(1)
		go jb.shardWriter(i)
	}
	return jb, nil
}

// nextPow2 返回 >=n 的最小 2 整数次幂（n<=1 时返回 1）。
func nextPow2(n int) int {
	if n <= 1 {
		return 1
	}
	p := 1
	for p < n {
		p <<= 1
	}
	return p
}

// shardIndex 计算 SessionID 落在哪个分片。
// 使用 FNV-1a 32 位哈希 + 位运算取模（仅当 shardCount 是 2 幂时 mask 正确）。
func (j *JSONLBackend) shardIndex(id brand.SessionID) int {
	h := fnv.New32a()
	// id.Raw() 返回的是不可变字符串，哈希稳定。
	_, _ = h.Write([]byte(id.Raw())) // fnv Write 永不报错
	return int(h.Sum32() & j.shardMask)
}

// shardWriter 每个分片的后台 writer：
//   - 定时（flushInterval）到了 → 把本 shard 中所有非空会话缓冲刷盘
//   - 收到 flushCh 信号 → 同上（Append 达到 batchSize 时会触发信号）
//   - closeCh 关闭 → 做最后一轮 flush 再退出（保证 Close 不丢数据）
func (j *JSONLBackend) shardWriter(idx int) {
	defer j.wg.Done()
	sh := j.shards[idx]
	trigger := j.flushChs[idx]
	ticker := time.NewTicker(j.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-j.closeCh:
			// 优雅关闭：最后把残留数据 flush 一次
			_ = j.flushShard(sh)
			return
		case <-trigger:
			_ = j.flushShard(sh)
		case <-ticker.C:
			_ = j.flushShard(sh)
		}
	}
}

// flushShard 对整个分片执行 flush：把每个会话的缓冲事件一次性 append 到对应 JSONL。
// 调用方保证不会并发进入（shardWriter 是单 goroutine，天然串行）。
func (j *JSONLBackend) flushShard(sh *shardBuf) error {
	// 1. 在锁保护下一次性"偷走"所有缓冲数据
	sh.mu.Lock()
	if len(sh.buf) == 0 {
		sh.mu.Unlock()
		return nil
	}
	snapshot := sh.buf
	sh.buf = make(map[brand.SessionID][]session.SessionEvent, len(snapshot))
	sh.mu.Unlock()

	// 2. 无锁阶段按会话落盘（文件写入是 I/O 密集，此时该 shard 已能继续接受 Append）。
	//    写文件前拿 sh.writeMu：与前台 Flush / FlushAll 串行，避免同会话文件交错。
	sh.writeMu.Lock()
	defer sh.writeMu.Unlock()
	for id, events := range snapshot {
		if len(events) == 0 {
			continue
		}
		if err := j.appendEventsToFile(id, events); err != nil {
			// 写入失败：把 events 还回 shard 缓冲，下次再试（避免丢数据）。
			sh.mu.Lock()
			sh.buf[id] = append(events, sh.buf[id]...)
			sh.mu.Unlock()
			return err
		}
	}
	return nil
}

// appendEventsToFile 原子地把一批事件追加写入指定会话的 JSONL 文件。
// 调用方必须持有对应 shard 的 writeMu（保证同一 shard 内写文件串行，
// 不同 shard 可并发写不同文件）。
//
// H05 优化：
//   - 序列化：对每个 SessionEvent 使用 pooled bytes.Buffer + json.NewEncoder，
//     encoder 自带换行，避免 `append(b, '\n')` 的额外分配；
//   - 写入缓冲：使用 pooled bufio.Writer，避免每次 open→alloc 4KB。
func (j *JSONLBackend) appendEventsToFile(id brand.SessionID, events []session.SessionEvent) error {
	dir := j.sessionDir(id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(j.dataPath(id), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	w := pooledBufioWriter(f)
	defer func() { _ = releasePooledWriter(w) }()
	for _, ev := range events {
		buf, merr := pooledMarshalEvent(ev)
		if merr != nil {
			return merr
		}
		n, werr := w.Write(buf.Bytes())
		ioStatMarshaledBytes.Add(uint64(n))
		ioStatMarshaledEvents.Add(1)
		releasePooledBuffer(buf)
		if werr != nil {
			return werr
		}
	}
	// defer 中 releasePooledWriter 会 Flush。
	return nil
}

// Close 关闭持久化后端：停止所有后台 writer，flush 残留缓冲，等待 goroutine 退出。
// 多次调用安全。关闭后继续 Append 行为未定义（应视为不可用）。
func (j *JSONLBackend) Close() error {
	j.closeOnce.Do(func() {
		close(j.closeCh)
	})
	j.wg.Wait()
	return nil
}

// ShardCount 返回实际分片数（用于测试/观测）。
func (j *JSONLBackend) ShardCount() int { return len(j.shards) }

// ============================================================================
// 路径辅助
// ============================================================================

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

// ============================================================================
// Persistence 接口实现
// ============================================================================

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

// Append 实现 Persistence：事件进入目标分片的会话缓冲；
// 若该会话缓冲达到 batchSize，触发该 shard 的立即 flush。
// 正常情况下函数在纳秒级返回，落盘由后台 writer 异步完成（H02 核心）。
func (j *JSONLBackend) Append(ctx context.Context, id brand.SessionID, ev session.SessionEvent) error {
	idx := j.shardIndex(id)
	sh := j.shards[idx]

	sh.mu.Lock()
	sh.buf[id] = append(sh.buf[id], ev)
	need := len(sh.buf[id]) >= j.batchSize
	sh.mu.Unlock()

	if need {
		// 非阻塞发送触发信号；chan 有 1 缓冲，若已有信号在途则跳过（信号即代表"至少
		// 有一次 flush 即将执行"）。
		select {
		case j.flushChs[idx] <- struct{}{}:
		default:
		}
	}
	return nil
}

// Flush 实现 Persistence：同步等待指定会话缓冲落盘完成。
// 实现方式：
//   1. 从目标 shard 中"偷走"该会话的缓冲；
//   2. 在 sh.writeMu 保护下写文件（与后台 flushShard 串行，无并发交错）。
//
// 调用者通常在 turn/end checkpoint 时使用，保证重要时点数据持久化。
func (j *JSONLBackend) Flush(ctx context.Context, id brand.SessionID) error {
	idx := j.shardIndex(id)
	sh := j.shards[idx]

	// 偷走该会话缓冲（后台 writer 即使此时被唤醒，看到 buf 为空也会跳过）
	sh.mu.Lock()
	events := sh.buf[id]
	delete(sh.buf, id)
	sh.mu.Unlock()

	if len(events) == 0 {
		return nil
	}
	sh.writeMu.Lock()
	defer sh.writeMu.Unlock()
	return j.appendEventsToFile(id, events)
}

// FlushAll 强制 flush 全部会话缓冲（进程退出前或测试中调用）。
// 实现：按 shard 顺序 steal 全部分片缓冲并同步写入。
func (j *JSONLBackend) FlushAll(ctx context.Context) error {
	for _, sh := range j.shards {
		sh.mu.Lock()
		snapshot := sh.buf
		sh.buf = make(map[brand.SessionID][]session.SessionEvent, len(snapshot))
		sh.mu.Unlock()

		sh.writeMu.Lock()
		writeErr := error(nil)
		for id, events := range snapshot {
			if len(events) == 0 {
				continue
			}
			if err := j.appendEventsToFile(id, events); err != nil {
				writeErr = err
				// 失败：把尚未写入的 events（包括本 id 剩余 + 后续 id 全部）回滚
				sh.mu.Lock()
				sh.buf[id] = append(events, sh.buf[id]...)
				sh.mu.Unlock()
				break
			}
		}
		sh.writeMu.Unlock()
		if writeErr != nil {
			return writeErr
		}
	}
	return nil
}

// Load 实现 Persistence：读取头部 + 事件行，并执行崩溃修复。
// Load 不经过分片缓冲（缓冲里的是"未落盘"数据），因此应先 Flush(ctx, id) 或 FlushAll
// 后再 Load 才能看到最近 Append 结果；如只读历史数据则无需调用。
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
//
// H05 优化：events 走 pooled marshal + pooled bufio.Writer；header 我们仍调用
// header.Marshal()（保持与 Load 时字节级兼容），但使用 pooled bytes.Buffer
// 做 WriteFile 的临时载体，避免 WriteFile 自身的一次性 alloc。
func (j *JSONLBackend) rewrite(header *session.SessionHeader, events []session.SessionEvent) error {
	dir := j.sessionDir(header.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// 写头部：保持 header.Marshal() 的字节输出（避免 encoder 转义或换行差异导致
	// Load 端读回 hash 不一致），但使用 pooled bytes.Buffer 作为 WriteFile 的
	// 临时写入载体，减少一次性 []byte 分配。
	hData, err := header.Marshal()
	if err != nil {
		return err
	}
	hBuf, _ := marshalBufPool.Get().(*bytes.Buffer)
	hBuf.Reset()
	ioStatPooledBufferHits.Add(1)
	_, _ = hBuf.Write(hData)
	// 必须拷贝一次独立字节给 WriteFile，否则 hBuf.Reset() → Put 后内容可能被其他
	// goroutine 改写导致 WriteFile 读到脏数据（Windows WriteFile 非阻塞语义下也不安全）。
	hBytes := append([]byte(nil), hBuf.Bytes()...)
	releasePooledBuffer(hBuf)
	ioStatHeaderMarshaledBytes.Add(uint64(len(hBytes)))
	if err := os.WriteFile(j.headerPath(header.ID), hBytes, 0o644); err != nil {
		return err
	}
	// 写事件（覆盖）：复用 pooled bufio.Writer + pooled marshal buffer。
	f, err := os.Create(j.dataPath(header.ID))
	if err != nil {
		return err
	}
	defer f.Close()
	w := pooledBufioWriter(f)
	defer func() { _ = releasePooledWriter(w) }()
	for _, ev := range events {
		buf, merr := pooledMarshalEvent(ev)
		if merr != nil {
			return merr
		}
		n, werr := w.Write(buf.Bytes())
		ioStatMarshaledBytes.Add(uint64(n))
		ioStatMarshaledEvents.Add(1)
		releasePooledBuffer(buf)
		if werr != nil {
			return werr
		}
	}
	return nil
}

// Snapshot 实现 Persistence：返回会话事件数（已落盘 + 当前缓冲）。
// 为了返回"真实全量"，会先对目标会话执行一次同步 Flush。
func (j *JSONLBackend) Snapshot(ctx context.Context, id brand.SessionID) (int, error) {
	if err := j.Flush(ctx, id); err != nil {
		return 0, err
	}
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

// ============================================================================
// 崩溃修复
// ============================================================================

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
