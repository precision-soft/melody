package middleware

import (
	nethttp "net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/precision-soft/melody/clock"
	"github.com/precision-soft/melody/http"
	httpcontract "github.com/precision-soft/melody/http/contract"
	"github.com/precision-soft/melody/internal/testhelper"
	runtimecontract "github.com/precision-soft/melody/runtime/contract"
)

func TestGetClientIp_UsesRemoteAddr(t *testing.T) {
	req := httptest.NewRequest(nethttp.MethodGet, "/test", nil)
	req.RemoteAddr = "192.168.1.100:12345"

	melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(req)

	ip := getClientIp(melodyRequest)
	if "192.168.1.100" != ip {
		t.Fatalf("expected IP without port, got: %s", ip)
	}
}

func TestGetClientIp_IgnoresXForwardedFor(t *testing.T) {
	req := httptest.NewRequest(nethttp.MethodGet, "/test", nil)
	req.RemoteAddr = "10.0.0.1:5555"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")

	melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(req)

	ip := getClientIp(melodyRequest)
	if "10.0.0.1" != ip {
		t.Fatalf("expected IP without port (ignoring X-Forwarded-For), got: %s", ip)
	}
}

func TestGetClientIp_IgnoresXRealIp(t *testing.T) {
	req := httptest.NewRequest(nethttp.MethodGet, "/test", nil)
	req.RemoteAddr = "10.0.0.2:6666"
	req.Header.Set("X-Real-IP", "5.6.7.8")

	melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(req)

	ip := getClientIp(melodyRequest)
	if "10.0.0.2" != ip {
		t.Fatalf("expected IP without port (ignoring X-Real-IP), got: %s", ip)
	}
}

func TestGetClientIp_IgnoresBothHeaders(t *testing.T) {
	req := httptest.NewRequest(nethttp.MethodGet, "/test", nil)
	req.RemoteAddr = "172.16.0.1:9999"
	req.Header.Set("X-Forwarded-For", "1.1.1.1, 2.2.2.2")
	req.Header.Set("X-Real-IP", "3.3.3.3")

	melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(req)

	ip := getClientIp(melodyRequest)
	if "172.16.0.1" != ip {
		t.Fatalf("expected IP without port (ignoring all proxy headers), got: %s", ip)
	}
}

func TestTokenBucketLimiter_AllowsUpToRate(t *testing.T) {
	frozenClock := clock.NewFrozenClock(time.Now())
	limiter := NewTokenBucketLimiterWithClock(frozenClock, 3, time.Minute)

	for i := 0; i < 3; i++ {
		if false == limiter.Allow("key1") {
			t.Fatalf("expected request %d to be allowed", i+1)
		}
	}

	if true == limiter.Allow("key1") {
		t.Fatalf("expected request to be rejected after rate exceeded")
	}
}

func TestTokenBucketLimiter_RefillsAfterWindow(t *testing.T) {
	frozenClock := clock.NewFrozenClock(time.Now())
	limiter := NewTokenBucketLimiterWithClock(frozenClock, 2, time.Minute)

	limiter.Allow("key1")
	limiter.Allow("key1")

	if true == limiter.Allow("key1") {
		t.Fatalf("expected rejection before window expires")
	}

	frozenClock.Advance(time.Minute + time.Second)

	if false == limiter.Allow("key1") {
		t.Fatalf("expected allow after window elapsed")
	}
}

func TestTokenBucketLimiter_Reset(t *testing.T) {
	frozenClock := clock.NewFrozenClock(time.Now())
	limiter := NewTokenBucketLimiterWithClock(frozenClock, 1, time.Minute)

	limiter.Allow("key1")

	if true == limiter.Allow("key1") {
		t.Fatalf("expected rejection")
	}

	limiter.Reset("key1")

	if false == limiter.Allow("key1") {
		t.Fatalf("expected allow after reset")
	}
}

func TestTokenBucketLimiter_Close(t *testing.T) {
	frozenClock := clock.NewFrozenClock(time.Now())
	limiter := NewTokenBucketLimiterWithClock(frozenClock, 5, time.Minute)

	err := limiter.Close()
	if nil != err {
		t.Fatalf("expected nil error from Close, got: %v", err)
	}
}

