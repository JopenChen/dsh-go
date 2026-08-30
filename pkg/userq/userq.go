// Package userq 提供 User Questions（用户提问）接缝。
//
// 对齐上游：packages/interaction/user-questions
//
// 设计要点：
//   - UQ 服务接口 Ask(ctx, options) → (idx, custom, err)：向用户提问并收集选择/自定义输入；
//   - provider_stub 实现同步阻塞返回默认选项（测试用）；生产由 SDK 注入真实 UI/回调实现；
//   - 支持多选（multiSelect）与意图标签（intent），决策逻辑只与接口耦合，可无缝替换 provider。
package userq

import (
	"context"
	"fmt"
)

// QuestionOptions 描述一次提问的选项。
type QuestionOptions struct {
	// Prompt 向用户展示的问题文本。
	Prompt string
	// Choices 候选选项列表。
	Choices []string
	// DefaultIndex 默认选中项下标（-1 表示无默认）。
	DefaultIndex int
	// MultiSelect 是否允许多选（此时返回逗号分隔的选项下标）。
	MultiSelect bool
	// Intent 意图标签（如 "approval" / "plan-exit" / "goal-blocker"）。
	Intent string
}

// QuestionResult 是提问结果。
type QuestionResult struct {
	// SelectedIndex 选中的选项下标（多选时为第一个）。
	SelectedIndex int
	// Custom 用户自定义输入（选择"other/custom"时）。
	Custom string
	// SelectedIndices 多选时的全部下标。
	SelectedIndices []int
}

// Provider 是用户提问的后端接口。
type Provider interface {
	// Ask 阻塞直到获得用户回答，返回 (result, error)。
	Ask(ctx context.Context, opts QuestionOptions) (*QuestionResult, error)
}

// Service 是用户提问服务（可切换 provider）。
type Service struct {
	provider Provider
}

// New 创建提问服务。
func New(p Provider) *Service {
	return &Service{provider: p}
}

// SetProvider 运行时切换 provider（SDK 注入真实实现）。
func (s *Service) SetProvider(p Provider) {
	s.provider = p
}

// Provider 返回当前 provider（供测试断言）。
func (s *Service) Provider() Provider {
	return s.provider
}

// Ask 向用户提问，返回 (idx, custom, err)。
// 兼容上游签名：selection index + custom string。
func (s *Service) Ask(ctx context.Context, opts QuestionOptions) (int, string, error) {
	if s.provider == nil {
		return 0, "", fmt.Errorf("userq: no provider installed")
	}
	res, err := s.provider.Ask(ctx, opts)
	if err != nil {
		return 0, "", err
	}
	return res.SelectedIndex, res.Custom, nil
}

// AskDetailed 返回完整提问结果（含多选下标）。
func (s *Service) AskDetailed(ctx context.Context, opts QuestionOptions) (*QuestionResult, error) {
	if s.provider == nil {
		return nil, fmt.Errorf("userq: no provider installed")
	}
	return s.provider.Ask(ctx, opts)
}

// ============================================================================
// Provider Stub（同步阻塞，测试用）
// ============================================================================

// StubProvider 是同步阻塞的测试 provider：总是返回默认选项。
type StubProvider struct {
	// DefaultIndex 默认返回的选项下标。
	DefaultIndex int
}

// NewStub 创建默认返回第 0 项的 stub。
func NewStub() *StubProvider {
	return &StubProvider{DefaultIndex: 0}
}

// Ask 实现 Provider：返回默认选中项（custom 为空）。
func (s *StubProvider) Ask(ctx context.Context, opts QuestionOptions) (*QuestionResult, error) {
	idx := opts.DefaultIndex
	if idx < 0 || idx >= len(opts.Choices) {
		idx = s.DefaultIndex
		if idx < 0 || idx >= len(opts.Choices) {
			idx = 0
		}
	}
	selected := []int{idx}
	if opts.MultiSelect {
		// stub 多选：返回全部下标
		selected = make([]int, 0, len(opts.Choices))
		for i := range opts.Choices {
			selected = append(selected, i)
		}
		idx = 0
	}
	return &QuestionResult{SelectedIndex: idx, SelectedIndices: selected}, nil
}