package middleware

import (
    "fmt"
    "math"
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
    request := httptest.NewRequest(nethttp.MethodGet, "/test", nil)
    request.RemoteAddr = "192.168.1.100:12345"

    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    ip := DefaultClientIp(melodyRequest)
    if "192.168.1.100" != ip {
        t.Fatalf("expected IP without port, got: %s", ip)
    }
}

func TestDefaultClientIp_IgnoresXForwardedFor(t *testing.T) {
    request := httptest.NewRequest(nethttp.MethodGet, "/test", nil)
    request.RemoteAddr = "10.0.0.1:5555"
    request.Header.Set("X-Forwarded-For", "1.2.3.4")

    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    ip := DefaultClientIp(melodyRequest)
    if "10.0.0.1" != ip {
        t.Fatalf("expected IP without port (ignoring X-Forwarded-For), got: %s", ip)
    }
}

func TestDefaultClientIp_IgnoresXRealIp(t *testing.T) {
    request := httptest.NewRequest(nethttp.MethodGet, "/test", nil)
    request.RemoteAddr = "10.0.0.2:6666"
    request.Header.Set("X-Real-IP", "5.6.7.8")

    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    ip := DefaultClientIp(melodyRequest)
    if "10.0.0.2" != ip {
        t.Fatalf("expected IP without port (ignoring X-Real-IP), got: %s", ip)
    }
}

func TestDefaultClientIp_IgnoresBothHeaders(t *testing.T) {
    request := httptest.NewRequest(nethttp.MethodGet, "/test", nil)
    request.RemoteAddr = "172.16.0.1:9999"
    request.Header.Set("X-Forwarded-For", "1.1.1.1, 2.2.2.2")
    request.Header.Set("X-Real-IP", "3.3.3.3")

    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

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

    request := httptest.NewRequest(nethttp.MethodGet, "/test", nil)
    recorder := httptest.NewRecorder()
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    response, err := handler(nil, recorder, melodyRequest)
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

    request := httptest.NewRequest(nethttp.MethodGet, "/test", nil)
    recorder := httptest.NewRecorder()
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    _, _ = handler(nil, recorder, melodyRequest)

    _, err := handler(nil, recorder, melodyRequest)
    if nil == err {
        t.Fatalf("expected error from rate limit exceeded")
    }
}

func TestDefaultKeyExtractor_UsesRemoteAddrByDefault(t *testing.T) {
    request := httptest.NewRequest(nethttp.MethodGet, "/api/data", nil)
    request.RemoteAddr = "10.20.30.40:1234"
    request.Header.Set("X-Forwarded-For", "spoofed-ip")

    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    limiter := NewTokenBucketLimiterWithClock(clock.NewFrozenClock(time.Now()), 10, time.Minute)
    config := NewRateLimitConfig(limiter, nil, nil)
    _ = RateLimitMiddleware(config)

    key := config.KeyExtractor()(melodyRequest)

    if "10.20.30.40" != key {
        t.Fatalf("unexpected key: %s", key)
    }
}

func TestRateLimitConfig_ClientIpResolver_OverridesDefault(t *testing.T) {
    request := httptest.NewRequest(nethttp.MethodGet, "/api/data", nil)
    request.RemoteAddr = "10.20.30.40:1234"
    request.Header.Set("X-Forwarded-For", "1.1.1.1")

    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    limiter := NewTokenBucketLimiterWithClock(clock.NewFrozenClock(time.Now()), 10, time.Minute)
    config := NewRateLimitConfig(limiter, nil, nil)

    config.SetClientIpResolver(func(request httpcontract.Request) string {
        return request.Header("X-Forwarded-For")
    })

    _ = RateLimitMiddleware(config)

    key := config.KeyExtractor()(melodyRequest)

    if "1.1.1.1" != key {
        t.Fatalf("expected resolver-provided IP, got: %s", key)
    }
}

