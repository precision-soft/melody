package middleware

import (
    "context"
    "fmt"
    "math"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/clock"
    "github.com/precision-soft/melody/v3/container"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/precision-soft/melody/v3/http"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal/testhelper"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
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

/* the window is trimmed by index rather than rebuilt, so the cut has to land on exactly the marks that left the window: one short and the caller keeps paying for a request that expired, one long and the limit is widened by a slot nobody spent. Staggered marks are what tells the two apart — a whole-window cut and a no-op cut both pass when every mark carries the same instant. */
func TestSlidingWindowLimiter_FreesExactlyTheExpiredPrefix(t *testing.T) {
    startedAt := time.Now()
    frozenClock := clock.NewFrozenClock(startedAt)
    limiter := NewSlidingWindowLimiterWithClock(frozenClock, 3, time.Minute)

    for i := 0; i < 3; i++ {
        if 0 < i {
            frozenClock.Advance(20 * time.Second)
        }

        if false == limiter.Allow("key1") {
            t.Fatalf("expected mark %d to be admitted", i+1)
        }
    }

    /* the marks sit at +0s, +20s and +40s, and the clock reads +40s */
    if true == limiter.Allow("key1") {
        t.Fatalf("expected refusal while all three marks are inside the window")
    }

    frozenClock.TravelTo(startedAt.Add(61 * time.Second))

    if false == limiter.Allow("key1") {
        t.Fatalf("expected the slot of the mark at +0s to be free")
    }

    if true == limiter.Allow("key1") {
        t.Fatalf("expected refusal: the marks at +20s and +40s have not expired yet")
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

/* the ceiling prune walks the whole map, so running it for every unseen key would make a full limiter cost O(tracked keys) per request under the lock all traffic shares; it runs at most once per window, and the reclaim it defers lands on the next walk */
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

/* the sliding window limiter carries the same ceiling walk and the same once-per-window gate */
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

/* the non-positive guard is load-bearing: without it a configuration-sourced zero sets the ceiling to zero, the map-full check trips on the very first request and every request is denied for the process lifetime */
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

/* the limiter is a fixed window, not a token bucket: the allowance is restored whole at the edge, so an instant straddling it admits up to twice the rate. Locked deliberately — SlidingWindowLimiter is the strict-invariant option. */
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

/* the strict alternative on the same traffic shape: half the allowance spent at the start and half just before the edge. The fixed window restores everything at the edge, so 150 requests land inside one trailing window; the sliding window only frees what actually aged out. */
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

/* the deprecated spelling must keep working: it is a type alias plus two forwarding constructors, so an application on the old name is unaffected */
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

/* a window past the midpoint of the duration range must not wrap the idle-prune threshold negative: the cleanup would then delete every bucket and refill a budget the window promised to hold */

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

/* SimpleRateLimit is one of the three helpers an application actually calls, and no test entered it. Its documented semantics are the direct peer — the resolver cannot be set afterwards because the helper builds its config internally — so two clients behind one proxy sharing a budget is the correct behaviour here, and the sentence that says so needs a test that fails if the helper starts reading a forwarded header. */

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

/* a budget the helper hands out has to be a budget: the refusal is the 429 the framework spells as TooManyRequests, not a bare error a handler might mistake for a store failure. */

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

func TestUserRateLimit_RefusesANilUserIdCallbackAtConstruction(t *testing.T) {
    for _, build := range []func(){
        func() { _ = UserRateLimit(1, nil) },
        func() { _ = UserRateLimitWithResolver(1, nil, nil) },
    } {
        func() {
            defer func() {
                recovered := recover()
                if nil == recovered {
                    t.Fatal("expected the nil callback to be refused at construction")
                }

                recoveredErr, isError := recovered.(error)
                if false == isError || false == strings.Contains(recoveredErr.Error(), "get user id callback is required") {
                    t.Fatalf("expected the refusal to name the callback, got %#v", recovered)
                }
            }()

            build()
        }()
    }
}

/* UserRateLimit keys on the identity rather than the address, which is the whole point of it: one user must carry one budget across every address they arrive from, and two users sharing an address must not share one. Neither direction had a test on the helper itself. */

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

/* a request carrying no identity falls back to the address. Without the fallback every anonymous request would key on one empty identity and share a single budget — the first unauthenticated client to arrive would spend it for everyone, which turns the limiter into a denial of service against the traffic it is most needed for. */

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

/* the resolver accessor is what makes SetClientIpResolver verifiable from outside; it had no test at all, so a setter that stored nowhere would have read as working through every path that only exercises the default. */

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

/* a limit handler that produces neither response nor error still refused the request, the reading the listener door gives: passed through, the nil response would be normalized into an empty 204 and the refused request served as success. */
func TestRateLimitMiddleware_AnswersTheSilentLimitHandlerWith429(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Now())
    limiter := NewTokenBucketLimiterWithClock(frozenClock, 1, time.Minute)

    silentHandler := func(request httpcontract.Request) (httpcontract.Response, error) {
        return nil, nil
    }

    config := NewRateLimitConfig(limiter, nil, silentHandler)
    middleware := RateLimitMiddleware(config)

    next := func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        return http.TextResponse(200, "ok"), nil
    }

    handler := middleware(next)

    request := httptest.NewRequest(nethttp.MethodGet, "/test", nil)
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    if _, firstErr := handler(nil, httptest.NewRecorder(), melodyRequest); nil != firstErr {
        t.Fatalf("unexpected error on the allowed request: %v", firstErr)
    }

    response, err := handler(nil, httptest.NewRecorder(), melodyRequest)
    if nil != err {
        t.Fatalf("unexpected error on the refused request: %v", err)
    }

    if nil == response {
        t.Fatal("expected the refused request to be answered rather than passed through as nil")
    }

    if nethttp.StatusTooManyRequests != response.StatusCode() {
        t.Fatalf("expected 429, got %d", response.StatusCode())
    }
}

/* the nil configuration is refused by name, the answer the listener door gives for the same wiring mistake, instead of an invalid-memory-address panic at boot. */
func TestRateLimitMiddleware_RefusesANilConfigByName(t *testing.T) {
    defer func() {
        recovered := recover()
        if nil == recovered {
            t.Fatal("expected the nil configuration to be refused with a panic")
        }

        panicErr, isError := recovered.(error)
        if false == isError || false == strings.Contains(panicErr.Error(), "limiter is required for rate limit middleware") {
            t.Fatalf("expected the named refusal, got %v", recovered)
        }
    }()

    _ = RateLimitMiddleware(nil)
}

/* the middleware's record classifies the caller's cancellation apart from a store failure: at error every disconnect on a rate-limited route paged the operator for a healthy store. */
func TestRateLimitMiddleware_ACancelledLimiterCallIsRecordedAtWarning(t *testing.T) {
    capture := &rateLimitCaptureLogger{}

    serviceContainer := container.NewContainer()
    scope := serviceContainer.NewScope()
    scope.MustOverrideProtectedInstance(logging.ServiceLogger, capture)
    runtimeInstance := runtime.New(context.Background(), scope, serviceContainer)

    config := NewRateLimitConfig(&cancellingRuntimeLimiter{}, nil, nil)
    middleware := RateLimitMiddleware(config)

    handler := middleware(
        func(
            innerRuntime runtimecontract.Runtime,
            writer nethttp.ResponseWriter,
            request httpcontract.Request,
        ) (httpcontract.Response, error) {
            return nil, nil
        },
    )

    request := testhelper.NewHttpTestRequest(nethttp.MethodGet, "http://example.com/limited")
    _, _ = handler(runtimeInstance, httptest.NewRecorder(), request)

    if 1 != capture.warningCalls || 0 != capture.errorCalls {
        t.Fatalf("expected one warning and no error for the cancelled call, got %d warnings %d errors", capture.warningCalls, capture.errorCalls)
    }
}

type cancellingRuntimeLimiter struct{}

func (instance *cancellingRuntimeLimiter) Allow(key string) bool {
    return true
}

func (instance *cancellingRuntimeLimiter) Reset(key string) {
}

func (instance *cancellingRuntimeLimiter) AllowWithRuntime(runtimeInstance runtimecontract.Runtime, key string) (bool, error) {
    return true, context.Canceled
}

type rateLimitCaptureLogger struct {
    loggingcontract.Logger
    warningCalls int
    errorCalls   int
}

func (instance *rateLimitCaptureLogger) Warning(message string, context loggingcontract.Context) {
    instance.warningCalls++
}

func (instance *rateLimitCaptureLogger) Error(message string, context loggingcontract.Context) {
    instance.errorCalls++
}

type alreadyReportingRuntimeLimiter struct {
    allowWithRuntimeCalls int
}

func (instance *alreadyReportingRuntimeLimiter) Allow(key string) bool {
    return true
}

func (instance *alreadyReportingRuntimeLimiter) Reset(key string) {
}

func (instance *alreadyReportingRuntimeLimiter) AllowWithRuntime(runtimeInstance runtimecontract.Runtime, key string) (bool, error) {
    instance.allowWithRuntimeCalls++

    return true, exception.MarkLogged(
        exception.NewError("rate limiter store failure", exceptioncontract.Context{"key": "actor"}, nil),
    )
}

/* a limiter that filed its own record marks it, and the middleware then writes nothing beside it. The limiter knows the key and the failure mode and has doors with no error return at all, so it is the honest place to file from; without the mark being read here, arming its default turned every refused request during an outage into two identical records — at the moment the journal is under the most load. */
func TestRateLimitMiddleware_AFailureTheLimiterAlreadyRecordedIsNotRecordedAgain(t *testing.T) {
    capture := &rateLimitCaptureLogger{}

    serviceContainer := container.NewContainer()
    scope := serviceContainer.NewScope()
    scope.MustOverrideProtectedInstance(logging.ServiceLogger, capture)
    runtimeInstance := runtime.New(context.Background(), scope, serviceContainer)

    limiter := &alreadyReportingRuntimeLimiter{}
    middleware := RateLimitMiddleware(NewRateLimitConfig(limiter, nil, nil))

    handler := middleware(
        func(
            innerRuntime runtimecontract.Runtime,
            writer nethttp.ResponseWriter,
            request httpcontract.Request,
        ) (httpcontract.Response, error) {
            return nil, nil
        },
    )

    request := testhelper.NewHttpTestRequest(nethttp.MethodGet, "http://example.com/limited")
    _, _ = handler(runtimeInstance, httptest.NewRecorder(), request)

    /* an empty journal is also what a middleware that never metered the request leaves behind, so the silence means nothing until the limiter says it was asked */
    if 1 != limiter.allowWithRuntimeCalls {
        t.Fatalf("expected the middleware to meter the request exactly once, got %d calls", limiter.allowWithRuntimeCalls)
    }

    if 0 != capture.warningCalls || 0 != capture.errorCalls {
        t.Fatalf(
            "a failure the limiter already recorded must not be recorded a second time, got %d warnings %d errors",
            capture.warningCalls,
            capture.errorCalls,
        )
    }
}

/* a typed-nil limiter passes the plain comparison, looks live for the guard, and dereferences its nil receiver on the first request the middleware meters; the interface read refuses it at construction under the same name */
func TestRateLimitMiddleware_RefusesATypedNilLimiterByName(t *testing.T) {
    defer func() {
        recovered := recover()
        if nil == recovered {
            t.Fatalf("expected the typed-nil limiter to be refused at construction")
        }
    }()

    _ = RateLimitMiddleware(NewRateLimitConfig((*FixedWindowLimiter)(nil), nil, nil))
}

/* the listener door shares the middleware door's refusal. The panic is asserted by NAME: with the guard dead the nil dispatcher three lines below panics too, and a recover that accepts any panic would report that second failure as the refusal it is not. */
func TestRegisterRateLimitRequestListener_RefusesATypedNilLimiterByName(t *testing.T) {
    defer func() {
        recovered := recover()
        if nil == recovered {
            t.Fatalf("expected the typed-nil limiter to be refused at registration")
        }

        if false == strings.Contains(fmt.Sprintf("%v", recovered), "limiter is required") {
            t.Fatalf("expected the refusal to name the limiter, got %v", recovered)
        }
    }()

    RegisterRateLimitRequestListener(nil, NewRateLimitConfig((*FixedWindowLimiter)(nil), nil, nil))
}

/* the marks are searched with a binary search, which is entitled to an ordered slice, so the recorded instant is clamped to the last mark. A wall clock moved backwards under the process otherwise appended out of order, and an unordered slice does not merely keep an expired mark — the search can cut a LIVE one away and hand the key its budget back, on a limiter the package points at login, one-time codes and password reset. */
func TestSlidingWindowLimiter_AClockMovedBackwardsNeverReplenishesTheBudget(t *testing.T) {
    startedAt := time.Now()
    frozenClock := clock.NewFrozenClock(startedAt)
    limiter := NewSlidingWindowLimiterWithClock(frozenClock, 2, 12*time.Second)

    frozenClock.TravelTo(startedAt.Add(20 * time.Second))
    if false == limiter.Allow("key1") {
        t.Fatal("expected the first request to be admitted")
    }

    /* the clock answers an earlier instant than one already recorded */
    frozenClock.TravelTo(startedAt.Add(5 * time.Second))
    if false == limiter.Allow("key1") {
        t.Fatal("expected the second request to be admitted; the budget is two")
    }

    /* the mark at +20s is still inside the twelve second window that opens at +18s, so the budget is spent */
    frozenClock.TravelTo(startedAt.Add(30 * time.Second))
    if true == limiter.Allow("key1") {
        t.Fatal("expected the refusal: the live mark must not be cut away by a search over unordered marks")
    }
}
