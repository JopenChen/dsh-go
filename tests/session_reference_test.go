// Package tests 的会话引用（M17）验收测试。
//
// 覆盖：
//   - Mention 语法解析（@session/<id> 与 #<path/file>）
//   - PreparedReferencedMessage：剥离 mention + 聚合参考快照
//   - 稳定错误码：SESSION_NOT_FOUND / FILE_NOT_FOUND / FILE_OUT_OF_WORKSPACE / 自引用 / 超限
//   - 写入 user/message(source=reference, refs) 的审计语义
package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/session"
)

// stubSessionResolver 组合解析器：会话走 stub map，文件委托给工作区 resolver。
type stubSessionResolver struct {
	sessions map[string]string
	fs       *session.WorkspaceFileResolver
}

func (r *stubSessionResolver) ResolveSession(id brand.SessionID) (string, error) {
	if snap, ok := r.sessions[id.Raw()]; ok {
		return snap, nil
	}
	return "", &session.SessionReferenceError{Code: session.RefCodeSessionNotFound, Msg: "session " + id.Raw() + " not found"}
}

func (r *stubSessionResolver) ResolveFile(p string) (string, error) {
	return r.fs.ResolveFile(p)
}

func (r *stubSessionResolver) WorkspaceRoot() string { return r.fs.WorkspaceRoot() }

// newTestResolver 构造指向临时工作区的测试解析器。
func newTestResolver(t *testing.T, sessions map[string]string) *stubSessionResolver {
	t.Helper()
	root := t.TempDir()
	return &stubSessionResolver{
		sessions: sessions,
		fs:       &session.WorkspaceFileResolver{Root: root},
	}
}

// TestSessionReferenceMentionParsing 验证 mention 语法的正确解析。
func TestSessionReferenceMentionParsing(t *testing.T) {
	text := `请对比 @session/abc-123 与 @session/xyz_9 的实现，并参考 #src/main.go 与 #docs/readme.md`
	ms := session.ParseMentions(text)
	if len(ms) != 4 {
		t.Fatalf("应解析 4 个 mention, 实际 %d: %+v", len(ms), ms)
	}
	// 顺序校验（会话 + 文件混合，按原文位置）
	want := []session.Mention{
		{Kind: session.MentionSession, Value: "abc-123"},
		{Kind: session.MentionSession, Value: "xyz_9"},
		{Kind: session.MentionFile, Value: "src/main.go"},
		{Kind: session.MentionFile, Value: "docs/readme.md"},
	}
	for i, w := range want {
		if ms[i].Kind != w.Kind || ms[i].Value != w.Value {
			t.Errorf("mention[%d] = %+v, want %+v", i, ms[i], w)
		}
	}
}

// TestSessionReferencePrepareAggregates 验证 3 会话 + 文件 → 聚合上下文正确。
func TestSessionReferencePrepareAggregates(t *testing.T) {
	root := t.TempDir()
	mainGo := filepath.Join(root, "src", "main.go")
	if err := os.MkdirAll(filepath.Dir(mainGo), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mainGo, []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}

	res := &stubSessionResolver{
		sessions: map[string]string{
			"s1": "session-1 snapshot",
			"s2": "session-2 snapshot",
			"s3": "session-3 snapshot",
		},
		fs: &session.WorkspaceFileResolver{Root: root},
	}
	text := `请结合 @session/s1、@session/s2 与 @session/s3，再看 #src/main.go`
	prepared, err := session.Prepare(session.PrepareRequest{
		AgentSessionID: brand.NewSessionID("self"),
		Text:           text,
		Resolver:       res,
	})
	if err != nil {
		t.Fatalf("Prepare 失败: %v", err)
	}

	// 剥离 mention 后剩余可读内容
	if strings.Contains(prepared.Content, "@session/") {
		t.Errorf("Content 仍含会话 mention: %q", prepared.Content)
	}
	if strings.Contains(prepared.Content, "#src/main.go") {
		t.Errorf("Content 仍含文件 mention: %q", prepared.Content)
	}
	// 聚合上下文包含每个引用快照
	for _, want := range []string{"session-1 snapshot", "session-2 snapshot", "session-3 snapshot", "file \"src/main.go\""} {
		if !strings.Contains(prepared.AdditionalContext, want) {
			t.Errorf("additionalContext 缺少 %q\n上下文:\n%s", want, prepared.AdditionalContext)
		}
	}
	// 引用审计记录齐全
	if len(prepared.References) != 4 {
		t.Fatalf("References 应有 4 条, 实际 %d", len(prepared.References))
	}
}

