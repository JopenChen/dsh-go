// Package tests 的 attachment 并发限流器验收测试。
package tests

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/JopenChen/dsh-go/pkg/attachment"
)

func TestLimiterBounded(t *testing.T) {
	l := attachment.NewLimiter(2)
	if !l.TryAcquire() || !l.TryAcquire() {
		t.Fatal("first two acquires succeed")
	}
	if l.TryAcquire() {
		t.Fatal("third acquire blocked at concurrency 2")
	}
	l.Release()
	if !l.TryAcquire() {
		t.Fatal("acquire after release succeeds")
	}
}

func TestLimiterRun(t *testing.T) {
	l := attachment.NewLimiter(1)
	var ran int32
	done := make(chan struct{})
	go l.Run(func() {
		atomic.AddInt32(&ran, 1)
		time.Sleep(20 * time.Millisecond)
		close(done)
	})
	time.Sleep(10 * time.Millisecond)
	if l.Active() != 1 {
		t.Fatal("slot occupied during run")
	}
	<-done
	if atomic.LoadInt32(&ran) != 1 {
		t.Fatal("task ran")
	}
}