func TestIpRateLimit_UsesRemoteAddrByDefault(t *testing.T) {
    request := httptest.NewRequest(nethttp.MethodGet, "/test", nil)
    request.RemoteAddr = "192.168.0.50:8080"
    request.Header.Set("X-Forwarded-For", "evil-ip")
    request.Header.Set("X-Real-IP", "also-evil")

    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

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

func TestDefaultKeyExtractor_KeysOnClientIpAcrossPaths(t *testing.T) {
    limiter := NewTokenBucketLimiterWithClock(clock.NewFrozenClock(time.Now()), 10, time.Minute)
    config := NewRateLimitConfig(limiter, nil, nil)
    _ = RateLimitMiddleware(config)

    keyFor := func(path string) string {
        request := httptest.NewRequest(nethttp.MethodGet, path, nil)
        request.RemoteAddr = "1.2.3.4:5555"

        return config.KeyExtractor()(testhelper.NewHttpTestRequestFromHttpRequest(request))
    }

    canonical := keyFor("/login")
    if "1.2.3.4" != canonical {
        t.Fatalf("unexpected key: %s", canonical)
    }

    for _, path := range []string{"/dashboard", "/api/data", "/a/b/c"} {
        if canonical != keyFor(path) {
            t.Fatalf("expected %q from the same ip to share one bucket, got %q", path, keyFor(path))
        }
    }
}

func TestTokenBucketLimiter_MaxKeysDeniesUnseenKeyWhenFull(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Now())
    limiter := NewTokenBucketLimiterWithClock(frozenClock, 5, time.Minute)
    limiter.SetMaxKeys(2)

    if false == limiter.Allow("a") {
        t.Fatalf("expected the first key to be admitted")
    }
    if false == limiter.Allow("b") {
        t.Fatalf("expected the second key to be admitted")
    }

    if true == limiter.Allow("c") {
        t.Fatalf("expected an unseen key to be denied once the map is full of active entries")
    }

    if false == limiter.Allow("a") {
        t.Fatalf("expected an already-tracked key to keep being served")
    }

    limiter.mutex.RLock()
    trackedKeys := len(limiter.buckets)
    limiter.mutex.RUnlock()

    if 2 != trackedKeys {
        t.Fatalf("expected the map to stay bounded at 2 keys, got %d", trackedKeys)
    }
}

func TestTokenBucketLimiter_MaxKeysReclaimsIdleBeforeDenying(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Now())
    limiter := NewTokenBucketLimiterWithClock(frozenClock, 5, time.Minute)
    limiter.SetMaxKeys(2)

    limiter.Allow("a")
    limiter.Allow("b")

    frozenClock.Advance(3 * time.Minute)

    if false == limiter.Allow("c") {
        t.Fatalf("expected an unseen key to be admitted after idle entries are reclaimed")
    }
}

func TestSlidingWindowLimiter_MaxKeysDeniesUnseenKeyWhenFull(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Now())
    limiter := NewSlidingWindowLimiterWithClock(frozenClock, 5, time.Minute)
    limiter.SetMaxKeys(2)

    if false == limiter.Allow("a") {
        t.Fatalf("expected the first key to be admitted")
    }
    if false == limiter.Allow("b") {
        t.Fatalf("expected the second key to be admitted")
    }

    if true == limiter.Allow("c") {
        t.Fatalf("expected an unseen key to be denied once the map is full of active entries")
    }

    limiter.mutex.RLock()
    trackedKeys := len(limiter.windows)
    limiter.mutex.RUnlock()

    if 2 != trackedKeys {
        t.Fatalf("expected the map to stay bounded at 2 keys, got %d", trackedKeys)
    }
}

/* @info the ceiling prune walks the whole map, so running it for every unseen key would make a full limiter cost O(tracked keys) per request under the lock all traffic shares; it runs at most once per window, and the reclaim it defers lands on the next walk */
func TestTokenBucketLimiter_CeilingPruneRunsAtMostOncePerWindow(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Now())
    limiter := NewTokenBucketLimiterWithClock(frozenClock, 5, time.Minute)
    limiter.SetMaxKeys(2)

    limiter.Allow("a")
    limiter.Allow("b")

    frozenClock.Advance(110 * time.Second)

    /* the first unseen key walks the map; nothing is idle yet (an entry falls idle after twice the window), so it is denied */
    if true == limiter.Allow("c") {
        t.Fatalf("expected an unseen key to be denied while every entry is still active")
    }

    frozenClock.Advance(20 * time.Second)

    /* both entries are idle now, but the previous walk ran 20 seconds ago — inside the window — so this request is denied without paying for another walk */
    if true == limiter.Allow("d") {
        t.Fatalf("expected an unseen key to be denied without a second map walk inside the same window")
    }

    frozenClock.Advance(40 * time.Second)

    /* a full window has passed since the last walk, so the idle entries are reclaimed and the unseen key is admitted */
    if false == limiter.Allow("e") {
        t.Fatalf("expected the idle entries to be reclaimed once a window has passed since the last walk")
    }
}

