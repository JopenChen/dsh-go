package tools

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunParallelOrder(t *testing.T) {
	tasks := make([]func(context.Context) (int, error), 5)
	for i := range tasks {
		i := i
		tasks[i] = func(context.Context) (int, error) { return i * i, nil }
	}
	got, err := RunParallel(context.Background(), tasks, 2)
	if err != nil {
		t.Fatal(err)
	}
	want := []int{0, 1, 4, 9, 16}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestRunParallelBounded(t *testing.T) {
	var inFlight, peak int32
	tasks := make([]func(context.Context) (int, error), 8)
	for i := range tasks {
		tasks[i] = func(context.Context) (int, error) {
			cur := atomic.AddInt32(&inFlight, 1)
			for {
				old := atomic.LoadInt32(&peak)
				if cur <= old || atomic.CompareAndSwapInt32(&peak, old, cur) {
					break
				}
			}
			time.Sleep(10 * time.Millisecond)
			atomic.AddInt32(&inFlight, -1)
			return 0, nil
		}
	}
	if _, err := RunParallel(context.Background(), tasks, 3); err != nil {
		t.Fatal(err)
	}
	if peak > 3 {
		t.Fatalf("peak concurrency = %d, want <= 3", peak)
	}
}

func TestRunParallelError(t *testing.T) {
	boom := errors.New("boom")
	tasks := []func(context.Context) (int, error){
		func(context.Context) (int, error) { return 1, nil },
		func(context.Context) (int, error) { return 0, boom },
	}
	if _, err := RunParallel(context.Background(), tasks, 2); !errors.Is(err, boom) {
		t.Fatalf("got %v want boom", err)
	}
}