func TestSlidingWindowLimiter_AllowsUpToLimit(t *testing.T) {
	frozenClock := clock.NewFrozenClock(time.Now())
	limiter := NewSlidingWindowLimiterWithClock(frozenClock, 3, time.Minute)

	for i := 0; i < 3; i++ {
		if false == limiter.Allow("key1") {
			t.Fatalf("expected request %d to be allowed", i+1)
		}
	}

	if true == limiter.Allow("key1") {
		t.Fatalf("expected rejection after limit exceeded")
	}
}

func TestSlidingWindowLimiter_AllowsAfterWindowExpires(t *testing.T) {
	frozenClock := clock.NewFrozenClock(time.Now())
	limiter := NewSlidingWindowLimiterWithClock(frozenClock, 1, time.Minute)

	limiter.Allow("key1")

	if true == limiter.Allow("key1") {
		t.Fatalf("expected rejection")
	}

	frozenClock.Advance(time.Minute + time.Second)

	if false == limiter.Allow("key1") {
		t.Fatalf("expected allow after window elapsed")
	}
}

func TestSlidingWindowLimiter_Reset(t *testing.T) {
	frozenClock := clock.NewFrozenClock(time.Now())
	limiter := NewSlidingWindowLimiterWithClock(frozenClock, 1, time.Minute)

	limiter.Allow("key1")

	if true == limiter.Allow("key1") {
		t.Fatalf("expected rejection")
	}

	limiter.Reset("key1")

	if false == limiter.Allow("key1") {
		t.Fatalf("expected allow after reset")
	}
}

func TestSlidingWindowLimiter_Close(t *testing.T) {
	frozenClock := clock.NewFrozenClock(time.Now())
	limiter := NewSlidingWindowLimiterWithClock(frozenClock, 5, time.Minute)

	err := limiter.Close()
	if nil != err {
		t.Fatalf("expected nil error from Close, got: %v", err)
	}
}

func TestRateLimitMiddleware_AllowsRequest(t *testing.T) {
	frozenClock := clock.NewFrozenClock(time.Now())
	limiter := NewTokenBucketLimiterWithClock(frozenClock, 10, time.Minute)

	config := NewRateLimitConfig(limiter, nil, nil)
	middleware := RateLimitMiddleware(config)

	nextCalled := false
	next := func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
		nextCalled = true
		return http.TextResponse(200, "ok"), nil
	}

	handler := middleware(next)

	req := httptest.NewRequest(nethttp.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(req)

	response, err := handler(nil, rec, melodyRequest)
	if nil != err {
		t.Fatalf("unexpected error: %v", err)
	}
	if nil == response {
		t.Fatalf("expected response")
	}
	if false == nextCalled {
		t.Fatalf("expected next handler to be called")
	}
}

func TestRateLimitMiddleware_RejectsWhenLimitExceeded(t *testing.T) {
	frozenClock := clock.NewFrozenClock(time.Now())
	limiter := NewTokenBucketLimiterWithClock(frozenClock, 1, time.Minute)

	config := NewRateLimitConfig(limiter, nil, nil)
	middleware := RateLimitMiddleware(config)

	next := func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
		return http.TextResponse(200, "ok"), nil
	}

	handler := middleware(next)

	req := httptest.NewRequest(nethttp.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(req)

	_, _ = handler(nil, rec, melodyRequest)

	_, err := handler(nil, rec, melodyRequest)
	if nil == err {
		t.Fatalf("expected error from rate limit exceeded")
	}
}

func TestDefaultKeyExtractor_UsesRemoteAddr(t *testing.T) {
	req := httptest.NewRequest(nethttp.MethodGet, "/api/data", nil)
	req.RemoteAddr = "10.20.30.40:1234"
	req.Header.Set("X-Forwarded-For", "spoofed-ip")

	melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(req)

	key := defaultKeyExtractor(melodyRequest)

	if "10.20.30.40:/api/data" != key {
		t.Fatalf("unexpected key: %s", key)
	}
}

