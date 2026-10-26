package middleware

import (
    "context"
    "fmt"
    "math"
    nethttp "net/http"
    "net/http/httptest"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/clock"
    "github.com/precision-soft/melody/v4/container"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/http"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    "github.com/precision-soft/melody/v4/runtime"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
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

func TestFixedWindowLimiter_AllowsUpToRate(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Now())
    limiter := NewFixedWindowLimiter(LimiterConfig{Clock: frozenClock, Limit: 3, Window: time.Minute})

    for i := 0; i < 3; i++ {
        if false == limiter.Allow("key1") {
            t.Fatalf("expected request %d to be allowed", i+1)
        }
    }

    if true == limiter.Allow("key1") {
        t.Fatalf("expected request to be rejected after rate exceeded")
    }
}

func TestFixedWindowLimiter_RefillsAfterWindow(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Now())
    limiter := NewFixedWindowLimiter(LimiterConfig{Clock: frozenClock, Limit: 2, Window: time.Minute})

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

func TestFixedWindowLimiter_Reset(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Now())
    limiter := NewFixedWindowLimiter(LimiterConfig{Clock: frozenClock, Limit: 1, Window: time.Minute})

    limiter.Allow("key1")

    if true == limiter.Allow("key1") {
        t.Fatalf("expected rejection")
    }

    limiter.Reset("key1")

    if false == limiter.Allow("key1") {
        t.Fatalf("expected allow after reset")
    }
}

func TestFixedWindowLimiter_Close(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Now())
    limiter := NewFixedWindowLimiter(LimiterConfig{Clock: frozenClock, Limit: 5, Window: time.Minute})

    err := limiter.Close()
    if nil != err {
        t.Fatalf("expected nil error from Close, got: %v", err)
    }
}

func TestSlidingWindowLimiter_AllowsUpToLimit(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Now())
    limiter := NewSlidingWindowLimiter(LimiterConfig{Clock: frozenClock, Limit: 3, Window: time.Minute})

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
    limiter := NewSlidingWindowLimiter(LimiterConfig{Clock: frozenClock, Limit: 1, Window: time.Minute})

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
    limiter := NewSlidingWindowLimiter(LimiterConfig{Clock: frozenClock, Limit: 3, Window: time.Minute})

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
    limiter := NewSlidingWindowLimiter(LimiterConfig{Clock: frozenClock, Limit: 1, Window: time.Minute})

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
    limiter := NewSlidingWindowLimiter(LimiterConfig{Clock: frozenClock, Limit: 5, Window: time.Minute})

    err := limiter.Close()
    if nil != err {
        t.Fatalf("expected nil error from Close, got: %v", err)
    }
}

func TestRateLimitMiddleware_AllowsRequest(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Now())
    limiter := NewFixedWindowLimiter(LimiterConfig{Clock: frozenClock, Limit: 10, Window: time.Minute})

    config := RateLimitConfig{Limiter: limiter}
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
    limiter := NewFixedWindowLimiter(LimiterConfig{Clock: frozenClock, Limit: 1, Window: time.Minute})

    config := RateLimitConfig{Limiter: limiter}
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

    limiter := NewFixedWindowLimiter(LimiterConfig{Clock: clock.NewFrozenClock(time.Now()), Limit: 10, Window: time.Minute})
    config := RateLimitConfig{Limiter: limiter}
    key := resolveRateLimitConfig(config, "").keyExtractor(melodyRequest)

    if "10.20.30.40" != key {
        t.Fatalf("unexpected key: %s", key)
    }
}