/* @info the sliding window limiter carries the same ceiling walk and the same once-per-window gate */
func TestSlidingWindowLimiter_CeilingPruneRunsAtMostOncePerWindow(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Now())
    limiter := NewSlidingWindowLimiterWithClock(frozenClock, 5, time.Minute)
    limiter.SetMaxKeys(2)

    limiter.Allow("a")
    limiter.Allow("b")

    frozenClock.Advance(110 * time.Second)

    if true == limiter.Allow("c") {
        t.Fatalf("expected an unseen key to be denied while every entry is still active")
    }

    frozenClock.Advance(20 * time.Second)

    if true == limiter.Allow("d") {
        t.Fatalf("expected an unseen key to be denied without a second map walk inside the same window")
    }

    frozenClock.Advance(40 * time.Second)

    if false == limiter.Allow("e") {
        t.Fatalf("expected the idle entries to be reclaimed once a window has passed since the last walk")
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

    request := httptest.NewRequest(nethttp.MethodGet, "/test", nil)
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

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

    request := httptest.NewRequest(nethttp.MethodGet, "/test", nil)
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

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

    request := httptest.NewRequest(nethttp.MethodGet, "/test", nil)
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    if _, err := handler(nil, httptest.NewRecorder(), melodyRequest); nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if false == nextCalled {
        t.Fatalf("expected the fail-open allowance to reach the handler")
    }
}

/* @info the non-positive guard is load-bearing: without it a configuration-sourced zero sets the ceiling to zero, the map-full check trips on the very first request and every request is denied for the process lifetime */
func TestTokenBucketLimiter_NonPositiveMaxKeysIsIgnored(t *testing.T) {
    limiter := NewTokenBucketLimiter(10, time.Minute)

    limiter.SetMaxKeys(0)
    if defaultMaxRateLimitKeys != limiter.maxKeys {
        t.Fatalf("expected a zero ceiling to be ignored, got %d", limiter.maxKeys)
    }

    limiter.SetMaxKeys(-1)
    if defaultMaxRateLimitKeys != limiter.maxKeys {
        t.Fatalf("expected a negative ceiling to be ignored, got %d", limiter.maxKeys)
    }

    for index := 0; index < 100; index++ {
        if false == limiter.Allow(fmt.Sprintf("client-%d", index)) {
            t.Fatalf("expected the limiter to keep admitting unseen keys, denied at %d", index)
        }
    }
}

func TestSlidingWindowLimiter_NonPositiveMaxKeysIsIgnored(t *testing.T) {
    limiter := NewSlidingWindowLimiter(10, time.Minute)

    limiter.SetMaxKeys(0)
    if defaultMaxRateLimitKeys != limiter.maxKeys {
        t.Fatalf("expected a zero ceiling to be ignored, got %d", limiter.maxKeys)
    }

    limiter.SetMaxKeys(-1)
    if defaultMaxRateLimitKeys != limiter.maxKeys {
        t.Fatalf("expected a negative ceiling to be ignored, got %d", limiter.maxKeys)
    }

    for index := 0; index < 100; index++ {
        if false == limiter.Allow(fmt.Sprintf("client-%d", index)) {
            t.Fatalf("expected the limiter to keep admitting unseen keys, denied at %d", index)
        }
    }
}

