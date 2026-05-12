package telegram

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// --- backoff math ---

func TestComputeNextBackoff_Exponential(t *testing.T) {
	// jitterRoll=0.5 -> jitter=0 exactly, so next = prev * factor capped at max.
	cases := []struct {
		prev time.Duration
		max  time.Duration
		want time.Duration
	}{
		{1 * time.Second, 60 * time.Second, 2 * time.Second},
		{2 * time.Second, 60 * time.Second, 4 * time.Second},
		{30 * time.Second, 60 * time.Second, 60 * time.Second},
		{60 * time.Second, 60 * time.Second, 60 * time.Second}, // capped
		{120 * time.Second, 60 * time.Second, 60 * time.Second},
	}
	for _, c := range cases {
		got := computeNextBackoff(c.prev, c.max, 0.5)
		if got != c.want {
			t.Errorf("prev=%s max=%s: got %s, want %s", c.prev, c.max, got, c.want)
		}
	}
}

func TestComputeNextBackoff_JitterBounds(t *testing.T) {
	prev := 4 * time.Second
	max := 60 * time.Second
	// jitterRoll=0 -> jitter=-fraction; jitterRoll≈1 -> jitter=+fraction.
	low := computeNextBackoff(prev, max, 0)
	high := computeNextBackoff(prev, max, 0.999999)
	wantBase := 8 * time.Second
	wantLow := time.Duration(float64(wantBase) * (1 - backoffJitterFraction))
	wantHigh := time.Duration(float64(wantBase) * (1 + backoffJitterFraction))
	if low != wantLow {
		t.Errorf("low end: got %s, want %s", low, wantLow)
	}
	// High end may equal wantHigh-1ns due to rounding; allow 1ms slack.
	if abs(high-wantHigh) > time.Millisecond {
		t.Errorf("high end: got %s, want %s", high, wantHigh)
	}
}

func TestComputeNextBackoff_NeverNegative(t *testing.T) {
	// Edge: jitterRoll=0 and very small prev.
	got := computeNextBackoff(0, 60*time.Second, 0)
	if got < 0 {
		t.Errorf("backoff went negative: %s", got)
	}
}

func TestComputeNextBackoff_RespectsCapAfterJitter(t *testing.T) {
	// At max, even +25% jitter shouldn't push past max.
	got := computeNextBackoff(60*time.Second, 60*time.Second, 0.999999)
	if got > 60*time.Second {
		t.Errorf("jitter pushed past max: %s", got)
	}
}

func abs(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

// --- retryRun orchestration ---

func TestRetryRun_StopsOnCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	calls := atomic.Int32{}
	err := retryRun(ctx,
		func(_ context.Context) error {
			calls.Add(1)
			return errors.New("network down")
		},
		func(_ context.Context, _ time.Duration) error { return nil },
		nil,
		1*time.Millisecond, 1*time.Second)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if calls.Load() != 1 {
		t.Errorf("expected 1 call before noticing cancellation, got %d", calls.Load())
	}
}

func TestRetryRun_PropagatesContextErrorFromRunFn(t *testing.T) {
	ctx := context.Background()
	err := retryRun(ctx,
		func(_ context.Context) error { return context.DeadlineExceeded },
		func(_ context.Context, _ time.Duration) error { return nil },
		nil,
		1*time.Millisecond, 1*time.Second)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected DeadlineExceeded, got %v", err)
	}
}

func TestRetryRun_RetriesOnTransportError(t *testing.T) {
	// runFn fails twice with transport errors, then succeeds on third call
	// (returns context.Canceled to stop the loop cleanly).
	calls := atomic.Int32{}
	runFn := func(_ context.Context) error {
		n := calls.Add(1)
		if n < 3 {
			return errors.New("network down")
		}
		return context.Canceled
	}

	reconnects := atomic.Int32{}
	onReconnect := func(_ error, _ time.Duration) { reconnects.Add(1) }
	sleeps := atomic.Int32{}
	sleep := func(_ context.Context, _ time.Duration) error {
		sleeps.Add(1)
		return nil
	}

	err := retryRun(context.Background(), runFn, sleep, onReconnect, 1*time.Millisecond, 1*time.Second)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected canceled (terminating success), got %v", err)
	}
	if calls.Load() != 3 {
		t.Errorf("expected 3 calls, got %d", calls.Load())
	}
	if reconnects.Load() != 2 {
		t.Errorf("expected 2 reconnect callbacks, got %d", reconnects.Load())
	}
	if sleeps.Load() != 2 {
		t.Errorf("expected 2 sleeps, got %d", sleeps.Load())
	}
}

func TestRetryRun_SleepFailureTerminates(t *testing.T) {
	// If sleep returns an error (e.g. context cancelled during wait), retryRun stops.
	calls := atomic.Int32{}
	runFn := func(_ context.Context) error {
		calls.Add(1)
		return errors.New("disconnect")
	}
	sleep := func(_ context.Context, _ time.Duration) error {
		return context.Canceled
	}
	err := retryRun(context.Background(), runFn, sleep, nil, 1*time.Millisecond, 1*time.Second)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected canceled from sleep, got %v", err)
	}
	if calls.Load() != 1 {
		t.Errorf("expected 1 call before sleep terminates, got %d", calls.Load())
	}
}

func TestRetryRun_BackoffGrowsBetweenAttempts(t *testing.T) {
	var slept []time.Duration
	calls := atomic.Int32{}
	runFn := func(_ context.Context) error {
		n := calls.Add(1)
		if n < 5 {
			return errors.New("down")
		}
		return context.Canceled
	}
	sleep := func(_ context.Context, d time.Duration) error {
		slept = append(slept, d)
		return nil
	}
	_ = retryRun(context.Background(), runFn, sleep, nil, 1*time.Second, 60*time.Second)
	if len(slept) != 4 {
		t.Fatalf("expected 4 sleeps, got %d: %v", len(slept), slept)
	}
	// First sleep is the initial backoff (no jitter applied yet to that one).
	if slept[0] != 1*time.Second {
		t.Errorf("first sleep = %s, want 1s (initial)", slept[0])
	}
	// Subsequent sleeps must be strictly increasing (within jitter bounds).
	// Floor of next is prev*2*(1-0.25); 1s -> next is in [1.5s, 2.5s].
	for i := 1; i < len(slept); i++ {
		if slept[i] <= slept[i-1] {
			t.Errorf("sleep[%d]=%s should be > sleep[%d]=%s",
				i, slept[i], i-1, slept[i-1])
		}
	}
}

// --- sleepCtx ---

func TestSleepCtx_WaitsForDuration(t *testing.T) {
	start := time.Now()
	err := sleepCtx(context.Background(), 20*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 15*time.Millisecond {
		t.Errorf("slept only %s, expected ~20ms", elapsed)
	}
}

func TestSleepCtx_CancelledMidSleep(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()
	err := sleepCtx(ctx, 500*time.Millisecond)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected canceled, got %v", err)
	}
}

func TestSleepCtx_ZeroDurationReturnsImmediately(t *testing.T) {
	start := time.Now()
	_ = sleepCtx(context.Background(), 0)
	if elapsed := time.Since(start); elapsed > 1*time.Millisecond {
		t.Errorf("zero-duration sleep took too long: %s", elapsed)
	}
}
