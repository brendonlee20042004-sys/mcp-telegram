package ratelimit

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestPerPeer_NilIsNoOp(t *testing.T) {
	var l *PerPeerLimiter
	if err := l.Wait(context.Background(), "anything"); err != nil {
		t.Errorf("nil limiter must return nil, got %v", err)
	}
}

func TestPerPeer_ZeroRpsReturnsNil(t *testing.T) {
	l := NewPerPeer(0, 5)
	if l != nil {
		t.Error("rps=0 should return nil limiter")
	}
	l2 := NewPerPeer(-1, 5)
	if l2 != nil {
		t.Error("negative rps should return nil limiter")
	}
}

func TestPerPeer_FirstCallNoWait(t *testing.T) {
	l := NewPerPeer(1, 1) // 1 rps, burst 1
	start := time.Now()
	if err := l.Wait(context.Background(), "@alice"); err != nil {
		t.Fatalf("first wait: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Millisecond {
		t.Errorf("first call should not wait (burst=1), elapsed=%s", elapsed)
	}
}

func TestPerPeer_SecondCallWaits(t *testing.T) {
	// 10 rps with burst 1 = 100ms between successful calls.
	l := NewPerPeer(10, 1)
	ctx := context.Background()
	_ = l.Wait(ctx, "@alice")
	start := time.Now()
	if err := l.Wait(ctx, "@alice"); err != nil {
		t.Fatalf("second wait: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 50*time.Millisecond {
		t.Errorf("second call should wait ~100ms, elapsed=%s", elapsed)
	}
}

func TestPerPeer_DifferentPeersIndependent(t *testing.T) {
	l := NewPerPeer(10, 1)
	ctx := context.Background()
	_ = l.Wait(ctx, "@alice")
	start := time.Now()
	if err := l.Wait(ctx, "@bob"); err != nil {
		t.Fatalf("@bob first call: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Millisecond {
		t.Errorf("different peer should not wait, elapsed=%s", elapsed)
	}
}

func TestPerPeer_ContextCancelTerminatesWait(t *testing.T) {
	l := NewPerPeer(1, 1) // 1 rps -> second call waits ~1 second
	ctx, cancel := context.WithCancel(context.Background())
	_ = l.Wait(ctx, "@alice")

	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := l.Wait(ctx, "@alice")
	elapsed := time.Since(start)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if elapsed > 200*time.Millisecond {
		t.Errorf("cancel should terminate wait quickly, elapsed=%s", elapsed)
	}
}

func TestPerPeer_ConcurrentSafeOnSameKey(t *testing.T) {
	// 50 goroutines all hammering the same key. Should not race or panic.
	l := NewPerPeer(1000, 50)
	var wg sync.WaitGroup
	wg.Add(50)
	for i := 0; i < 50; i++ {
		go func() {
			defer wg.Done()
			_ = l.Wait(context.Background(), "@common")
		}()
	}
	wg.Wait()
}