func TestRateLimitConfig_ClientIpResolver_OverridesDefault(t *testing.T) {
    request := httptest.NewRequest(nethttp.MethodGet, "/api/data", nil)
    request.RemoteAddr = "10.20.30.40:1234"
    request.Header.Set("X-Forwarded-For", "1.1.1.1")

    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    limiter := NewFixedWindowLimiter(LimiterConfig{Clock: clock.NewFrozenClock(time.Now()), Limit: 10, Window: time.Minute})
    config := RateLimitConfig{Limiter: limiter}

    config.ClientIpResolver = func(request httpcontract.Request) string {
        return request.Header("X-Forwarded-For")
    }

    key := resolveRateLimitConfig(config, "").keyExtractor(melodyRequest)

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

func TestFixedWindowLimiter_Cleanup(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Now())
    limiter := NewFixedWindowLimiter(LimiterConfig{Clock: frozenClock, Limit: 5, Window: time.Minute})
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
    limiter := NewSlidingWindowLimiter(LimiterConfig{Clock: frozenClock, Limit: 5, Window: time.Minute})
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

func TestFixedWindowLimiter_SeparateKeys(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Now())
    limiter := NewFixedWindowLimiter(LimiterConfig{Clock: frozenClock, Limit: 1, Window: time.Minute})

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

func TestDefaultKeyExtractor_KeysOnClientIpAcrossPaths(t *testing.T) {
    limiter := NewFixedWindowLimiter(LimiterConfig{Clock: clock.NewFrozenClock(time.Now()), Limit: 10, Window: time.Minute})
    config := RateLimitConfig{Limiter: limiter}
    keyFor := func(path string) string {
        request := httptest.NewRequest(nethttp.MethodGet, path, nil)
        request.RemoteAddr = "1.2.3.4:5555"

        return resolveRateLimitConfig(config, "").keyExtractor(testhelper.NewHttpTestRequestFromHttpRequest(request))
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

func TestFixedWindowLimiter_MaxKeysDeniesUnseenKeyWhenFull(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Now())
    limiter := NewFixedWindowLimiter(LimiterConfig{Clock: frozenClock, Limit: 5, Window: time.Minute, MaxKeys: bound.Of(2)})

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

func TestFixedWindowLimiter_MaxKeysReclaimsIdleBeforeDenying(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Now())
    limiter := NewFixedWindowLimiter(LimiterConfig{Clock: frozenClock, Limit: 5, Window: time.Minute, MaxKeys: bound.Of(2)})

    limiter.Allow("a")
    limiter.Allow("b")

    frozenClock.Advance(3 * time.Minute)

    if false == limiter.Allow("c") {
        t.Fatalf("expected an unseen key to be admitted after idle entries are reclaimed")
    }
}

func TestSlidingWindowLimiter_MaxKeysDeniesUnseenKeyWhenFull(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Now())
    limiter := NewSlidingWindowLimiter(LimiterConfig{Clock: frozenClock, Limit: 5, Window: time.Minute, MaxKeys: bound.Of(2)})

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
func TestFixedWindowLimiter_CeilingPruneRunsAtMostOncePerWindow(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Now())
    limiter := NewFixedWindowLimiter(LimiterConfig{Clock: frozenClock, Limit: 5, Window: time.Minute, MaxKeys: bound.Of(2)})

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
    limiter := NewSlidingWindowLimiter(LimiterConfig{Clock: frozenClock, Limit: 5, Window: time.Minute, MaxKeys: bound.Of(2)})

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

    config := RateLimitConfig{Limiter: limiter}
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

    config := RateLimitConfig{Limiter: limiter}
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

    config := RateLimitConfig{Limiter: limiter}

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

/* a ceiling of zero would deny every request for the process lifetime */
func TestLimiters_RefuseAMaxKeysTheRuleRefuses(t *testing.T) {
    for name, build := range map[string]func(config LimiterConfig){
        "fixed":   func(config LimiterConfig) { NewFixedWindowLimiter(config) },
        "sliding": func(config LimiterConfig) { NewSlidingWindowLimiter(config) },
    } {
        t.Run(name, func(t *testing.T) {
            assertPanicsNamingField(t, func() { build(LimiterConfig{Limit: 10, Window: time.Minute, MaxKeys: bound.Of(0)}) }, "the limit is below its minimum", "LimiterConfig.MaxKeys")
            assertPanicsNamingField(t, func() { build(LimiterConfig{Limit: 10, Window: time.Minute, MaxKeys: bound.UnlimitedCount()}) }, "the limit does not admit unlimited", "LimiterConfig.MaxKeys")
        })
    }
}

func TestLimiters_RefuseALimitOrAWindowBelowItsMinimum(t *testing.T) {
    for name, build := range map[string]func(config LimiterConfig){
        "fixed":   func(config LimiterConfig) { NewFixedWindowLimiter(config) },
        "sliding": func(config LimiterConfig) { NewSlidingWindowLimiter(config) },
    } {
        t.Run(name, func(t *testing.T) {
            assertPanicsNamingField(t, func() { build(LimiterConfig{Limit: 0, Window: time.Minute}) }, "the limit is below its minimum", "LimiterConfig.Limit")
            assertPanicsNamingField(t, func() { build(LimiterConfig{Limit: -1, Window: time.Minute}) }, "the limit is below its minimum", "LimiterConfig.Limit")
            assertPanicsNamingField(t, func() { build(LimiterConfig{Limit: 1}) }, "the limit is below its minimum", "LimiterConfig.Window")
            assertPanicsNamingField(t, func() { build(LimiterConfig{Limit: 1, Window: time.Microsecond}) }, "the limit is below its minimum", "LimiterConfig.Window")
            assertPanicsNamingField(t, func() { build(LimiterConfig{Limit: 1, Window: -time.Minute}) }, "the limit is below its minimum", "LimiterConfig.Window")
            build(LimiterConfig{Limit: 1, Window: time.Millisecond})
        })
    }
}

func TestLimiters_ResolveTheCleanupInterval(t *testing.T) {
    if 5*time.Minute != NewFixedWindowLimiter(LimiterConfig{Limit: 1, Window: time.Minute}).cleanupInterval {
        t.Fatalf("expected the fixed window limiter's absent cleanup interval to read 5m")
    }

    if 5*time.Minute != NewSlidingWindowLimiter(LimiterConfig{Limit: 1, Window: time.Minute}).cleanupInterval {
        t.Fatalf("expected the sliding window limiter's absent cleanup interval to read 5m")
    }

    if time.Second != NewSlidingWindowLimiter(LimiterConfig{Limit: 1, Window: time.Minute, CleanupInterval: bound.Of(time.Second)}).cleanupInterval {
        t.Fatalf("expected the declared cleanup interval to be kept")
    }

    assertPanicsNamingField(t, func() {
        NewFixedWindowLimiter(LimiterConfig{Limit: 1, Window: time.Minute, CleanupInterval: bound.Of(time.Microsecond)})
    }, "the limit is below its minimum", "LimiterConfig.CleanupInterval")
    assertPanicsNamingField(t, func() {
        NewSlidingWindowLimiter(LimiterConfig{Limit: 1, Window: time.Minute, CleanupInterval: bound.UnlimitedDuration()})
    }, "the limit does not admit unlimited", "LimiterConfig.CleanupInterval")
}

func TestFixedWindowLimiter_ForgetsAnIdleKeyAtTheDeclaredCleanupInterval(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Now())
    limiter := NewFixedWindowLimiter(LimiterConfig{Clock: frozenClock, Limit: 1, Window: time.Second, CleanupInterval: bound.Of(10 * time.Second)})

    limiter.Allow("idle")
    frozenClock.Advance(11 * time.Second)
    limiter.Allow("other")

    limiter.mutex.RLock()
    _, idleHeld := limiter.buckets["idle"]
    limiter.mutex.RUnlock()

    if true == idleHeld {
        t.Fatalf("expected the idle key forgotten after the declared cleanup interval")
    }
}

/* the limiter is a fixed window, not a token bucket: the allowance is restored whole at the edge, so an instant straddling it admits up to twice the rate. Locked deliberately — SlidingWindowLimiter is the strict-invariant option. */
func TestFixedWindowLimiter_RestoresTheWholeAllowanceAtTheWindowEdge(t *testing.T) {
    clockInstance := clock.NewFrozenClock(time.Unix(0, 0).UTC())
    limiter := NewFixedWindowLimiter(LimiterConfig{Clock: clockInstance, Limit: 100, Window: time.Minute})

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
    fixedLimiter := NewFixedWindowLimiter(LimiterConfig{Clock: fixedClock, Limit: 100, Window: time.Minute})
    fixedAdmitted := spendHalfThenCountAtEdge(fixedLimiter.Allow, fixedClock.Advance)

    slidingClock := clock.NewFrozenClock(time.Unix(0, 0).UTC())
    slidingLimiter := NewSlidingWindowLimiter(LimiterConfig{Clock: slidingClock, Limit: 100, Window: time.Minute})
    slidingAdmitted := spendHalfThenCountAtEdge(slidingLimiter.Allow, slidingClock.Advance)

    if 100 != fixedAdmitted {
        t.Fatalf("expected the fixed window to restore the whole allowance at the edge, got %d", fixedAdmitted)
    }

    if 50 != slidingAdmitted {
        t.Fatalf("expected the sliding window to free only the aged-out half, got %d", slidingAdmitted)
    }
}

func allowingNext() httpcontract.Handler {
    return func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        return http.TextResponse(200, "ok"), nil
    }
}

