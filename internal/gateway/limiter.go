package gateway

import (
	"sync"
	"time"
)

type Limiter interface {
	Allow(key string, reqDelta, tokDelta, maxReq, maxTok int) bool
}

type minuteBucket struct {
	windowStart time.Time
	reqCount    int
	tokenCount  int
}

type minuteLimiter struct {
	mu      sync.Mutex
	buckets map[string]*minuteBucket
}

func newMinuteLimiter() *minuteLimiter {
	return &minuteLimiter{
		buckets: make(map[string]*minuteBucket),
	}
}

func (l *minuteLimiter) Allow(key string, reqDelta, tokDelta, maxReq, maxTok int) bool {
	if maxReq <= 0 && maxTok <= 0 {
		return true
	}

	now := time.Now().UTC().Truncate(time.Minute)
	l.mu.Lock()
	defer l.mu.Unlock()

	b, ok := l.buckets[key]
	if !ok || b.windowStart.Before(now) {
		b = &minuteBucket{windowStart: now}
		l.buckets[key] = b
	}

	if maxReq > 0 && (b.reqCount+reqDelta) > maxReq {
		return false
	}
	if maxTok > 0 && (b.tokenCount+tokDelta) > maxTok {
		return false
	}

	b.reqCount += reqDelta
	b.tokenCount += tokDelta
	return true
}