/* @info the limiter is a fixed window, not a token bucket: the allowance is restored whole at the edge, so an instant straddling it admits up to twice the rate. Locked deliberately — SlidingWindowLimiter is the strict-invariant option. */
func TestFixedWindowLimiter_RestoresTheWholeAllowanceAtTheWindowEdge(t *testing.T) {
    clockInstance := clock.NewFrozenClock(time.Unix(0, 0).UTC())
    limiter := NewFixedWindowLimiterWithClock(clockInstance, 100, time.Minute)

    admittedBeforeEdge := 0
    for index := 0; index < 100; index++ {
        if true == limiter.Allow("client") {
            admittedBeforeEdge++
        }
    }

    if 100 != admittedBeforeEdge {
        t.Fatalf("expected the whole allowance before the edge, got %d", admittedBeforeEdge)
    }

    if true == limiter.Allow("client") {
        t.Fatalf("expected the allowance to be spent before the edge")
    }

    clockInstance.Advance(time.Minute)

    admittedAfterEdge := 0
    for index := 0; index < 100; index++ {
        if true == limiter.Allow("client") {
            admittedAfterEdge++
        }
    }

    if 100 != admittedAfterEdge {
        t.Fatalf("expected the whole allowance to be restored at the edge, got %d", admittedAfterEdge)
    }
}

/* @info the strict alternative on the same traffic shape: half the allowance spent at the start and half just before the edge. The fixed window restores everything at the edge, so 150 requests land inside one trailing window; the sliding window only frees what actually aged out. */
func TestSlidingWindowLimiter_HoldsTheRateWhereTheFixedWindowDoesNot(t *testing.T) {
    spendHalfThenCountAtEdge := func(allow func(string) bool, advance func(time.Duration)) int {
        for index := 0; index < 50; index++ {
            if false == allow("client") {
                t.Fatalf("expected the first half of the allowance to be admitted, denied at %d", index)
            }
        }

        advance(59 * time.Second)

        for index := 0; index < 50; index++ {
            if false == allow("client") {
                t.Fatalf("expected the second half of the allowance to be admitted, denied at %d", index)
            }
        }

        advance(time.Second)

        admittedAtEdge := 0
        for index := 0; index < 100; index++ {
            if true == allow("client") {
                admittedAtEdge++
            }
        }

        return admittedAtEdge
    }

    fixedClock := clock.NewFrozenClock(time.Unix(0, 0).UTC())
    fixedLimiter := NewFixedWindowLimiterWithClock(fixedClock, 100, time.Minute)
    fixedAdmitted := spendHalfThenCountAtEdge(fixedLimiter.Allow, fixedClock.Advance)

    slidingClock := clock.NewFrozenClock(time.Unix(0, 0).UTC())
    slidingLimiter := NewSlidingWindowLimiterWithClock(slidingClock, 100, time.Minute)
    slidingAdmitted := spendHalfThenCountAtEdge(slidingLimiter.Allow, slidingClock.Advance)

    if 100 != fixedAdmitted {
        t.Fatalf("expected the fixed window to restore the whole allowance at the edge, got %d", fixedAdmitted)
    }

    if 50 != slidingAdmitted {
        t.Fatalf("expected the sliding window to free only the aged-out half, got %d", slidingAdmitted)
    }
}

/* @info the deprecated spelling must keep working: it is a type alias plus two forwarding constructors, so an application on the old name is unaffected */
func TestTokenBucketLimiter_DeprecatedAliasStillConstructsTheFixedWindowLimiter(t *testing.T) {
    var limiter *FixedWindowLimiter = NewTokenBucketLimiter(2, time.Minute)

    if false == limiter.Allow("client") {
        t.Fatalf("expected the first request to be admitted")
    }

    var aliased *TokenBucketLimiter = limiter

    if false == aliased.Allow("client") {
        t.Fatalf("expected the second request to be admitted through the alias")
    }

    if true == aliased.Allow("client") {
        t.Fatalf("expected the allowance to be spent")
    }
}

func allowingNext() httpcontract.Handler {
    return func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        return http.TextResponse(200, "ok"), nil
    }
}