/* IpRateLimit builds its config internally and hands back only the middleware, so the resolver the documentation prescribes for a deployment behind a reverse proxy could never be reached: every client shared the proxy's single budget. The additive variant takes the resolver up front. */
func TestIpRateLimit_ChargesTheForwardedClient(t *testing.T) {
    resolver := NewForwardedClientIpResolver(trustingPolicy("10.0.0.0/8"))
    handler := IpRateLimit(QuickLimitConfig{RequestsPerMinute: 1, ClientIpResolver: resolver})(allowingNext())

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

/* The direct-peer behaviour of the helper without a resolver is correct without a proxy in front, so an application that upgrades keeps compiling and keeps its semantics. */
func TestIpRateLimit_StillChargesTheDirectPeer(t *testing.T) {
    handler := IpRateLimit(QuickLimitConfig{RequestsPerMinute: 1})(allowingNext())

    _, firstErr := handler(nil, httptest.NewRecorder(), forwardedRequest("10.0.0.1:5555", "203.0.113.7"))
    if nil != firstErr {
        t.Fatalf("the first request must pass, got: %v", firstErr)
    }

    _, secondErr := handler(nil, httptest.NewRecorder(), forwardedRequest("10.0.0.1:6666", "203.0.113.8"))
    if nil == secondErr {
        t.Fatalf("without a resolver both clients key the direct peer and share one budget")
    }
}

func TestSimpleRateLimit_ChargesTheForwardedClient(t *testing.T) {
    resolver := NewForwardedClientIpResolver(trustingPolicy("10.0.0.0/8"))
    handler := SimpleRateLimit(QuickLimitConfig{RequestsPerMinute: 1, ClientIpResolver: resolver})(allowingNext())

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
func TestUserRateLimit_ChargesTheForwardedClientWhenAnonymous(t *testing.T) {
    resolver := NewForwardedClientIpResolver(trustingPolicy("10.0.0.0/8"))
    anonymous := func(request httpcontract.Request) string { return "" }
    handler := UserRateLimit(UserLimitConfig{RequestsPerMinute: 1, UserId: anonymous, ClientIpResolver: resolver})(allowingNext())

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
func TestUserRateLimit_KeepsKeyingIdentifiedUsersById(t *testing.T) {
    resolver := NewForwardedClientIpResolver(trustingPolicy("10.0.0.0/8"))
    identified := func(request httpcontract.Request) string { return "user-1" }
    handler := UserRateLimit(UserLimitConfig{RequestsPerMinute: 1, UserId: identified, ClientIpResolver: resolver})(allowingNext())

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
func TestQuickRateLimits_NilResolverKeepsTheDirectPeer(t *testing.T) {
    handler := IpRateLimit(QuickLimitConfig{RequestsPerMinute: 1})(allowingNext())

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
    limiter := NewFixedWindowLimiter(LimiterConfig{Clock: frozenClock, Limit: 1, Window: time.Duration(math.MaxInt64)})

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
    limiter := NewSlidingWindowLimiter(LimiterConfig{Clock: frozenClock, Limit: 1, Window: time.Duration(math.MaxInt64)})

    if false == limiter.Allow("client") {
        t.Fatalf("the single budgeted request must be allowed")
    }

    frozenClock.Advance(limiter.cleanupInterval + time.Second)

    if true == limiter.Allow("client") {
        t.Fatalf("a never-refilling window must not refill after the idle cleanup")
    }
}

/* SimpleRateLimit is one of the three helpers an application actually calls. Its documented semantics are the direct peer — the resolver cannot be set afterwards because the helper builds its config internally — so two clients behind one proxy sharing a budget is the correct behaviour here, and this test fails if the helper starts reading a forwarded header. */

func TestSimpleRateLimit_ChargesTheDirectPeer(t *testing.T) {
    handler := SimpleRateLimit(QuickLimitConfig{RequestsPerMinute: 1})(allowingNext())

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
    handler := SimpleRateLimit(QuickLimitConfig{RequestsPerMinute: 1})(allowingNext())

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
    assertPanicsNamingField(t, func() { UserRateLimit(UserLimitConfig{RequestsPerMinute: 1}) }, "get user id callback is required for user rate limit middleware", "UserLimitConfig.UserId")
}

func TestQuickRateLimits_RefuseABudgetBelowOne(t *testing.T) {
    userId := func(request httpcontract.Request) string { return "" }

    for _, requestsPerMinute := range []int{0, -1} {
        assertPanicsNamingField(t, func() { SimpleRateLimit(QuickLimitConfig{RequestsPerMinute: requestsPerMinute}) }, "the limit is below its minimum", "QuickLimitConfig.RequestsPerMinute")
        assertPanicsNamingField(t, func() { IpRateLimit(QuickLimitConfig{RequestsPerMinute: requestsPerMinute}) }, "the limit is below its minimum", "QuickLimitConfig.RequestsPerMinute")
        assertPanicsNamingField(t, func() { UserRateLimit(UserLimitConfig{RequestsPerMinute: requestsPerMinute, UserId: userId}) }, "the limit is below its minimum", "UserLimitConfig.RequestsPerMinute")
    }
}

func TestRateLimitMiddleware_AResolverSetAfterTheBuildDoesNotChangeTheKey(t *testing.T) {
    limiter := NewFixedWindowLimiter(LimiterConfig{Limit: 1, Window: time.Minute, Clock: clock.NewFrozenClock(time.Now())})
    config := RateLimitConfig{Limiter: limiter}
    handler := RateLimitMiddleware(config)(allowingNext())

    config.ClientIpResolver = func(request httpcontract.Request) string { return "203.0.113.9" }

    request := httptest.NewRequest(nethttp.MethodGet, "/x", nil)
    request.RemoteAddr = "10.0.0.1:1234"

    if _, err := handler(nil, httptest.NewRecorder(), testhelper.NewHttpTestRequestFromHttpRequest(request)); nil != err {
        t.Fatalf("expected the first request admitted, got %v", err)
    }

    if true == limiter.Allow("10.0.0.1") {
        t.Fatalf("expected the request charged to the direct peer, the key the middleware was built with")
    }

    if false == limiter.Allow("203.0.113.9") {
        t.Fatalf("expected the resolver written after the build never to have been charged")
    }
}

func TestLimiters_ANilClockReadsTheSystemClock(t *testing.T) {
    if nil == NewFixedWindowLimiter(LimiterConfig{Limit: 1, Window: time.Minute}).clockInstance {
        t.Fatalf("expected the fixed window limiter to read the system clock")
    }

    sliding := NewSlidingWindowLimiter(LimiterConfig{Limit: 1, Window: time.Minute})
    if nil == sliding.clockInstance {
        t.Fatalf("expected the sliding window limiter to read the system clock")
    }

    if false == sliding.Allow("a") || true == sliding.Allow("a") {
        t.Fatalf("expected the system-clock limiter to admit one request and refuse the next")
    }
}

/* UserRateLimit keys on the identity rather than the address, which is the whole point of it: one user must carry one budget across every address they arrive from, and two users sharing an address must not share one. */

func TestUserRateLimit_KeysOnTheIdentityRatherThanTheAddress(t *testing.T) {
    identity := "alice"
    getUserId := func(request httpcontract.Request) string { return identity }

    handler := UserRateLimit(UserLimitConfig{RequestsPerMinute: 1, UserId: getUserId})(allowingNext())

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

    handler := UserRateLimit(UserLimitConfig{RequestsPerMinute: 1, UserId: anonymous})(allowingNext())

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

/* a limit handler that produces neither response nor error still refused the request, the reading the listener door gives: passed through, the nil response would be normalized into an empty 204 and the refused request served as success. */
func TestRateLimitMiddleware_AnswersTheSilentLimitHandlerWith429(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Now())
    limiter := NewFixedWindowLimiter(LimiterConfig{Clock: frozenClock, Limit: 1, Window: time.Minute})

    silentHandler := func(request httpcontract.Request) (httpcontract.Response, error) {
        return nil, nil
    }

    config := RateLimitConfig{Limiter: limiter, OnLimitExceeded: silentHandler}
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

func TestRateLimitMiddleware_RefusesTheZeroConfigByName(t *testing.T) {
    assertPanicsNamingField(t, func() { RateLimitMiddleware(RateLimitConfig{}) }, "limiter is required for rate limit middleware", "RateLimitConfig.Limiter")
}

/* the middleware's record classifies the caller's cancellation apart from a store failure: at error every disconnect on a rate-limited route paged the operator for a healthy store. */
func TestRateLimitMiddleware_ACancelledLimiterCallIsRecordedAtWarning(t *testing.T) {
    capture := &rateLimitCaptureLogger{}

    serviceContainer := container.NewContainer()
    scope := serviceContainer.NewScope()
    scope.MustOverrideProtectedInstance(logging.ServiceLogger, capture, container.OverrideOptions{})
    runtimeInstance := runtime.New(context.Background(), scope, serviceContainer)

    config := RateLimitConfig{Limiter: &cancellingRuntimeLimiter{}}
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
    scope.MustOverrideProtectedInstance(logging.ServiceLogger, capture, container.OverrideOptions{})
    runtimeInstance := runtime.New(context.Background(), scope, serviceContainer)

    limiter := &alreadyReportingRuntimeLimiter{}
    middleware := RateLimitMiddleware(RateLimitConfig{Limiter: limiter})

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
    assertPanicsNamingField(t, func() { RateLimitMiddleware(RateLimitConfig{Limiter: (*FixedWindowLimiter)(nil)}) }, "limiter is required for rate limit middleware", "RateLimitConfig.Limiter")
}

/* the listener door shares the middleware door's refusal. The panic is asserted by NAME: with the guard dead, the nil dispatcher passed in the same call panics too, and a recover that accepts any panic would report that second failure as the refusal it is not. */
func TestRegisterRateLimitRequestListener_RefusesATypedNilLimiterByName(t *testing.T) {
    assertPanicsNamingField(t, func() { RegisterRateLimitRequestListener(nil, RateLimitConfig{Limiter: (*FixedWindowLimiter)(nil)}) }, "limiter is required for rate limit request listener", "RateLimitConfig.Limiter")
}

/* the marks are searched with a binary search, which is entitled to an ordered slice, so the recorded instant is clamped to the last mark. A wall clock moved backwards under the process otherwise appended out of order, and an unordered slice does not merely keep an expired mark — the search can cut a LIVE one away and hand the key its budget back, on a limiter the package points at login, one-time codes and password reset. */
func TestSlidingWindowLimiter_AClockMovedBackwardsNeverReplenishesTheBudget(t *testing.T) {
    startedAt := time.Now()
    frozenClock := clock.NewFrozenClock(startedAt)
    limiter := NewSlidingWindowLimiter(LimiterConfig{Clock: frozenClock, Limit: 2, Window: 12 * time.Second})

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

/* one IPv6 host is routinely handed a /64 and can rotate through it, so the key aggregates an IPv6 address to its /64: keyed on the /128, every request of one host had a budget of its own */
func TestDefaultKeyExtractor_AggregatesAnIpv6ClientToItsSlash64(t *testing.T) {
    limiter := NewFixedWindowLimiter(LimiterConfig{Limit: 5, Window: time.Minute})
    config := RateLimitConfig{Limiter: limiter}
    allowedCount := 0
    for index := 1; index <= 100; index++ {
        request := httptest.NewRequest(nethttp.MethodGet, "/api/data", nil)
        request.RemoteAddr = fmt.Sprintf("[2001:db8:1:2::%x]:4711", index)

        if true == limiter.Allow(resolveRateLimitConfig(config, "").keyExtractor(testhelper.NewHttpTestRequestFromHttpRequest(request))) {
            allowedCount++
        }
    }

    if 5 != allowedCount {
        t.Fatalf("expected 100 addresses of one /64 to share one budget of 5, got %d allowed", allowedCount)
    }

    for _, testCase := range []struct {
        remoteAddress string
        expectedKey   string
    }{
        {"[2001:db8:1:2::1]:4711", "2001:db8:1:2::/64"},
        {"[2001:db8:1:3::1]:4711", "2001:db8:1:3::/64"},
        {"[fe80::1%eth0]:4711", "fe80::/64"},
        {"203.0.113.7:4711", "203.0.113.7"},
        {"[::ffff:203.0.113.7]:4711", "203.0.113.7"},
        {"not-an-address", "not-an-address"},
    } {
        request := httptest.NewRequest(nethttp.MethodGet, "/api/data", nil)
        request.RemoteAddr = testCase.remoteAddress

        key := resolveRateLimitConfig(config, "").keyExtractor(testhelper.NewHttpTestRequestFromHttpRequest(request))
        if testCase.expectedKey != key {
            t.Fatalf("expected the key of %q to be %q, got %q", testCase.remoteAddress, testCase.expectedKey, key)
        }
    }
}

func TestRateLimitConfig_ClientIpResolver_AnIpv6AddressItResolvesKeysOnItsSlash64(t *testing.T) {
    request := httptest.NewRequest(nethttp.MethodGet, "/api/data", nil)
    request.RemoteAddr = "10.0.0.1:4711"
    request.Header.Set("X-Forwarded-For", "2001:db8:aa:bb:cc:dd:ee:ff")

    config := RateLimitConfig{Limiter: NewFixedWindowLimiter(LimiterConfig{Limit: 5, Window: time.Minute})}
    config.ClientIpResolver = NewForwardedClientIpResolver(httpcontract.ForwardedHeadersPolicy{
        TrustForwardedHeaders: true,
        TrustedProxyList:      []string{"10.0.0.0/8"},
    })
    key := resolveRateLimitConfig(config, "").keyExtractor(testhelper.NewHttpTestRequestFromHttpRequest(request))
    if "2001:db8:aa:bb::/64" != key {
        t.Fatalf("expected the forwarded IPv6 client to key on its /64, got %q", key)
    }
}

/* evicting at the ceiling admits a stranger at the cost of the key used longest ago: a key touched since keeps its spent budget, the evicted one comes back with a full one */
func TestFixedWindowLimiter_EvictsTheOldestKeyAtTheCeiling(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Now())
    limiter := NewFixedWindowLimiter(LimiterConfig{Clock: frozenClock, Limit: 1, Window: time.Hour, MaxKeys: bound.Of(2), EvictOldestKeyAtCeiling: true})

    limiter.Allow("a")
    frozenClock.Advance(time.Second)
    limiter.Allow("b")
    frozenClock.Advance(time.Second)
    limiter.Allow("a")

    if false == limiter.Allow("c") {
        t.Fatalf("expected an unseen key admitted at the ceiling")
    }

    if true == limiter.Allow("a") {
        t.Fatalf("expected the key touched last to keep its spent budget")
    }

    limiter.mutex.RLock()
    _, bHeld := limiter.buckets["b"]
    trackedKeys := len(limiter.buckets)
    limiter.mutex.RUnlock()

    if true == bHeld || 2 != trackedKeys {
        t.Fatalf("expected b, the key used longest ago, evicted and the map held at 2, got b=%t size=%d", bHeld, trackedKeys)
    }
}

func TestSlidingWindowLimiter_EvictsTheOldestKeyAtTheCeiling(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Now())
    limiter := NewSlidingWindowLimiter(LimiterConfig{Clock: frozenClock, Limit: 1, Window: time.Hour, MaxKeys: bound.Of(2), EvictOldestKeyAtCeiling: true})

    limiter.Allow("a")
    frozenClock.Advance(time.Second)
    limiter.Allow("b")
    frozenClock.Advance(time.Second)
    limiter.Allow("a")

    if false == limiter.Allow("c") {
        t.Fatalf("expected an unseen key admitted at the ceiling")
    }

    if true == limiter.Allow("a") {
        t.Fatalf("expected the key touched last to keep its spent budget")
    }

    limiter.mutex.RLock()
    _, bHeld := limiter.windows["b"]
    limiter.mutex.RUnlock()

    if true == bHeld {
        t.Fatalf("expected b, the key used longest ago, evicted")
    }
}

func TestLimiters_ResolveTheMaxKeys(t *testing.T) {
    if 1_000_000 != NewFixedWindowLimiter(LimiterConfig{Limit: 1, Window: time.Minute}).maxKeys {
        t.Fatalf("expected the fixed window limiter's absent ceiling to read 1,000,000")
    }

    if 1_000_000 != NewSlidingWindowLimiter(LimiterConfig{Limit: 1, Window: time.Minute}).maxKeys {
        t.Fatalf("expected the sliding window limiter's absent ceiling to read 1,000,000")
    }

    if 3 != NewFixedWindowLimiter(LimiterConfig{Limit: 1, Window: time.Minute, MaxKeys: bound.Of(3)}).maxKeys {
        t.Fatalf("expected the declared ceiling to be kept")
    }
}

func TestQuickLimiterConfig_IsTheBudgetOverOneMinute(t *testing.T) {
    config := quickLimiterConfig(7)
    if 7 != config.Limit || time.Minute != config.Window {
        t.Fatalf("expected a budget of 7 over one minute, got %d over %s", config.Limit, config.Window)
    }
}

func TestUserRateLimit_AdmitsTheBudgetPerUser(t *testing.T) {
    identified := func(request httpcontract.Request) string { return "42" }
    handler := UserRateLimit(UserLimitConfig{RequestsPerMinute: 2, UserId: identified})(allowingNext())

    for attempt := 1; attempt <= 3; attempt++ {
        request := httptest.NewRequest(nethttp.MethodGet, "/x", nil)
        _, err := handler(nil, httptest.NewRecorder(), testhelper.NewHttpTestRequestFromHttpRequest(request))
        if (3 == attempt) != (nil != err) {
            t.Fatalf("expected requests 1 and 2 admitted and 3 refused, attempt %d answered %v", attempt, err)
        }
    }
}
