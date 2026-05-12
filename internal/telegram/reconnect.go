package telegram

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"
)

// Defaults for the reconnect backoff schedule.
const (
	defaultInitialBackoff = 1 * time.Second
	defaultMaxBackoff     = 60 * time.Second
	backoffFactor         = 2.0
	backoffJitterFraction = 0.25 // ±25% of the computed delay
)

// nextBackoff returns the next backoff interval given the previous one.
// The schedule is exponential (current * factor), capped at max, with a
// uniform ±jitterFraction perturbation applied to the final value.
//
// Pure function — randomness is via math/rand/v2 which is goroutine-safe
// but produces a different sequence per call; tests pass a deterministic
// rand source by calling computeNextBackoff directly.
func nextBackoff(prev, max time.Duration) time.Duration {
	return computeNextBackoff(prev, max, rand.Float64())
}

// computeNextBackoff is the pure version. jitterRoll must be in [0, 1).
// Tests call this directly to assert on bounds without RNG flakiness.
func computeNextBackoff(prev, max time.Duration, jitterRoll float64) time.Duration {
	next := time.Duration(float64(prev) * backoffFactor)
	if next > max {
		next = max
	}
	// jitter: scale jitterRoll [0,1) into [-jitterFraction, +jitterFraction).
	jitter := (jitterRoll*2 - 1) * backoffJitterFraction
	scaled := time.Duration(float64(next) * (1 + jitter))
	if scaled < 0 {
		scaled = 0
	}
	if scaled > max {
		scaled = max
	}
	return scaled
}

// retryRun drives a connection lifecycle with exponential-backoff reconnect.
// It calls runFn in a loop; runFn is expected to block until the connection
// drops or ctx is cancelled, and to return an error describing why it stopped.
// If runFn returns a context error or ctx is cancelled, retryRun stops and
// returns that error. Otherwise it sleeps for the current backoff (using the
// sleep parameter so tests can inject a fake clock) and retries.
//
// onReconnect is invoked between attempts so callers can log or update state;
// it receives the most recent runFn error and the backoff duration that is
// about to be slept through. May be nil.
func retryRun(
	ctx context.Context,
	runFn func(context.Context) error,
	sleep func(context.Context, time.Duration) error,
	onReconnect func(err error, backoff time.Duration),
	initial, max time.Duration,
) error {
	backoff := initial
	for {
		err := runFn(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		if onReconnect != nil {
			onReconnect(err, backoff)
		}
		if sleepErr := sleep(ctx, backoff); sleepErr != nil {
			return sleepErr
		}
		backoff = nextBackoff(backoff, max)
	}
}

// sleepCtx waits for d or for ctx to be cancelled, whichever comes first.
// Returns nil on completed wait, ctx.Err() on cancellation.
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
