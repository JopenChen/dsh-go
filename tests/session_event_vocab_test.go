// 本文件对应任务 M04：Session 事件溯源 & 45+ 词汇表（round-trip 一致性）。
package tests

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/JopenChen/dsh-go/pkg/brand"
	"github.com/JopenChen/dsh-go/pkg/session"
)

// fixedTestTime 返回固定测试时间（保证 round-trip 确定性）。
func fixedTestTime() time.Time {
	return time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
}

// sampleDataFor 为每种事件类型构造一份带代表字段值的样本数据。
func sampleDataFor(t session.EventType) session.EventData {
	switch t {
	case session.EventTurnStart:
		return session.TurnStartData{}
	case session.EventTurnEnd:
		return session.TurnEndData{Reason: session.ReasonFinished}
	case session.EventTurnStopping:
		return session.TurnStoppingData{Reason: "goal/round"}
	case session.EventStepStart:
		return session.StepStartData{StepSeq: 1}
	case session.EventStepEnd:
		return session.StepEndData{StepSeq: 1}
	case session.EventAgentError:
		return session.AgentErrorData{Message: "boom", Pkg: "pkg/agent"}
	case session.EventAgentPreStep:
		return session.AgentPreStepData{Blocked: false}
	case session.EventAgentRequest:
		return session.AgentRequestData{Provider: "deepseek", Model: "deepseek-chat"}
	case session.EventUserMessage:
		return session.UserMessageData{Content: "hello", Source: "user"}
	case session.EventAssistantMessage:
		return session.AssistantMessageData{Content: "hi"}
	case session.EventAssistantChunk:
		return session.AssistantChunkData{Text: "ch"}
	case session.EventAssistantReasoning:
		return session.AssistantReasoningData{Text: "thinking"}
	case session.EventInjectionContext:
		return session.InjectionContextData{Name: "skills", Content: "catalog", Hash: "h1"}
	case session.EventSessionTitle:
		return session.SessionTitleData{Title: "My Session"}
	case session.EventToolCall:
		return session.ToolCallData{CallID: brand.NewToolCallID("call_1"), Tool: "bash", Input: json.RawMessage(`{"cmd":"ls"}`)}
	case session.EventToolResult:
		return session.ToolResultData{CallID: brand.NewToolCallID("call_1"), Output: "out"}
	case session.EventToolError:
		return session.ToolErrorData{CallID: brand.NewToolCallID("call_2"), Error: "e"}
	case session.EventToolObserved:
		return session.ToolObservedData{CallID: brand.NewToolCallID("call_1"), Kind: "fs"}
	case session.EventToolPresentation:
		return session.ToolPresentationData{CallID: brand.NewToolCallID("call_1"), Card: "terminal"}
	case session.EventPlanMode:
		return session.PlanModeData{Mode: "on"}
	case session.EventPlanApproval:
		return session.PlanApprovalData{Approved: true}
	case session.EventGoalChange:
		return session.GoalChangeData{GoalID: "g1", Phase: "active", Revision: 1}
	case session.EventGoalRound:
		return session.GoalRoundData{Round: 3}
	case session.EventTodoWrite:
		return session.TodoWriteData{Items: []string{"a", "b"}}
	case session.EventEndSeed:
		return session.EndSeedData{}
	case session.EventPlanUpdate:
		return session.PlanUpdateData{Content: "plan"}
	case session.EventRequestHeader:
		return session.RequestHeaderData{ConfigEpoch: 1, SystemHash: "s", ToolCount: 2}
	case session.EventRequestContext:
		return session.RequestContextData{Provider: "p", Model: "m", Reason: "initial"}
	case session.EventApprovalAsked:
		return session.ApprovalRequestIDData{RequestID: brand.NewApprovalRequestID("req_1"), Tool: "bash", Summary: "run"}
	case session.EventApprovalDecided:
		return session.ApprovalDecidedData{RequestID: brand.NewApprovalRequestID("req_1"), Allowed: true}
	case session.EventPresetChange:
		return session.PresetChangeData{Preset: "safe"}
	case session.EventAgentPresetChange:
		return session.AgentPresetChangeData{Preset: "coder"}
	case session.EventCommandRun:
		return session.CommandRunData{Command: "/plan", Args: "off"}
	case session.EventCommandDone:
		return session.CommandDoneData{Command: "/plan"}
	case session.EventFSWriteIntent:
		return session.FsWriteIntentData{Path: "/a.txt", Version: 1, Observed: true}
	case session.EventFSEditIntent:
		return session.FsEditIntentData{Path: "/a.txt", Version: 1}
	case session.EventFSObserved:
		return session.FsObservedData{Path: "/a.txt"}
	case session.EventFSVersionBump:
		return session.FsVersionBumpData{Path: "/a.txt", Version: 2}
	case session.EventFSRead:
		return session.FsReadData{Path: "/a.txt"}
	case session.EventFSDirList:
		return session.FsDirListData{Path: "/dir"}
	case session.EventSkillsChange:
		return session.SkillsChangeData{Added: []string{"s1"}, Removed: []string{"s2"}}
	case session.EventSkillsCatalog:
		return session.SkillsCatalogData{Catalog: "cat", Hash: "h"}
	case session.EventSkillInject:
		return session.SkillInjectData{SkillName: "s1", Content: "c"}
	case session.EventLLMRequest:
		return session.LLMRequestData{Provider: "deepseek", Model: "m"}
	case session.EventLLMStream:
		return session.LLMStreamData{Text: "tok"}
	case session.EventLLMRetry:
		return session.LLMRetryData{Attempt: 1, BackoffMs: 500, Error: "overload"}
	case session.EventLLMError:
		return session.LLMErrorData{Kind: "overload", Error: "e"}
	case session.EventLLMDone:
		return session.LLMDoneData{PromptTokens: 10, CompletionTokens: 5}
	case session.EventSurfaceReplace:
		return session.SurfaceReplaceData{Start: 1, End: 2, Data: json.RawMessage(`{"x":1}`)}
	case session.EventAttachmentAdded:
		return session.AttachmentAddedData{AttachmentID: brand.NewAttachmentID("att_1"), Name: "pic.png"}
	case session.EventSessionProjectionUpdate:
		return session.ProjectionUpdateData{ProjectionID: brand.NewProjectionID("proj_1"), Hash: "h"}
	case session.EventWorkspaceChange:
		return session.WorkspaceChangeData{WorkspaceID: brand.NewWorkspaceID("ws_1"), Root: "/ws"}
	case session.EventUserQuestion:
		return session.UserQuestionData{Question: "q", Answer: "a"}
	default:
		return nil
	}
}