/* IpRateLimit builds its config internally and hands back only the middleware, so the resolver the documentation prescribes for a deployment behind a reverse proxy could never be reached: every client shared the proxy's single budget. The additive variant takes the resolver up front. */
func TestIpRateLimitWithResolver_ChargesTheForwardedClient(t *testing.T) {
    resolver := NewForwardedClientIpResolver(trustingPolicy("10.0.0.0/8"))
    handler := IpRateLimitWithResolver(1, resolver)(allowingNext())

    _, firstErr := handler(nil, httptest.NewRecorder(), forwardedRequest("10.0.0.1:5555", "203.0.113.7"))
    if nil != firstErr {
        t.Fatalf("the first client's only request must pass, got: %v", firstErr)
    }

    _, secondErr := handler(nil, httptest.NewRecorder(), forwardedRequest("10.0.0.1:6666", "203.0.113.8"))
    if nil != secondErr {
        t.Fatalf("a different client behind the same proxy must have its own budget, got: %v", secondErr)
    }

    _, thirdErr := handler(nil, httptest.NewRecorder(), forwardedRequest("10.0.0.1:7777", "203.0.113.7"))
    if nil == thirdErr {
        t.Fatalf("the first client's second request must exhaust its own budget")
    }
}

/* The direct-peer behaviour of the original helper is correct without a proxy in front and stays exactly as it was, so an application that upgrades keeps compiling and keeps its semantics. */
func TestIpRateLimit_StillChargesTheDirectPeer(t *testing.T) {
    handler := IpRateLimit(1)(allowingNext())

    _, firstErr := handler(nil, httptest.NewRecorder(), forwardedRequest("10.0.0.1:5555", "203.0.113.7"))
    if nil != firstErr {
        t.Fatalf("the first request must pass, got: %v", firstErr)
    }

    _, secondErr := handler(nil, httptest.NewRecorder(), forwardedRequest("10.0.0.1:6666", "203.0.113.8"))
    if nil == secondErr {
        t.Fatalf("without a resolver both clients key the direct peer and share one budget")
    }
}

func TestSimpleRateLimitWithResolver_ChargesTheForwardedClient(t *testing.T) {
    resolver := NewForwardedClientIpResolver(trustingPolicy("10.0.0.0/8"))
    handler := SimpleRateLimitWithResolver(1, resolver)(allowingNext())

    _, firstErr := handler(nil, httptest.NewRecorder(), forwardedRequest("10.0.0.1:5555", "203.0.113.7"))
    if nil != firstErr {
        t.Fatalf("the first client's only request must pass, got: %v", firstErr)
    }

    _, secondErr := handler(nil, httptest.NewRecorder(), forwardedRequest("10.0.0.1:6666", "203.0.113.8"))
    if nil != secondErr {
        t.Fatalf("a different client behind the same proxy must have its own budget, got: %v", secondErr)
    }

    _, thirdErr := handler(nil, httptest.NewRecorder(), forwardedRequest("10.0.0.1:7777", "203.0.113.7"))
    if nil == thirdErr {
        t.Fatalf("the first client's second request must exhaust its own budget")
    }
}

/* UserRateLimit falls back to the client address for a request carrying no user id, so unauthenticated traffic behind a proxy shared one budget — the traffic a limiter is most needed for. */
func TestUserRateLimitWithResolver_ChargesTheForwardedClientWhenAnonymous(t *testing.T) {
    resolver := NewForwardedClientIpResolver(trustingPolicy("10.0.0.0/8"))
    anonymous := func(request httpcontract.Request) string { return "" }
    handler := UserRateLimitWithResolver(1, anonymous, resolver)(allowingNext())

    _, firstErr := handler(nil, httptest.NewRecorder(), forwardedRequest("10.0.0.1:5555", "203.0.113.7"))
    if nil != firstErr {
        t.Fatalf("the first client's only request must pass, got: %v", firstErr)
    }

    _, secondErr := handler(nil, httptest.NewRecorder(), forwardedRequest("10.0.0.1:6666", "203.0.113.8"))
    if nil != secondErr {
        t.Fatalf("a different anonymous client behind the same proxy must have its own budget, got: %v", secondErr)
    }

    _, thirdErr := handler(nil, httptest.NewRecorder(), forwardedRequest("10.0.0.1:7777", "203.0.113.7"))
    if nil == thirdErr {
        t.Fatalf("the first client's second request must exhaust its own budget")
    }
}

