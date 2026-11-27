package scheduler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunKeepsGoingWhenAJobFails(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var good, bad atomic.Int32
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	done := make(chan struct{})
	go func() {
		Run(ctx, time.Millisecond, log,
			Job{Name: "bad", Run: func(context.Context) (int, error) { bad.Add(1); return 0, errors.New("boom") }},
			Job{Name: "good", Run: func(context.Context) (int, error) {
				if good.Add(1) == 3 {
					cancel()
				}
				return 1, nil
			}},
		)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("loop did not stop after cancel")
	}
	if good.Load() < 3 || bad.Load() < 3 {
		t.Fatalf("runs good=%d bad=%d, want both >= 3", good.Load(), bad.Load())
	}
}

func TestRunStopsWhenContextIsAlreadyDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var calls atomic.Int32
	Run(ctx, time.Millisecond, nil, Job{Name: "x", Run: func(context.Context) (int, error) {
		calls.Add(1)
		return 0, nil
	}})
	if calls.Load() != 0 {
		t.Fatalf("job ran %d times after cancel", calls.Load())
	}
}
