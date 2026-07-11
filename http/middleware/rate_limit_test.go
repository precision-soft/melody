package middleware

import (
    nethttp "net/http"
    "net/http/httptest"
    "testing"
    "time"

    "github.com/precision-soft/melody/clock"
    "github.com/precision-soft/melody/exception"
    "github.com/precision-soft/melody/http"
    httpcontract "github.com/precision-soft/melody/http/contract"
    "github.com/precision-soft/melody/internal/testhelper"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
)

func TestDefaultClientIp_UsesRemoteAddr(t *testing.T) {
    req := httptest.NewRequest(nethttp.MethodGet, "/test", nil)
    req.RemoteAddr = "192.168.1.100:12345"

    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(req)

    ip := DefaultClientIp(melodyRequest)
    if "192.168.1.100" != ip {
        t.Fatalf("expected IP without port, got: %s", ip)
    }
}

func TestDefaultClientIp_IgnoresXForwardedFor(t *testing.T) {
    req := httptest.NewRequest(nethttp.MethodGet, "/test", nil)
    req.RemoteAddr = "10.0.0.1:5555"
    req.Header.Set("X-Forwarded-For", "1.2.3.4")

    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(req)

    ip := DefaultClientIp(melodyRequest)
    if "10.0.0.1" != ip {
        t.Fatalf("expected IP without port (ignoring X-Forwarded-For), got: %s", ip)
    }
}

func TestDefaultClientIp_IgnoresXRealIp(t *testing.T) {
    req := httptest.NewRequest(nethttp.MethodGet, "/test", nil)
    req.RemoteAddr = "10.0.0.2:6666"
    req.Header.Set("X-Real-IP", "5.6.7.8")

    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(req)

    ip := DefaultClientIp(melodyRequest)
    if "10.0.0.2" != ip {
        t.Fatalf("expected IP without port (ignoring X-Real-IP), got: %s", ip)
    }
}

func TestDefaultClientIp_IgnoresBothHeaders(t *testing.T) {
    req := httptest.NewRequest(nethttp.MethodGet, "/test", nil)
    req.RemoteAddr = "172.16.0.1:9999"
    req.Header.Set("X-Forwarded-For", "1.1.1.1, 2.2.2.2")
    req.Header.Set("X-Real-IP", "3.3.3.3")

    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(req)

    ip := DefaultClientIp(melodyRequest)
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

func TestDefaultKeyExtractor_UsesRemoteAddrByDefault(t *testing.T) {
    req := httptest.NewRequest(nethttp.MethodGet, "/api/data", nil)
    req.RemoteAddr = "10.20.30.40:1234"
    req.Header.Set("X-Forwarded-For", "spoofed-ip")

    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(req)

    limiter := NewTokenBucketLimiterWithClock(clock.NewFrozenClock(time.Now()), 10, time.Minute)
    config := NewRateLimitConfig(limiter, nil, nil)
    _ = RateLimitMiddleware(config)

    key := config.KeyExtractor()(melodyRequest)

    if "10.20.30.40:/api/data" != key {
        t.Fatalf("unexpected key: %s", key)
    }
}

func TestRateLimitConfig_ClientIpResolver_OverridesDefault(t *testing.T) {
    req := httptest.NewRequest(nethttp.MethodGet, "/api/data", nil)
    req.RemoteAddr = "10.20.30.40:1234"
    req.Header.Set("X-Forwarded-For", "1.1.1.1")

    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(req)

    limiter := NewTokenBucketLimiterWithClock(clock.NewFrozenClock(time.Now()), 10, time.Minute)
    config := NewRateLimitConfig(limiter, nil, nil)

    config.SetClientIpResolver(func(request httpcontract.Request) string {
        return request.Header("X-Forwarded-For")
    })

    _ = RateLimitMiddleware(config)

    key := config.KeyExtractor()(melodyRequest)

    if "1.1.1.1:/api/data" != key {
        t.Fatalf("expected resolver-provided IP, got: %s", key)
    }
}

func TestIpRateLimit_UsesRemoteAddrByDefault(t *testing.T) {
    req := httptest.NewRequest(nethttp.MethodGet, "/test", nil)
    req.RemoteAddr = "192.168.0.50:8080"
    req.Header.Set("X-Forwarded-For", "evil-ip")
    req.Header.Set("X-Real-IP", "also-evil")

    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(req)

    ip := DefaultClientIp(melodyRequest)

    if "192.168.0.50" != ip {
        t.Fatalf("expected IP without port as key, got: %s", ip)
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

func TestTokenBucketLimiter_ZeroWindowDoesNotFailOpen(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Now())
    limiter := NewTokenBucketLimiterWithClock(frozenClock, 1, 0)

    if false == limiter.Allow("key1") {
        t.Fatalf("expected the first request to be allowed")
    }

    if true == limiter.Allow("key1") {
        t.Fatalf("expected the second request to be rejected: a zero window must not silently disable the limiter")
    }
}

func TestSlidingWindowLimiter_ZeroWindowDoesNotFailOpen(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Now())
    limiter := NewSlidingWindowLimiterWithClock(frozenClock, 1, 0)

    if false == limiter.Allow("key1") {
        t.Fatalf("expected the first request to be allowed")
    }

    if true == limiter.Allow("key1") {
        t.Fatalf("expected the second request to be rejected: a zero window must not silently disable the limiter")
    }
}

func TestTokenBucketLimiter_NonPositiveRateDoesNotDenyAll(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Now())
    limiter := NewTokenBucketLimiterWithClock(frozenClock, 0, time.Minute)

    if false == limiter.Allow("key1") {
        t.Fatalf("expected at least one request to be allowed: a non-positive rate must be clamped, not deny all traffic")
    }
}