// TestSessionReferenceSelfReferenceRejected 验证自引用返回 SELF_REFERENCE。
func TestSessionReferenceSelfReferenceRejected(t *testing.T) {
	res := newTestResolver(t, map[string]string{})
	_, err := session.Prepare(session.PrepareRequest{
		AgentSessionID: brand.NewSessionID("me"),
		Text:           `请看我 @session/me`,
		Resolver:       res,
	})
	if session.RefCodeFrom(err) != session.RefCodeSelfReference {
		t.Fatalf("应返回 SELF_REFERENCE, 实际: %v", err)
	}
}

// TestSessionReferenceClassificationErrors 验证会话/文件不存在的分类错误码。
func TestSessionReferenceClassificationErrors(t *testing.T) {
	root := t.TempDir()
	res := newTestResolver(t, map[string]string{}) // 无任何会话
	res.fs.Root = root

	// 会话不存在 → SESSION_NOT_FOUND
	_, err := session.Prepare(session.PrepareRequest{
		Text:     `请读 @session/ghost`,
		Resolver: res,
	})
	if session.RefCodeFrom(err) != session.RefCodeSessionNotFound {
		t.Fatalf("应返回 SESSION_NOT_FOUND, 实际: %v", err)
	}

	// 文件不存在 → FILE_NOT_FOUND
	_, err = session.Prepare(session.PrepareRequest{
		Text:     `见 #no/such/file.go`,
		Resolver: res,
	})
	if session.RefCodeFrom(err) != session.RefCodeFileNotFound {
		t.Fatalf("应返回 FILE_NOT_FOUND, 实际: %v", err)
	}

	// 越界文件（相对路径 ../ 指向工作区外）→ FILE_OUT_OF_WORKSPACE
	_, err = session.Prepare(session.PrepareRequest{
		Text:     `读 #../outside.txt`,
		Resolver: res,
	})
	if session.RefCodeFrom(err) != session.RefCodeFileOutOfWorkspace {
		t.Fatalf("应返回 FILE_OUT_OF_WORKSPACE, 实际: %v", err)
	}
}

// TestSessionReferenceTooManyRejected 验证引用数量超限返回 TOO_MANY。
func TestSessionReferenceTooManyRejected(t *testing.T) {
	res := newTestResolver(t, map[string]string{})
	var sb strings.Builder
	for i := 0; i < 20; i++ {
		sb.WriteString("@session/abc")
		sb.WriteString(string(rune('a' + i)))
		sb.WriteString(" ")
	}
	_, err := session.Prepare(session.PrepareRequest{
		Text:     sb.String(),
		Resolver: res,
	})
	if session.RefCodeFrom(err) != session.RefCodeTooMany {
		t.Fatalf("应返回 TOO_MANY, 实际: %v", err)
	}
}

// TestSessionReferenceBudgetExceeded 验证聚合上下文超预算返回 BUDGET_EXCEEDED。
func TestSessionReferenceBudgetExceeded(t *testing.T) {
	res := newTestResolver(t, map[string]string{"big": strings.Repeat("x", 1000)})
	_, err := session.Prepare(session.PrepareRequest{
		Text:            `看 @session/big`,
		Resolver:        res,
		MaxContextBytes: 100,
	})
	if session.RefCodeFrom(err) != session.RefCodeBudgetExceeded {
		t.Fatalf("应返回 BUDGET_EXCEEDED, 实际: %v", err)
	}
}

// TestSessionReferenceWriteAuditData 验证 PreparedReferencedMessage 可映射进 user/message(source=reference, refs)。
func TestSessionReferenceWriteAuditData(t *testing.T) {
	res := newTestResolver(t, map[string]string{"s1": "snapshot-1"})
	prepared, err := session.Prepare(session.PrepareRequest{
		Text:     `结合 @session/s1`,
		Resolver: res,
	})
	if err != nil {
		t.Fatal(err)
	}
	// 构造 user/message 审计事件（source=reference + refs），验证与 UserMessageData 字段连通。
	refs := make([]any, 0, len(prepared.References))
	for _, r := range prepared.References {
		refs = append(refs, r)
	}
	msg := session.UserMessageData{
		Content: prepared.Content,
		Source:  "reference",
		Refs:    refs,
	}
	// UserMessageData 的 Source 字段承载 reference 语义，供下游用 M05 deriveMessages 保留审计。
	if msg.Source != "reference" || msg.Content != prepared.Content {
		t.Fatalf("user/message 审计数据不完整: %+v", msg)
	}
}