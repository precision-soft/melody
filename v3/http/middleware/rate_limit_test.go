package middleware

import (
	"net"
	nethttp "net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/precision-soft/melody/v3/clock"
	"github.com/precision-soft/melody/v3/http"
	httpcontract "github.com/precision-soft/melody/v3/http/contract"
	"github.com/precision-soft/melody/v3/internal/testhelper"
	runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func TestGetClientIp_ExtractsHostFromHostPort(t *testing.T) {
	req := httptest.NewRequest(nethttp.MethodGet, "http://example.com/", nil)
	req.RemoteAddr = "192.168.1.10:54321"

	melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(req)

	ip := getClientIp(melodyRequest)

	if "192.168.1.10" != ip {
		t.Fatalf("expected 192.168.1.10, got: %s", ip)
	}
}

func TestGetClientIp_ReturnsRawAddrWhenNoPort(t *testing.T) {
	req := httptest.NewRequest(nethttp.MethodGet, "http://example.com/", nil)
	req.RemoteAddr = "192.168.1.10"

	melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(req)

	ip := getClientIp(melodyRequest)

	if "192.168.1.10" != ip {
		t.Fatalf("expected 192.168.1.10, got: %s", ip)
	}
}

func TestGetClientIp_HandlesIpv6WithPort(t *testing.T) {
	req := httptest.NewRequest(nethttp.MethodGet, "http://example.com/", nil)
	req.RemoteAddr = net.JoinHostPort("::1", "54321")

	melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(req)

	ip := getClientIp(melodyRequest)

	if "::1" != ip {
		t.Fatalf("expected ::1, got: %s", ip)
	}
}

func TestTokenBucketLimiter_AllowsUpToRate(t *testing.T) {
	frozenClock := clock.NewFrozenClock(time.Now())
	limiter := NewTokenBucketLimiterWithClock(frozenClock, 3, time.Minute)

	for i := 0; i < 3; i++ {
		if false == limiter.Allow("key") {
			t.Fatalf("expected allow on request %d", i+1)
		}
	}

	if true == limiter.Allow("key") {
		t.Fatalf("expected deny after exceeding rate")
	}
}

func TestTokenBucketLimiter_RefillsAfterWindow(t *testing.T) {
	frozenClock := clock.NewFrozenClock(time.Now())
	limiter := NewTokenBucketLimiterWithClock(frozenClock, 1, time.Minute)

	if false == limiter.Allow("key") {
		t.Fatalf("expected allow on first request")
	}

	if true == limiter.Allow("key") {
		t.Fatalf("expected deny after exceeding rate")
	}

	frozenClock.Advance(2 * time.Minute)

	if false == limiter.Allow("key") {
		t.Fatalf("expected allow after window expired")
	}
}

func TestTokenBucketLimiter_ResetClearsKey(t *testing.T) {
	frozenClock := clock.NewFrozenClock(time.Now())
	limiter := NewTokenBucketLimiterWithClock(frozenClock, 1, time.Minute)

	if false == limiter.Allow("key") {
		t.Fatalf("expected allow")
	}

	if true == limiter.Allow("key") {
		t.Fatalf("expected deny")
	}

	limiter.Reset("key")

	if false == limiter.Allow("key") {
		t.Fatalf("expected allow after reset")
	}
}

func TestSlidingWindowLimiter_AllowsUpToLimit(t *testing.T) {
	frozenClock := clock.NewFrozenClock(time.Now())
	limiter := NewSlidingWindowLimiterWithClock(frozenClock, 2, time.Minute)

	if false == limiter.Allow("key") {
		t.Fatalf("expected allow on first request")
	}

	if false == limiter.Allow("key") {
		t.Fatalf("expected allow on second request")
	}

	if true == limiter.Allow("key") {
		t.Fatalf("expected deny after exceeding limit")
	}
}

func TestSlidingWindowLimiter_SlidesWindow(t *testing.T) {
	frozenClock := clock.NewFrozenClock(time.Now())
	limiter := NewSlidingWindowLimiterWithClock(frozenClock, 1, time.Minute)

	if false == limiter.Allow("key") {
		t.Fatalf("expected allow")
	}

	if true == limiter.Allow("key") {
		t.Fatalf("expected deny")
	}

	frozenClock.Advance(2 * time.Minute)

	if false == limiter.Allow("key") {
		t.Fatalf("expected allow after window slides")
	}
}

func TestRateLimitMiddleware_ReturnsRateLimitResponse(t *testing.T) {
	frozenClock := clock.NewFrozenClock(time.Now())
	limiter := NewTokenBucketLimiterWithClock(frozenClock, 1, time.Minute)

	middleware := RateLimitMiddleware(
		NewRateLimitConfig(limiter, nil, nil),
	)

	next := func(
		runtimeInstance runtimecontract.Runtime,
		writer nethttp.ResponseWriter,
		request httpcontract.Request,
	) (httpcontract.Response, error) {
		return http.EmptyResponse(nethttp.StatusOK), nil
	}

	handler := middleware(next)

	req := httptest.NewRequest(nethttp.MethodGet, "/test", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(req)

	firstResponse, firstErr := handler(nil, httptest.NewRecorder(), melodyRequest)
	if nil != firstErr {
		t.Fatalf("expected nil error on first request, got: %v", firstErr)
	}
	if nethttp.StatusOK != firstResponse.StatusCode() {
		t.Fatalf("expected 200 on first request, got: %d", firstResponse.StatusCode())
	}

	_, secondErr := handler(nil, httptest.NewRecorder(), melodyRequest)
	if nil == secondErr {
		t.Fatalf("expected error on rate-limited request")
	}
}

func TestTokenBucketLimiter_CleanupRemovesExpiredBuckets(t *testing.T) {
	frozenClock := clock.NewFrozenClock(time.Now())
	limiter := NewTokenBucketLimiterWithClock(frozenClock, 10, time.Minute)
	limiter.cleanupInterval = 1 * time.Second

	_ = limiter.Allow("old-key")

	frozenClock.Advance(3 * time.Minute)

	_ = limiter.Allow("new-key")

	limiter.mutex.RLock()
	_, oldExists := limiter.buckets["old-key"]
	_, newExists := limiter.buckets["new-key"]
	limiter.mutex.RUnlock()

	if true == oldExists {
		t.Fatalf("expected old-key to be cleaned up")
	}

	if false == newExists {
		t.Fatalf("expected new-key to still exist")
	}
}
