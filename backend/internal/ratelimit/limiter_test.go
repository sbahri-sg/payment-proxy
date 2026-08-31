package ratelimit

import (
	"testing"
	"time"
)

func TestLimiterAllowsBurstThenRefills(t *testing.T) {
	limiter := New(2, 2)
	now := time.Unix(1_700_000_000, 0)
	limiter.now = func() time.Time { return now }
	if first := limiter.Allow("merchant-a"); !first.Allowed || first.Remaining != 1 {
		t.Fatalf("unexpected first decision: %#v", first)
	}
	if second := limiter.Allow("merchant-a"); !second.Allowed || second.Remaining != 0 {
		t.Fatalf("unexpected second decision: %#v", second)
	}
	if denied := limiter.Allow("merchant-a"); denied.Allowed || denied.RetryAfter < time.Second {
		t.Fatalf("burst exhaustion was not rejected: %#v", denied)
	}
	now = now.Add(500 * time.Millisecond)
	if refilled := limiter.Allow("merchant-a"); !refilled.Allowed {
		t.Fatalf("token was not refilled: %#v", refilled)
	}
}

func TestDisabledLimiterAlwaysAllows(t *testing.T) {
	limiter := New(0, 0)
	if decision := limiter.Allow("merchant-a"); !decision.Allowed || decision.Limit != 0 {
		t.Fatalf("disabled limiter rejected request: %#v", decision)
	}
}
