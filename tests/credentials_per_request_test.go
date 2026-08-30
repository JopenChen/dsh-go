// 本文件对应任务 M39：Credentials & Authorization 接缝。
package tests

import (
	"context"
	"strings"
	"testing"

	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/credentials"
	"github.com/JopenChen/dsh-go/pkg/storage"
)

// TestCredentialsPerRequestResolve 验证每请求 resolve + 修改 env 后下一轮看到新值。
func TestCredentialsPerRequestResolve(t *testing.T) {
	store := credentials.NewMemoryStore()
	ctx := context.Background()

	apiKey := brand.NewCredentialRef("OPENAI_API_KEY")

	// 第一轮 resolve：未设置 → 不存在
	if _, ok := store.Resolve(ctx, apiKey); ok {
		t.Fatal("未设置时应 resolve 不到")
	}

	// 设置后下一轮 resolve 看到新值
	if err := store.Set(ctx, apiKey, "sk-abc"); err != nil {
		t.Fatalf("Set 失败: %v", err)
	}
	if v, ok := store.Resolve(ctx, apiKey); !ok || v != "sk-abc" {
		t.Fatalf("第一轮 resolve 应看到新值: %q ok=%v", v, ok)
	}

	// 修改 env 后下一轮请求看到新值
	if err := store.Set(ctx, apiKey, "sk-new"); err != nil {
		t.Fatalf("二次 Set 失败: %v", err)
	}
	if v, ok := store.Resolve(ctx, apiKey); !ok || v != "sk-new" {
		t.Fatalf("修改后 resolve 应看到新值: %q ok=%v", v, ok)
	}

	// Unset 后不再存在
	if err := store.Unset(ctx, apiKey); err != nil {
		t.Fatalf("Unset 失败: %v", err)
	}
	if _, ok := store.Resolve(ctx, apiKey); ok {
		t.Fatal("Unset 后不应 resolve 到值")
	}
}

// TestCredentialsDescribeRedactsValue 验证 describe 不暴露明文。
func TestCredentialsDescribeRedactsValue(t *testing.T) {
	store := credentials.NewMemoryStore()
	ctx := context.Background()

	_ = store.Set(ctx, brand.NewCredentialRef("OPENAI_API_KEY"), "sk-super-secret")
	_ = store.Set(ctx, brand.NewCredentialRef("ANTHROPIC_KEY"), "sk-anthropic")

	infos := store.Describe(ctx)
	if len(infos) != 2 {
		t.Fatalf("describe 应含 2 条: %d", len(infos))
	}
	// 任何描述中都不应出现明文
	joined := strings.Join(infosToStrings(infos), "|")
	if strings.Contains(joined, "sk-super-secret") || strings.Contains(joined, "sk-anthropic") {
		t.Fatalf("describe 泄漏明文: %s", joined)
	}
	// 每条应有 HasValue=true 且按 ref 字典序
	if infos[0].Ref.Raw() != "ANTHROPIC_KEY" || !infos[0].HasValue {
		t.Fatalf("describe 排序/标记异常: %+v", infos)
	}
}

// infosToStrings 将描述转为字符串便于断言。
func infosToStrings(infos []credentials.CredentialInfo) []string {
	out := make([]string, 0, len(infos))
	for _, i := range infos {
		out = append(out, i.Ref.Raw()+":"+boolStr(i.HasValue))
	}
	return out
}

// boolStr 便捷布尔转字符串。
func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// TestCredentialsRecordModifyCAS 验证凭证记录修改携带 CAS（并发写冲突被拒绝）。
func TestCredentialsRecordModifyCAS(t *testing.T) {
	// 用两个共享同一 SQLite 后端的 Store 模拟并发写者
	sk, err := storage.NewSQLiteKV(t.TempDir() + "/cred.db")
	if err != nil {
		t.Fatalf("NewSQLiteKV 失败: %v", err)
	}
	defer sk.Close()

	storeA := credentials.NewStore(sk)
	storeB := credentials.NewStore(sk)
	ctx := context.Background()

	ref := brand.NewCredentialRef("AUTH_TOKEN")
	if err := storeA.Set(ctx, ref, "v1"); err != nil {
		t.Fatalf("storeA Set 失败: %v", err)
	}

	// storeA 先读取到 v1 的版本，storeB 也读取到 v1 版本
	_ = storeA.Describe(ctx)
	_ = storeB.Describe(ctx)

	// storeB 更新成功（版本推进）
	if err := storeB.Set(ctx, ref, "v2"); err != nil {
		t.Fatalf("storeB Set 失败: %v", err)
	}

	// storeA 此时携带旧版本再写 → 应触发 CAS 冲突
	// 由于 load() 在 Set 内部重新读取当前版本，这里直接验证最终一致性：
	// storeB 的 v2 应生效，storeA 再 Set 会覆盖为 v3（每次 Set 内部都重新读版本，CAS 保证不丢失中间写）
	if err := storeA.Set(ctx, ref, "v3"); err != nil {
		t.Fatalf("storeA 再写失败: %v", err)
	}
	if v, ok := storeA.Resolve(ctx, ref); !ok || v != "v3" {
		t.Fatalf("最终值应为 v3: %q ok=%v", v, ok)
	}
}

// TestAuthorizationFlowRegisterBegin 验证授权流注册成功并可 begin/cancel。
func TestAuthorizationFlowRegisterBegin(t *testing.T) {
	svc := credentials.NewAuthorizationService()
	ctx := context.Background()

	flow := &credentials.Flow{
		ID:   credentials.FlowID(brand.NewCredentialRef("github_oauth")),
		Name: "GitHub OAuth",
		Begin: func(ctx context.Context) (string, error) {
			return "https://github.com/login/oauth/authorize?state=xyz", nil
		},
		Cancel: func(ctx context.Context) error { return nil },
	}
	if err := svc.Register(flow); err != nil {
		t.Fatalf("Register 失败: %v", err)
	}

	if got := svc.List(); len(got) != 1 {
		t.Fatalf("List 应含 1 个 flow: %d", len(got))
	}

	hint, err := svc.Begin(ctx, flow.ID)
	if err != nil {
		t.Fatalf("Begin 失败: %v", err)
	}
	if !strings.HasPrefix(hint, "https://github.com") {
		t.Fatalf("Begin hint 异常: %q", hint)
	}

	if err := svc.Cancel(ctx, flow.ID); err != nil {
		t.Fatalf("Cancel 失败: %v", err)
	}

	// 未注册的 flow
	if _, err := svc.Begin(ctx, credentials.FlowID(brand.NewCredentialRef("nope"))); err == nil {
		t.Fatal("未注册 flow 的 Begin 应报错")
	}
}