/* An identified user is keyed by id whatever the resolver says, so the resolver only governs the anonymous fallback. */
func TestUserRateLimitWithResolver_KeepsKeyingIdentifiedUsersById(t *testing.T) {
    resolver := NewForwardedClientIpResolver(trustingPolicy("10.0.0.0/8"))
    identified := func(request httpcontract.Request) string { return "user-1" }
    handler := UserRateLimitWithResolver(1, identified, resolver)(allowingNext())

    _, firstErr := handler(nil, httptest.NewRecorder(), forwardedRequest("10.0.0.1:5555", "203.0.113.7"))
    if nil != firstErr {
        t.Fatalf("the user's only request must pass, got: %v", firstErr)
    }

    _, secondErr := handler(nil, httptest.NewRecorder(), forwardedRequest("10.0.0.1:6666", "203.0.113.8"))
    if nil == secondErr {
        t.Fatalf("one user reaching the service from two addresses must still share one budget")
    }
}

/* A nil resolver is the documented way to ask for the direct peer, so the additive variants must behave exactly like the originals when given one. */
func TestRateLimitWithResolver_NilResolverKeepsTheDirectPeer(t *testing.T) {
    handler := IpRateLimitWithResolver(1, nil)(allowingNext())

    _, firstErr := handler(nil, httptest.NewRecorder(), forwardedRequest("10.0.0.1:5555", "203.0.113.7"))
    if nil != firstErr {
        t.Fatalf("the first request must pass, got: %v", firstErr)
    }

    _, secondErr := handler(nil, httptest.NewRecorder(), forwardedRequest("10.0.0.1:6666", "203.0.113.8"))
    if nil == secondErr {
        t.Fatalf("a nil resolver must key the direct peer, so both clients share one budget")
    }
}

/* @info a window past the midpoint of the duration range must not wrap the idle-prune threshold negative: the cleanup would then delete every bucket and refill a budget the window promised to hold */

func TestFixedWindowLimiter_MaxDurationWindowSurvivesIdlePrune(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Now())
    limiter := NewFixedWindowLimiterWithClock(frozenClock, 1, time.Duration(math.MaxInt64))

    if false == limiter.Allow("client") {
        t.Fatalf("the single budgeted request must be allowed")
    }

    frozenClock.Advance(limiter.cleanupInterval + time.Second)

    if true == limiter.Allow("client") {
        t.Fatalf("a never-refilling window must not refill after the idle cleanup")
    }
}

func TestSlidingWindowLimiter_MaxDurationWindowSurvivesIdlePrune(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Now())
    limiter := NewSlidingWindowLimiterWithClock(frozenClock, 1, time.Duration(math.MaxInt64))

    if false == limiter.Allow("client") {
        t.Fatalf("the single budgeted request must be allowed")
    }

    frozenClock.Advance(limiter.cleanupInterval + time.Second)

    if true == limiter.Allow("client") {
        t.Fatalf("a never-refilling window must not refill after the idle cleanup")
    }
}

/* @info SimpleRateLimit is one of the three helpers an application actually calls, and no test entered it. Its documented semantics are the direct peer — the resolver cannot be set afterwards because the helper builds its config internally — so two clients behind one proxy sharing a budget is the correct behaviour here, and the sentence that says so needs a test that fails if the helper starts reading a forwarded header. */

func TestSimpleRateLimit_ChargesTheDirectPeer(t *testing.T) {
    handler := SimpleRateLimit(1)(allowingNext())

    _, firstErr := handler(nil, httptest.NewRecorder(), forwardedRequest("10.0.0.1:5555", "203.0.113.7"))
    if nil != firstErr {
        t.Fatalf("the first request must pass, got: %v", firstErr)
    }

    _, secondErr := handler(nil, httptest.NewRecorder(), forwardedRequest("10.0.0.1:6666", "203.0.113.8"))
    if nil == secondErr {
        t.Fatalf("without a resolver both clients key the direct peer and share one budget")
    }
}

/* @info a budget the helper hands out has to be a budget: the refusal is the 429 the framework spells as TooManyRequests, not a bare error a handler might mistake for a store failure. */

