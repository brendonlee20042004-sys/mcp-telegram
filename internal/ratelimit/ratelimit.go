package ratelimit

import (
	"context"
	"sync"

	"github.com/Prgebish/mcp-telegram/internal/config"
	"github.com/gotd/td/bin"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
	"golang.org/x/time/rate"
)

// Limiter wraps rate.Limiter and provides a telegram.Middleware
// that rate-limits every Telegram RPC call.
type Limiter struct {
	limiter *rate.Limiter
}

func New(cfg config.RateConfig) *Limiter {
	return &Limiter{
		limiter: rate.NewLimiter(rate.Limit(cfg.RequestsPerSecond), cfg.Burst),
	}
}

// Wait blocks until the rate limiter allows one event.
func (l *Limiter) Wait(ctx context.Context) error {
	return l.limiter.Wait(ctx)
}

// Middleware returns a telegram.Middleware that rate-limits every RPC call.
func (l *Limiter) Middleware() telegram.Middleware {
	return telegram.MiddlewareFunc(func(next tg.Invoker) telegram.InvokeFunc {
		return func(ctx context.Context, input bin.Encoder, output bin.Decoder) error {
			if err := l.limiter.Wait(ctx); err != nil {
				return err
			}
			return next.Invoke(ctx, input, output)
		}
	})
}

// PerPeerLimiter rate-limits actions on a per-peer basis. Each peer key
// (a stable string like "user:123" or "@boss") gets its own token bucket.
// Buckets are created lazily on first use and live for the process lifetime.
//
// Used by destructive tools to prevent an LLM from spamming a single chat
// even when the global rate budget is not exhausted. The classic failure
// this prevents: LLM gets stuck in a 'are you sure?' / 'yes, send' loop
// and fires N messages in quick succession to the same person.
//
// A nil *PerPeerLimiter is a no-op: Wait returns immediately. Disabled
// by default (rate=0 in config).
type PerPeerLimiter struct {
	rps   rate.Limit
	burst int
	mu    sync.Mutex
	bucks map[string]*rate.Limiter
}

// NewPerPeer returns a PerPeerLimiter with the given per-peer rate and burst.
// If rps <= 0 returns nil so callers can use the .Wait() method nil-safely.
func NewPerPeer(rps float64, burst int) *PerPeerLimiter {
	if rps <= 0 {
		return nil
	}
	if burst <= 0 {
		burst = 1
	}
	return &PerPeerLimiter{
		rps:   rate.Limit(rps),
		burst: burst,
		bucks: make(map[string]*rate.Limiter),
	}
}

// Wait blocks until the per-peer bucket allows one event. Returns nil
// immediately if the receiver is nil (disabled).
func (l *PerPeerLimiter) Wait(ctx context.Context, peerKey string) error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	b, ok := l.bucks[peerKey]
	if !ok {
		b = rate.NewLimiter(l.rps, l.burst)
		l.bucks[peerKey] = b
	}
	l.mu.Unlock()
	return b.Wait(ctx)
}
