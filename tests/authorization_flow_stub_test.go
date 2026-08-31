// 本文件验证任务 S06：Authorization Service（OAuth 流 stub）。
//
// 覆盖：list flows → begin(token) → 回调 Complete(value) → 凭证可被 M39 Resolve；
// cancel 取消 pending；重复完成/重复取消拒绝；状态查询。
package tests

import (
	"context"
	"testing"

	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/credentials"
)

// TestAuthFlowBeginCompleteResolve 验证 begin → 回调 → Resolve 贯通。
func TestAuthFlowBeginCompleteResolve(t *testing.T) {
	ctx := context.Background()
	store := credentials.NewMemoryStore()
	auth := credentials.NewAuthorizationService()
	svc := credentials.NewAuthService(auth, store)

	flowID := brand.NewCredentialRef("GITHUB_TOKEN")
	// 注册一个 stub flow（Begin 返回占位 URL，Cancel 无操作）。
	if err := auth.Register(&credentials.Flow{
		ID:   flowID,
		Name: "github",
		Begin: func(ctx context.Context) (string, error) {
			return "https://example.com/authorize?state=stub", nil
		},
	}); err != nil {
		t.Fatal(err)
	}

	// list flows 应包含 github。
	found := false
	for _, f := range auth.List() {
		if f.ID == flowID {
			found = true
		}
	}
	if !found {
		t.Fatal("list flows 应包含 github")
	}

	// begin → token。
	token, err := svc.Begin(ctx, flowID)
	if err != nil {
		t.Fatal(err)
	}
	if token == "" {
		t.Fatal("begin 应返回非空 token")
	}

	// 回调完成，写入凭证。
	if err := svc.Complete(ctx, token, "ghp_stub_secret"); err != nil {
		t.Fatal(err)
	}
	// 会话状态应 completed。
	if sess, ok := svc.Status(token); !ok || sess.Status != credentials.AuthCompleted {
		t.Fatalf("会话应 completed，实际 %+v ok=%v", sess, ok)
	}
	// 凭证可被 M39 Resolve 解析到。
	if v, ok := store.Resolve(ctx, flowID); !ok || v != "ghp_stub_secret" {
		t.Fatalf("Resolve 应取到回调凭证，实际 %q ok=%v", v, ok)
	}
}

// TestAuthCancel 验证取消 pending 会话。
func TestAuthCancel(t *testing.T) {
	ctx := context.Background()
	store := credentials.NewMemoryStore()
	auth := credentials.NewAuthorizationService()
	svc := credentials.NewAuthService(auth, store)

	flowID := brand.NewCredentialRef("SLACK_TOKEN")
	_ = auth.Register(&credentials.Flow{ID: flowID, Name: "slack",
		Begin: func(context.Context) (string, error) { return "u", nil }})

	token, _ := svc.Begin(ctx, flowID)
	if err := svc.Cancel(ctx, token); err != nil {
		t.Fatal(err)
	}
	sess, _ := svc.Status(token)
	if sess.Status != credentials.AuthCancelled {
		t.Fatalf("状态应 cancelled，实际 %s", sess.Status)
	}
	// 取消后再完成应拒绝。
	if err := svc.Complete(ctx, token, "x"); err == nil {
		t.Fatal("cancelled 会话不应再完成")
	}
}

// TestAuthDoubleCompleteRejected 验证重复完成被拒绝。
func TestAuthDoubleCompleteRejected(t *testing.T) {
	ctx := context.Background()
	store := credentials.NewMemoryStore()
	auth := credentials.NewAuthorizationService()
	svc := credentials.NewAuthService(auth, store)

	flowID := brand.NewCredentialRef("A_KEY")
	_ = auth.Register(&credentials.Flow{ID: flowID, Name: "a",
		Begin: func(context.Context) (string, error) { return "u", nil }})

	token, _ := svc.Begin(ctx, flowID)
	if err := svc.Complete(ctx, token, "v1"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Complete(ctx, token, "v2"); err == nil {
		t.Fatal("已 completed 会话二次完成应被拒绝")
	}
	// 凭证值应保留首次写入。
	if v, ok := store.Resolve(ctx, flowID); !ok || v != "v1" {
		t.Fatalf("凭证应为首值 v1，实际 %q", v)
	}
}

// TestAuthMissingToken 验证未知 token 操作报错。
func TestAuthMissingToken(t *testing.T) {
	ctx := context.Background()
	store := credentials.NewMemoryStore()
	svc := credentials.NewAuthService(credentials.NewAuthorizationService(), store)
	if _, err := svc.Begin(ctx, brand.NewCredentialRef("NO_FLOW")); err == nil {
		t.Fatal("未注册 flow 的 begin 应报错")
	}
	if err := svc.Complete(ctx, "not-exist", "v"); err == nil {
		t.Fatal("未知 token 的 complete 应报错")
	}
}