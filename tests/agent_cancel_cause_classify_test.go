// 本文件对应任务 M18：Agent Cancel 原因分类。
package tests

import (
	"testing"

	"github.com/JopenChen/dsh-go/pkg/agent"
	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/session"
)

// TestAgentCancelCauseClassify 验证 5 种取消路径原因可正确区分。
func TestAgentCancelCauseClassify(t *testing.T) {
	causes := agent.AllCancelCauses()
	if len(causes) != 5 {
		t.Fatalf("取消原因应恰好 5 类: %v", causes)
	}

	for _, cause := range causes {
		sl := session.NewSessionLog(brand.NewSessionID("cancel_" + string(cause)))
		// 打开 turn（turn-stopping 事件要求处于开放 turn 中）
		_, _ = sl.Append(session.TurnStartData{})
		// 记录取消
		if err := agent.RecordCancel(sl, cause); err != nil {
			t.Fatalf("RecordCancel(%s) 失败: %v", cause, err)
		}
		// 提取
		got, ok := agent.ExtractCancelCause(sl.Events())
		if !ok {
			t.Fatalf("原因 %s 应被提取到", cause)
		}
		if got != cause {
			t.Fatalf("原因提取 = %s, want %s", got, cause)
		}
	}
}

// TestAgentCancelViaAgentAPI 验证通过 Agent.Cancel 写入后可提取。
func TestAgentCancelViaAgentAPI(t *testing.T) {
	sl := session.NewSessionLog(brand.NewSessionID("cancel_api"))
	a := agent.NewAgent(brand.NewSessionID("cancel_api"), sl, nil, nil, nil)
	_, _ = sl.Append(session.TurnStartData{})

	a.Cancel(agent.CancelHook)
	got, ok := agent.ExtractCancelCause(sl.Events())
	if !ok || got != agent.CancelHook {
		t.Fatalf("Agent.Cancel 后提取 = %s ok=%v, want hook", got, ok)
	}
}

// TestAgentCancelAbsent 验证无取消记录时返回 false。
func TestAgentCancelAbsent(t *testing.T) {
	sl := session.NewSessionLog(brand.NewSessionID("no_cancel"))
	_, _ = sl.Append(session.UserMessageData{Content: "hi"})

	if _, ok := agent.ExtractCancelCause(sl.Events()); ok {
		t.Fatal("无取消记录时应返回 false")
	}
}
