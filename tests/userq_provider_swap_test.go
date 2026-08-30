// 本文件对应任务 M14：User Questions 接缝。
package tests

import (
	"context"
	"testing"

	"github.com/JopenChen/dsh-go/pkg/userq"
)

// mockProvider 是自定义 provider（可编程返回）。
type mockProvider struct {
	idx      int
	custom   string
	callOpts userq.QuestionOptions
}

func (m *mockProvider) Ask(ctx context.Context, opts userq.QuestionOptions) (*userq.QuestionResult, error) {
	m.callOpts = opts
	return &userq.QuestionResult{SelectedIndex: m.idx, Custom: m.custom, SelectedIndices: []int{m.idx}}, nil
}

// TestUserQStubDefault 验证 stub 返回默认选项。
func TestUserQStubDefault(t *testing.T) {
	svc := userq.New(userq.NewStub())
	ctx := context.Background()

	idx, custom, err := svc.Ask(ctx, userq.QuestionOptions{
		Prompt:    "是否继续？",
		Choices:   []string{"是", "否"},
		Intent:    "plan-exit",
	})
	if err != nil {
		t.Fatalf("Ask 失败: %v", err)
	}
	if idx != 0 {
		t.Fatalf("stub 默认选项 idx = %d, want 0", idx)
	}
	if custom != "" {
		t.Fatalf("stub custom 应为空: %q", custom)
	}
}

// TestUserQProviderSwap 验证替换 provider 后 Ask 路由到自定义实现。
func TestUserQProviderSwap(t *testing.T) {
	svc := userq.New(userq.NewStub())

	// 替换为自定义 mock
	mp := &mockProvider{idx: 2, custom: "自定义输入"}
	svc.SetProvider(mp)

	opts := userq.QuestionOptions{Prompt: "选择", Choices: []string{"a", "b", "c"}, Intent: "approval"}
	idx, custom, err := svc.Ask(context.Background(), opts)
	if err != nil {
		t.Fatalf("Ask 失败: %v", err)
	}
	if idx != 2 || custom != "自定义输入" {
		t.Fatalf("应路由到自定义实现: idx=%d custom=%q", idx, custom)
	}
	// 验证 intent 标签透传
	if mp.callOpts.Intent != "approval" || mp.callOpts.MultiSelect {
		t.Fatalf("intent 透传异常: %+v", mp.callOpts)
	}
}

// TestUserQMultiSelect 验证多选语义。
func TestUserQMultiSelect(t *testing.T) {
	svc := userq.New(userq.NewStub())
	ctx := context.Background()

	res, err := svc.AskDetailed(ctx, userq.QuestionOptions{
		Prompt:      "多选",
		Choices:     []string{"a", "b", "c"},
		MultiSelect: true,
	})
	if err != nil {
		t.Fatalf("AskDetailed 失败: %v", err)
	}
	if len(res.SelectedIndices) != 3 {
		t.Fatalf("多选应返回全部下标: %v", res.SelectedIndices)
	}
}

// TestUserQNoProvider 验证未安装 provider 时报错。
func TestUserQNoProvider(t *testing.T) {
	svc := userq.New(nil)
	if _, _, err := svc.Ask(context.Background(), userq.QuestionOptions{}); err == nil {
		t.Fatal("无 provider 应报错")
	}
}