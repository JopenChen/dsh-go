// Package tests 的 schedule 进程内调度验收测试。
package tests

import (
	"testing"
	"time"

	"github.com/JopenChen/dsh-go/pkg/schedule"
)

func TestScheduleAfterFires(t *testing.T) {
	s := schedule.New()
	defer s.Shutdown()
	if err := s.After("r1", "提醒我", 30*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	select {
	case d := <-s.Out():
		if d.ID != "r1" || d.Prompt != "提醒我" {
			t.Fatalf("dispatch wrong: %+v", d)
		}
	case <-time.After(time.Second):
		t.Fatal("after timer did not fire")
	}
	// 一次性触发后移除。
	if len(s.List()) != 0 {
		t.Fatal("one-shot entry removed after dispatch")
	}
}

func TestScheduleRejectEmptyAndPast(t *testing.T) {
	s := schedule.New()
	defer s.Shutdown()
	if err := s.After("a", "", time.Second); err != schedule.ErrEmptyPrompt {
		t.Fatalf("empty prompt rejected, got %v", err)
	}
	if err := s.After("b", "x", -time.Second); err != schedule.ErrNotFuture {
		t.Fatalf("non-positive delay rejected, got %v", err)
	}
}

func TestScheduleEveryMinInterval(t *testing.T) {
	s := schedule.New()
	defer s.Shutdown()
	if err := s.Every("e", "x", time.Second); err != schedule.ErrIntervalTooShort {
		t.Fatalf("below-min interval rejected, got %v", err)
	}
}

func TestScheduleCancel(t *testing.T) {
	s := schedule.New()
	defer s.Shutdown()
	_ = s.After("c", "x", 500*time.Millisecond)
	if !s.Cancel("c") {
		t.Fatal("cancel should find entry")
	}
	if s.Cancel("c") {
		t.Fatal("second cancel returns false")
	}
}

func TestScheduleList(t *testing.T) {
	s := schedule.New()
	defer s.Shutdown()
	_ = s.At("a1", "x", time.Now().Add(time.Hour))
	if len(s.List()) != 1 {
		t.Fatal("list should contain one entry")
	}
}