// TestSessionEventVocabRoundTrip 验证 45+ 事件类型全部可序列化/反序列化 round-trip 一致。
func TestSessionEventVocabRoundTrip(t *testing.T) {
	// 词汇表必须达到 45+ 的要求
	if len(session.AllEventTypes) < 45 {
		t.Fatalf("事件词汇表仅 %d 种，未达 45+ 要求", len(session.AllEventTypes))
	}

	for _, et := range session.AllEventTypes {
		data := sampleDataFor(et)
		if data == nil {
			t.Fatalf("缺少 %q 的样本数据构造", et)
		}
		if data.EventType() != et {
			t.Fatalf("样本类型 %q 与声明 %q 不一致", data.EventType(), et)
		}

		orig := session.SessionEvent{Seq: 1, Time: fixedTestTime(), Type: et, Data: data}
		origBytes, err := json.Marshal(orig)
		if err != nil {
			t.Fatalf("%q Marshal 失败: %v", et, err)
		}

		var restored session.SessionEvent
		if err := json.Unmarshal(origBytes, &restored); err != nil {
			t.Fatalf("%q Unmarshal 失败: %v", et, err)
		}

		restoredBytes, err := json.Marshal(restored)
		if err != nil {
			t.Fatalf("%q 二次 Marshal 失败: %v", et, err)
		}
		if string(origBytes) != string(restoredBytes) {
			t.Fatalf("%q round-trip 不一致\n  原值: %s\n  还原: %s", et, origBytes, restoredBytes)
		}
	}
}

// TestSessionEventUnknownTypeRejected 验证未知事件类型 fail-closed 拒绝。
func TestSessionEventUnknownTypeRejected(t *testing.T) {
	raw := `{"seq":1,"time":"2026-08-31T00:00:00Z","type":"bogus/type","data":{}}`
	var ev session.SessionEvent
	if err := json.Unmarshal([]byte(raw), &ev); err == nil {
		t.Fatal("未知事件类型应反序列化失败")
	}
}