func TestIpKeyExtractor_UsesRemoteAddr(t *testing.T) {
	req := httptest.NewRequest(nethttp.MethodGet, "/test", nil)
	req.RemoteAddr = "192.168.0.50:8080"
	req.Header.Set("X-Forwarded-For", "evil-ip")
	req.Header.Set("X-Real-IP", "also-evil")

	melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(req)

	key := ipKeyExtractor(melodyRequest)

	if "192.168.0.50" != key {
		t.Fatalf("expected IP without port as key, got: %s", key)
	}
}

func TestTokenBucketLimiter_Cleanup(t *testing.T) {
	frozenClock := clock.NewFrozenClock(time.Now())
	limiter := NewTokenBucketLimiterWithClock(frozenClock, 5, time.Minute)
	limiter.cleanupInterval = time.Second

	limiter.Allow("key1")
	limiter.Allow("key2")


	frozenClock.Advance(3 * time.Minute)


	limiter.Allow("key3")

	limiter.mutex.RLock()
	_, key1Exists := limiter.buckets["key1"]
	_, key2Exists := limiter.buckets["key2"]
	_, key3Exists := limiter.buckets["key3"]
	limiter.mutex.RUnlock()

	if true == key1Exists {
		t.Fatalf("expected key1 to be cleaned up")
	}
	if true == key2Exists {
		t.Fatalf("expected key2 to be cleaned up")
	}
	if false == key3Exists {
		t.Fatalf("expected key3 to exist")
	}
}

func TestSlidingWindowLimiter_Cleanup(t *testing.T) {
	frozenClock := clock.NewFrozenClock(time.Now())
	limiter := NewSlidingWindowLimiterWithClock(frozenClock, 5, time.Minute)
	limiter.cleanupInterval = time.Second

	limiter.Allow("key1")
	limiter.Allow("key2")


	frozenClock.Advance(3 * time.Minute)


	limiter.Allow("key3")

	limiter.mutex.RLock()
	_, key1Exists := limiter.windows["key1"]
	_, key2Exists := limiter.windows["key2"]
	_, key3Exists := limiter.windows["key3"]
	limiter.mutex.RUnlock()

	if true == key1Exists {
		t.Fatalf("expected key1 to be cleaned up")
	}
	if true == key2Exists {
		t.Fatalf("expected key2 to be cleaned up")
	}
	if false == key3Exists {
		t.Fatalf("expected key3 to exist")
	}
}

func TestRateLimitConfig_Accessors(t *testing.T) {
	frozenClock := clock.NewFrozenClock(time.Now())
	limiter := NewTokenBucketLimiterWithClock(frozenClock, 5, time.Minute)

	extractor := func(request httpcontract.Request) string { return "custom" }
	onExceeded := func(request httpcontract.Request) (httpcontract.Response, error) {
		return http.TextResponse(429, "custom exceeded"), nil
	}

	config := NewRateLimitConfig(limiter, extractor, onExceeded)

	if limiter != config.Limiter() {
		t.Fatalf("expected limiter to match")
	}

	if nil == config.KeyExtractor() {
		t.Fatalf("expected key extractor to be set")
	}

	if nil == config.OnLimitExceeded() {
		t.Fatalf("expected on limit exceeded to be set")
	}

	newExtractor := func(request httpcontract.Request) string { return "new" }
	config.SetKeyExtractor(newExtractor)
	if nil == config.KeyExtractor() {
		t.Fatalf("expected updated key extractor")
	}

	newOnExceeded := func(request httpcontract.Request) (httpcontract.Response, error) {
		return http.TextResponse(503, "service unavailable"), nil
	}
	config.SetOnLimitExceeded(newOnExceeded)
	if nil == config.OnLimitExceeded() {
		t.Fatalf("expected updated on limit exceeded")
	}
}

func TestTokenBucketLimiter_SeparateKeys(t *testing.T) {
	frozenClock := clock.NewFrozenClock(time.Now())
	limiter := NewTokenBucketLimiterWithClock(frozenClock, 1, time.Minute)

	if false == limiter.Allow("key1") {
		t.Fatalf("expected key1 first request to be allowed")
	}

	if false == limiter.Allow("key2") {
		t.Fatalf("expected key2 first request to be allowed (separate bucket)")
	}

	if true == limiter.Allow("key1") {
		t.Fatalf("expected key1 second request to be rejected")
	}
}