func TestSimpleRateLimit_RefusesWithTooManyRequests(t *testing.T) {
    handler := SimpleRateLimit(1)(allowingNext())

    peer := func() httpcontract.Request {
        request := httptest.NewRequest(nethttp.MethodGet, "/x", nil)
        request.RemoteAddr = "203.0.113.9:5555"

        return testhelper.NewHttpTestRequestFromHttpRequest(request)
    }

    if _, firstErr := handler(nil, httptest.NewRecorder(), peer()); nil != firstErr {
        t.Fatalf("the first request must pass, got: %v", firstErr)
    }

    _, secondErr := handler(nil, httptest.NewRecorder(), peer())
    if nil == secondErr {
        t.Fatalf("the second request must exhaust the budget")
    }

    httpException, isHttpException := secondErr.(*exception.HttpException)
    if false == isHttpException {
        t.Fatalf("expected the refusal to be an http exception, got: %T", secondErr)
    }

    if nethttp.StatusTooManyRequests != httpException.StatusCode() {
        t.Fatalf("expected 429, got: %d", httpException.StatusCode())
    }
}

/* @info UserRateLimit keys on the identity rather than the address, which is the whole point of it: one user must carry one budget across every address they arrive from, and two users sharing an address must not share one. Neither direction had a test on the helper itself. */

func TestUserRateLimit_KeysOnTheIdentityRatherThanTheAddress(t *testing.T) {
    identity := "alice"
    getUserId := func(request httpcontract.Request) string { return identity }

    handler := UserRateLimit(1, getUserId)(allowingNext())

    _, firstErr := handler(nil, httptest.NewRecorder(), forwardedRequest("10.0.0.1:5555", ""))
    if nil != firstErr {
        t.Fatalf("the first request must pass, got: %v", firstErr)
    }

    _, sameUserErr := handler(nil, httptest.NewRecorder(), forwardedRequest("198.51.100.4:9999", ""))
    if nil == sameUserErr {
        t.Fatalf("the same identity arriving from another address must carry the same budget")
    }

    identity = "bob"

    _, otherUserErr := handler(nil, httptest.NewRecorder(), forwardedRequest("10.0.0.1:5555", ""))
    if nil != otherUserErr {
        t.Fatalf("a different identity on the same address must have its own budget, got: %v", otherUserErr)
    }
}

/* @info a request carrying no identity falls back to the address. Without the fallback every anonymous request would key on one empty identity and share a single budget — the first unauthenticated client to arrive would spend it for everyone, which turns the limiter into a denial of service against the traffic it is most needed for. */

func TestUserRateLimit_FallsBackToTheAddressWhenAnonymous(t *testing.T) {
    anonymous := func(request httpcontract.Request) string { return "" }

    handler := UserRateLimit(1, anonymous)(allowingNext())

    _, firstErr := handler(nil, httptest.NewRecorder(), forwardedRequest("10.0.0.1:5555", ""))
    if nil != firstErr {
        t.Fatalf("the first anonymous request must pass, got: %v", firstErr)
    }

    _, otherAddressErr := handler(nil, httptest.NewRecorder(), forwardedRequest("198.51.100.4:9999", ""))
    if nil != otherAddressErr {
        t.Fatalf("another anonymous client must have its own budget rather than share one empty key, got: %v", otherAddressErr)
    }

    _, sameAddressErr := handler(nil, httptest.NewRecorder(), forwardedRequest("10.0.0.1:6666", ""))
    if nil == sameAddressErr {
        t.Fatalf("the same anonymous address must exhaust its own budget")
    }
}

/* @info the resolver accessor is what makes SetClientIpResolver verifiable from outside; it had no test at all, so a setter that stored nowhere would have read as working through every path that only exercises the default. */

func TestRateLimitConfig_ClientIpResolverAccessorReportsWhatWasSet(t *testing.T) {
    config := NewRateLimitConfig(NewFixedWindowLimiter(1, time.Minute), nil, nil)

    if nil != config.ClientIpResolver() {
        t.Fatalf("expected no resolver on a freshly built configuration")
    }

    config.SetClientIpResolver(func(request httpcontract.Request) string { return "203.0.113.1" })

    resolver := config.ClientIpResolver()
    if nil == resolver {
        t.Fatalf("expected the accessor to report the resolver that was set")
    }

    if "203.0.113.1" != resolver(nil) {
        t.Fatalf("expected the accessor to report the very resolver that was set")
    }

    config.SetClientIpResolver(nil)

    if nil != config.ClientIpResolver() {
        t.Fatalf("expected the accessor to report the resolver being cleared")
    }
}