func TestDefaultKeyExtractor_NormalizesTrailingSlashPaths(t *testing.T) {
    limiter := NewTokenBucketLimiterWithClock(clock.NewFrozenClock(time.Now()), 10, time.Minute)
    config := NewRateLimitConfig(limiter, nil, nil)
    _ = RateLimitMiddleware(config)

    keyFor := func(path string) string {
        req := httptest.NewRequest(nethttp.MethodGet, path, nil)
        req.RemoteAddr = "1.2.3.4:5555"

        return config.KeyExtractor()(testhelper.NewHttpTestRequestFromHttpRequest(req))
    }

    canonical := keyFor("/login")
    if "1.2.3.4:/login" != canonical {
        t.Fatalf("unexpected canonical key: %s", canonical)
    }

    for _, path := range []string{"/login/", "/login//", "/login///"} {
        if canonical != keyFor(path) {
            t.Fatalf("expected %q to share the /login bucket, got %q", path, keyFor(path))
        }
    }
}

type fakeRuntimeRateLimiter struct {
    allowCalls            int
    allowWithRuntimeCalls int
    allowed               bool
    err                   error
}

func (instance *fakeRuntimeRateLimiter) Allow(key string) bool {
    instance.allowCalls++
    return instance.allowed
}

func (instance *fakeRuntimeRateLimiter) Reset(key string) {}

func (instance *fakeRuntimeRateLimiter) AllowWithRuntime(
    runtimeInstance runtimecontract.Runtime,
    key string,
) (bool, error) {
    instance.allowWithRuntimeCalls++
    return instance.allowed, instance.err
}

func TestRateLimitMiddleware_PrefersRuntimeRateLimiter(t *testing.T) {
    limiter := &fakeRuntimeRateLimiter{allowed: true}

    config := NewRateLimitConfig(limiter, nil, nil)
    handler := RateLimitMiddleware(config)(func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        return http.TextResponse(200, "ok"), nil
    })

    req := httptest.NewRequest(nethttp.MethodGet, "/test", nil)
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(req)

    if _, err := handler(nil, httptest.NewRecorder(), melodyRequest); nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if 1 != limiter.allowWithRuntimeCalls || 0 != limiter.allowCalls {
        t.Fatalf(
            "expected the runtime path to be preferred: withRuntime=%d plain=%d",
            limiter.allowWithRuntimeCalls,
            limiter.allowCalls,
        )
    }
}

func TestRateLimitMiddleware_HonorsFailurePolicyDenialOnStoreError(t *testing.T) {
    /* a fail-closed limiter reports the store failure AND returns allowed=false; the middleware must honor the denial */
    limiter := &fakeRuntimeRateLimiter{allowed: false, err: exception.NewError("store unreachable", nil, nil)}

    config := NewRateLimitConfig(limiter, nil, nil)
    handler := RateLimitMiddleware(config)(func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        return http.TextResponse(200, "ok"), nil
    })

    req := httptest.NewRequest(nethttp.MethodGet, "/test", nil)
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(req)

    _, err := handler(nil, httptest.NewRecorder(), melodyRequest)
    if nil == err {
        t.Fatalf("expected the limit-exceeded rejection to surface")
    }
}

func TestRateLimitMiddleware_HonorsFailurePolicyAllowanceOnStoreError(t *testing.T) {
    /* a fail-open limiter reports the store failure but returns allowed=true; the request must pass */
    limiter := &fakeRuntimeRateLimiter{allowed: true, err: exception.NewError("store unreachable", nil, nil)}

    config := NewRateLimitConfig(limiter, nil, nil)

    nextCalled := false
    handler := RateLimitMiddleware(config)(func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        nextCalled = true
        return http.TextResponse(200, "ok"), nil
    })

    req := httptest.NewRequest(nethttp.MethodGet, "/test", nil)
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(req)

    if _, err := handler(nil, httptest.NewRecorder(), melodyRequest); nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if false == nextCalled {
        t.Fatalf("expected the fail-open allowance to reach the handler")
    }
}
