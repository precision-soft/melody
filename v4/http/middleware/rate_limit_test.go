package middleware

import (
    "context"
    "errors"
    "fmt"
    "math"
    nethttp "net/http"
    "net/http/httptest"
    "os"
    "strconv"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/clock"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    eventcontract "github.com/precision-soft/melody/v4/event/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/http"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
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
    limiter := NewFixedWindowLimiter("test", 3, time.Minute, LimiterConfig{Clock: frozenClock})

    for i := 0; i < 3; i++ {
        if false == allowedBy(limiter, "key1") {
            t.Fatalf("expected request %d to be allowed", i+1)
        }
    }

    if true == allowedBy(limiter, "key1") {
        t.Fatalf("expected request to be rejected after rate exceeded")
    }
}

func TestFixedWindowLimiter_RefillsAfterWindow(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Now())
    limiter := NewFixedWindowLimiter("test", 2, time.Minute, LimiterConfig{Clock: frozenClock})

    allowedBy(limiter, "key1")
    allowedBy(limiter, "key1")

    if true == allowedBy(limiter, "key1") {
        t.Fatalf("expected rejection before window expires")
    }

    frozenClock.Advance(time.Minute + time.Second)

    if false == allowedBy(limiter, "key1") {
        t.Fatalf("expected allow after window elapsed")
    }
}

func TestFixedWindowLimiter_Reset(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Now())
    limiter := NewFixedWindowLimiter("test", 1, time.Minute, LimiterConfig{Clock: frozenClock})

    allowedBy(limiter, "key1")

    if true == allowedBy(limiter, "key1") {
        t.Fatalf("expected rejection")
    }

    resetBy(limiter, "key1")

    if false == allowedBy(limiter, "key1") {
        t.Fatalf("expected allow after reset")
    }
}

func TestSlidingWindowLimiter_AllowsUpToLimit(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Now())
    limiter := NewSlidingWindowLimiter("test", 3, time.Minute, LimiterConfig{Clock: frozenClock})

    for i := 0; i < 3; i++ {
        if false == allowedBy(limiter, "key1") {
            t.Fatalf("expected request %d to be allowed", i+1)
        }
    }

    if true == allowedBy(limiter, "key1") {
        t.Fatalf("expected rejection after limit exceeded")
    }
}

func TestSlidingWindowLimiter_AllowsAfterWindowExpires(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Now())
    limiter := NewSlidingWindowLimiter("test", 1, time.Minute, LimiterConfig{Clock: frozenClock})

    allowedBy(limiter, "key1")

    if true == allowedBy(limiter, "key1") {
        t.Fatalf("expected rejection")
    }

    frozenClock.Advance(time.Minute + time.Second)

    if false == allowedBy(limiter, "key1") {
        t.Fatalf("expected allow after window elapsed")
    }
}

func TestSlidingWindowLimiter_FreesExactlyTheExpiredPrefix(t *testing.T) {
    startedAt := time.Now()
    frozenClock := clock.NewFrozenClock(startedAt)
    limiter := NewSlidingWindowLimiter("test", 3, time.Minute, LimiterConfig{Clock: frozenClock})

    for i := 0; i < 3; i++ {
        if 0 < i {
            frozenClock.Advance(20 * time.Second)
        }

        if false == allowedBy(limiter, "key1") {
            t.Fatalf("expected mark %d to be admitted", i+1)
        }
    }

    /* the marks sit at +0s, +20s and +40s, and the clock reads +40s */
    if true == allowedBy(limiter, "key1") {
        t.Fatalf("expected refusal while all three marks are inside the window")
    }

    frozenClock.TravelTo(startedAt.Add(61 * time.Second))

    if false == allowedBy(limiter, "key1") {
        t.Fatalf("expected the slot of the mark at +0s to be free")
    }

    if true == allowedBy(limiter, "key1") {
        t.Fatalf("expected refusal: the marks at +20s and +40s have not expired yet")
    }
}

func TestSlidingWindowLimiter_Reset(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Now())
    limiter := NewSlidingWindowLimiter("test", 1, time.Minute, LimiterConfig{Clock: frozenClock})

    allowedBy(limiter, "key1")

    if true == allowedBy(limiter, "key1") {
        t.Fatalf("expected rejection")
    }

    resetBy(limiter, "key1")

    if false == allowedBy(limiter, "key1") {
        t.Fatalf("expected allow after reset")
    }
}

func TestRateLimitMiddleware_AllowsRequest(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Now())
    limiter := NewFixedWindowLimiter("test", 10, time.Minute, LimiterConfig{Clock: frozenClock})

    config := RateLimitConfig{}
    middleware := RateLimitMiddleware(limiter, config)

    nextCalled := false
    next := func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        nextCalled = true
        return http.TextResponse(200, "ok"), nil
    }

    handler := middleware(next)

    request := httptest.NewRequest(nethttp.MethodGet, "/test", nil)
    recorder := httptest.NewRecorder()
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    response, err := handler(rateLimitTestRuntime(), recorder, melodyRequest)
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
    limiter := NewFixedWindowLimiter("test", 1, time.Minute, LimiterConfig{Clock: frozenClock})

    config := RateLimitConfig{}
    middleware := RateLimitMiddleware(limiter, config)

    next := func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        return http.TextResponse(200, "ok"), nil
    }

    handler := middleware(next)

    request := httptest.NewRequest(nethttp.MethodGet, "/test", nil)
    recorder := httptest.NewRecorder()
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    _, _ = handler(rateLimitTestRuntime(), recorder, melodyRequest)

    _, err := handler(rateLimitTestRuntime(), recorder, melodyRequest)
    if nil == err {
        t.Fatalf("expected error from rate limit exceeded")
    }
}

func TestDefaultKeyExtractor_UsesRemoteAddrByDefault(t *testing.T) {
    request := httptest.NewRequest(nethttp.MethodGet, "/api/data", nil)
    request.RemoteAddr = "10.20.30.40:1234"
    request.Header.Set("X-Forwarded-For", "spoofed-ip")

    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    limiter := NewFixedWindowLimiter("test", 10, time.Minute, LimiterConfig{Clock: clock.NewFrozenClock(time.Now())})
    config := RateLimitConfig{}
    key := resolveRateLimitConfig(limiter, config, "", "").keyExtractor(melodyRequest)

    if "10.20.30.40" != key {
        t.Fatalf("unexpected key: %s", key)
    }
}

func TestRateLimitConfig_ClientIpResolver_OverridesDefault(t *testing.T) {
    request := httptest.NewRequest(nethttp.MethodGet, "/api/data", nil)
    request.RemoteAddr = "10.20.30.40:1234"
    request.Header.Set("X-Forwarded-For", "1.1.1.1")

    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    limiter := NewFixedWindowLimiter("test", 10, time.Minute, LimiterConfig{Clock: clock.NewFrozenClock(time.Now())})
    config := RateLimitConfig{}

    config.ClientIpResolver = func(request httpcontract.Request) string {
        return request.Header("X-Forwarded-For")
    }

    key := resolveRateLimitConfig(limiter, config, "", "").keyExtractor(melodyRequest)

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
    limiter := NewFixedWindowLimiter("test", 5, time.Minute, LimiterConfig{Clock: frozenClock})
    limiter.cleanupInterval = time.Second

    allowedBy(limiter, "key1")
    allowedBy(limiter, "key2")

    frozenClock.Advance(3 * time.Minute)

    allowedBy(limiter, "key3")

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
    limiter := NewSlidingWindowLimiter("test", 5, time.Minute, LimiterConfig{Clock: frozenClock})
    limiter.cleanupInterval = time.Second

    allowedBy(limiter, "key1")
    allowedBy(limiter, "key2")

    frozenClock.Advance(3 * time.Minute)

    allowedBy(limiter, "key3")

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
    limiter := NewFixedWindowLimiter("test", 1, time.Minute, LimiterConfig{Clock: frozenClock})

    if false == allowedBy(limiter, "key1") {
        t.Fatalf("expected key1 first request to be allowed")
    }

    if false == allowedBy(limiter, "key2") {
        t.Fatalf("expected key2 first request to be allowed (separate bucket)")
    }

    if true == allowedBy(limiter, "key1") {
        t.Fatalf("expected key1 second request to be rejected")
    }
}

func TestDefaultKeyExtractor_KeysOnClientIpAcrossPaths(t *testing.T) {
    limiter := NewFixedWindowLimiter("test", 10, time.Minute, LimiterConfig{Clock: clock.NewFrozenClock(time.Now())})
    config := RateLimitConfig{}
    keyFor := func(path string) string {
        request := httptest.NewRequest(nethttp.MethodGet, path, nil)
        request.RemoteAddr = "1.2.3.4:5555"

        return resolveRateLimitConfig(limiter, config, "", "").keyExtractor(testhelper.NewHttpTestRequestFromHttpRequest(request))
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

type boundedLimiter struct {
    name    string
    limiter httpcontract.RateLimiter
    held    func(key string) bool
    size    func() int
    ordered func() int
}

func boundedLimiters(limit int, window time.Duration, config LimiterConfig) []boundedLimiter {
    fixed := NewFixedWindowLimiter("test", limit, window, config)
    sliding := NewSlidingWindowLimiter("test", limit, window, config)

    return []boundedLimiter{
        {
            name:    "fixed",
            limiter: fixed,
            held: func(key string) bool {
                fixed.mutex.RLock()
                defer fixed.mutex.RUnlock()
                _, held := fixed.buckets[key]
                return held
            },
            size: func() int {
                fixed.mutex.RLock()
                defer fixed.mutex.RUnlock()
                return len(fixed.buckets)
            },
            ordered: func() int {
                fixed.mutex.RLock()
                defer fixed.mutex.RUnlock()
                return len(fixed.recency.elements)
            },
        },
        {
            name:    "sliding",
            limiter: sliding,
            held: func(key string) bool {
                sliding.mutex.RLock()
                defer sliding.mutex.RUnlock()
                _, held := sliding.windows[key]
                return held
            },
            size: func() int {
                sliding.mutex.RLock()
                defer sliding.mutex.RUnlock()
                return len(sliding.windows)
            },
            ordered: func() int {
                sliding.mutex.RLock()
                defer sliding.mutex.RUnlock()
                return len(sliding.recency.elements)
            },
        },
    }
}

func TestLimiters_AFullTableEvictsTheLeastRecentlySeenKey(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Now())

    for _, subject := range boundedLimiters(1, time.Hour, LimiterConfig{Clock: frozenClock, MaxKeys: bound.Of(2)}) {
        allowedBy(subject.limiter, "a")
        frozenClock.Advance(time.Second)
        allowedBy(subject.limiter, "b")
        frozenClock.Advance(time.Second)
        allowedBy(subject.limiter, "a")

        if false == allowedBy(subject.limiter, "c") {
            t.Fatalf("%s: expected an unseen key admitted at a full table of active keys", subject.name)
        }

        if true == allowedBy(subject.limiter, "a") {
            t.Fatalf("%s: expected the key seen last to keep its spent budget", subject.name)
        }

        if true == subject.held("b") || 2 != subject.size() {
            t.Fatalf("%s: expected b, the key seen longest ago, evicted and the table held at 2, got b=%t size=%d", subject.name, subject.held("b"), subject.size())
        }
    }
}

func TestLimiters_AnEvictedKeyComesBackWithAFullBudget(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Now())

    for _, subject := range boundedLimiters(1, time.Hour, LimiterConfig{Clock: frozenClock, MaxKeys: bound.Of(2)}) {
        allowedBy(subject.limiter, "a")
        if true == allowedBy(subject.limiter, "a") {
            t.Fatalf("%s: expected a's budget spent", subject.name)
        }

        allowedBy(subject.limiter, "b")
        allowedBy(subject.limiter, "c")

        if true == subject.held("a") {
            t.Fatalf("%s: expected a evicted by c", subject.name)
        }

        if false == allowedBy(subject.limiter, "a") {
            t.Fatalf("%s: expected the evicted key back with a full budget", subject.name)
        }
    }
}

func TestLimiters_AKeySeenAgainIsNotTheOneEvicted(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Now())

    for _, subject := range boundedLimiters(1, time.Hour, LimiterConfig{Clock: frozenClock, MaxKeys: bound.Of(2)}) {
        allowedBy(subject.limiter, "a")
        allowedBy(subject.limiter, "b")

        if true == allowedBy(subject.limiter, "a") {
            t.Fatalf("%s: expected a's second request denied", subject.name)
        }

        allowedBy(subject.limiter, "c")

        if false == subject.held("a") || true == subject.held("b") {
            t.Fatalf("%s: expected b evicted and a kept, got a=%t b=%t", subject.name, subject.held("a"), subject.held("b"))
        }

        if true == allowedBy(subject.limiter, "a") {
            t.Fatalf("%s: expected the kept key to keep its spent budget", subject.name)
        }
    }
}

func TestLimiters_TheTableNeverHoldsMoreThanMaxKeys(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Now())

    for _, subject := range boundedLimiters(1, time.Hour, LimiterConfig{Clock: frozenClock, MaxKeys: bound.Of(3)}) {
        for index := 0; index < 30; index++ {
            if false == allowedBy(subject.limiter, fmt.Sprintf("flood-%d", index)) {
                t.Fatalf("%s: expected unseen key %d admitted", subject.name, index)
            }

            if 3 < subject.size() {
                t.Fatalf("%s: expected at most 3 keys tracked, got %d after key %d", subject.name, subject.size(), index)
            }
        }

        if 3 != subject.size() || 3 != subject.ordered() {
            t.Fatalf("%s: expected the table and its eviction order full at 3 keys, got %d and %d", subject.name, subject.size(), subject.ordered())
        }

        for _, key := range []string{"flood-27", "flood-28", "flood-29"} {
            if false == subject.held(key) {
                t.Fatalf("%s: expected the three most recent keys kept, %s missing", subject.name, key)
            }
        }
    }
}

func TestLimiters_AResetKeyFreesItsSlot(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Now())

    for _, subject := range boundedLimiters(1, time.Hour, LimiterConfig{Clock: frozenClock, MaxKeys: bound.Of(2)}) {
        allowedBy(subject.limiter, "a")
        allowedBy(subject.limiter, "b")
        resetBy(subject.limiter, "a")
        allowedBy(subject.limiter, "c")

        if false == subject.held("b") || false == subject.held("c") || 2 != subject.size() {
            t.Fatalf("%s: expected the reset key's slot reused without an eviction, got b=%t c=%t size=%d", subject.name, subject.held("b"), subject.held("c"), subject.size())
        }

        if subject.size() != subject.ordered() {
            t.Fatalf("%s: expected the eviction order to hold the %d tracked keys alone, got %d", subject.name, subject.size(), subject.ordered())
        }
    }
}

type fakeRuntimeRateLimiter struct {
    allowCalls   int
    allowed      bool
    retryAfter   time.Duration
    err          error
    seenRuntime  containercontract.Runtime
}

func (instance *fakeRuntimeRateLimiter) Allow(runtimeInstance containercontract.Runtime, key string, _ httpcontract.AllowOptions) (httpcontract.RateLimitDecision, error) {
    instance.allowCalls++
    instance.seenRuntime = runtimeInstance

    return httpcontract.RateLimitDecision{Allowed: instance.allowed, RetryAfter: instance.retryAfter, Limit: 1}, instance.err
}

func (instance *fakeRuntimeRateLimiter) Reset(runtimeInstance containercontract.Runtime, key string) error {
    return nil
}

func TestRateLimitMiddleware_HandsTheRequestRuntimeToTheLimiter(t *testing.T) {
    limiter := &fakeRuntimeRateLimiter{allowed: true}

    config := RateLimitConfig{}
    handler := RateLimitMiddleware(limiter, config)(func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        return http.TextResponse(200, "ok"), nil
    })

    request := httptest.NewRequest(nethttp.MethodGet, "/test", nil)
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    runtimeInstance := rateLimitTestRuntime()
    if _, err := handler(runtimeInstance, httptest.NewRecorder(), melodyRequest); nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if 1 != limiter.allowCalls || runtimeInstance != limiter.seenRuntime {
        t.Fatalf("expected one call carrying the request runtime, got %d calls", limiter.allowCalls)
    }
}

func TestRateLimitMiddleware_HonorsFailurePolicyDenialOnStoreError(t *testing.T) {
    limiter := &fakeRuntimeRateLimiter{allowed: false, err: exception.NewError("store unreachable", nil, nil)}

    config := RateLimitConfig{}
    handler := RateLimitMiddleware(limiter, config)(func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        return http.TextResponse(200, "ok"), nil
    })

    request := httptest.NewRequest(nethttp.MethodGet, "/test", nil)
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    _, err := handler(rateLimitTestRuntime(), httptest.NewRecorder(), melodyRequest)
    if nil == err {
        t.Fatalf("expected the limit-exceeded rejection to surface")
    }
}

func TestRateLimitMiddleware_HonorsFailurePolicyAllowanceOnStoreError(t *testing.T) {
    limiter := &fakeRuntimeRateLimiter{allowed: true, err: exception.NewError("store unreachable", nil, nil)}

    config := RateLimitConfig{}

    nextCalled := false
    handler := RateLimitMiddleware(limiter, config)(func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        nextCalled = true
        return http.TextResponse(200, "ok"), nil
    })

    request := httptest.NewRequest(nethttp.MethodGet, "/test", nil)
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    if _, err := handler(rateLimitTestRuntime(), httptest.NewRecorder(), melodyRequest); nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if false == nextCalled {
        t.Fatalf("expected the fail-open allowance to reach the handler")
    }
}

/* a ceiling of zero would deny every request for the process lifetime */
func TestLimiters_RefuseAMaxKeysTheRuleRefuses(t *testing.T) {
    for name, build := range map[string]func(config LimiterConfig){
        "fixed":   func(config LimiterConfig) { NewFixedWindowLimiter("test", 10, time.Minute, config) },
        "sliding": func(config LimiterConfig) { NewSlidingWindowLimiter("test", 10, time.Minute, config) },
    } {
        t.Run(name, func(t *testing.T) {
            assertPanicsNamingField(t, func() { build(LimiterConfig{MaxKeys: bound.Of(0)}) }, "the limit is below its minimum", "LimiterConfig.MaxKeys")
            assertPanicsNamingField(t, func() { build(LimiterConfig{MaxKeys: bound.UnlimitedCount()}) }, "the limit does not admit unlimited", "LimiterConfig.MaxKeys")
        })
    }
}

func TestRateLimiter_ALimitBelowOneAndAWindowBelowAMillisecondAreRefusedNamingTheArgument(t *testing.T) {
    for name, build := range map[string]func(limit int, window time.Duration){
        "fixed":   func(limit int, window time.Duration) { NewFixedWindowLimiter("test", limit, window, LimiterConfig{}) },
        "sliding": func(limit int, window time.Duration) { NewSlidingWindowLimiter("test", limit, window, LimiterConfig{}) },
    } {
        t.Run(name, func(t *testing.T) {
            assertPanicsNamingContextKey(t, func() { build(0, time.Minute) }, "the limit is below its minimum", "argument", "limit")
            assertPanicsNamingContextKey(t, func() { build(-1, time.Minute) }, "the limit is below its minimum", "argument", "limit")
            assertPanicsNamingContextKey(t, func() { build(1, 0) }, "the limit is below its minimum", "argument", "window")
            assertPanicsNamingContextKey(t, func() { build(1, time.Microsecond) }, "the limit is below its minimum", "argument", "window")
            assertPanicsNamingContextKey(t, func() { build(1, -time.Minute) }, "the limit is below its minimum", "argument", "window")
            build(1, time.Millisecond)
        })
    }
}

func TestRateLimiter_AnEmptyOrMalformedNameIsRefusedAtConstruction(t *testing.T) {
    for name, build := range map[string]func(limiterName string){
        "fixed":   func(limiterName string) { NewFixedWindowLimiter(limiterName, 1, time.Minute, LimiterConfig{}) },
        "sliding": func(limiterName string) { NewSlidingWindowLimiter(limiterName, 1, time.Minute, LimiterConfig{}) },
    } {
        t.Run(name, func(t *testing.T) {
            for _, refused := range []string{"", "a:b", "a b", strings.Repeat("a", 65), "é", "a/b"} {
                assertPanicsNamingContextKey(t, func() { build(refused) }, "the limiter name is malformed", "argument", "name")
            }

            for _, admitted := range []string{"a", "Login-2fa_budget.v1", strings.Repeat("z", 64)} {
                build(admitted)
            }
        })
    }
}

func TestRateLimiter_TheInProcessLimitersAnswerTheirName(t *testing.T) {
    if "login" != NewFixedWindowLimiter("login", 1, time.Minute, LimiterConfig{}).Name() {
        t.Fatalf("expected the fixed window limiter to answer its name")
    }

    if "login" != NewSlidingWindowLimiter("login", 1, time.Minute, LimiterConfig{}).Name() {
        t.Fatalf("expected the sliding window limiter to answer its name")
    }
}

func TestLimiters_ResolveTheCleanupInterval(t *testing.T) {
    if 5*time.Minute != NewFixedWindowLimiter("test", 1, time.Minute, LimiterConfig{}).cleanupInterval {
        t.Fatalf("expected the fixed window limiter's absent cleanup interval to read 5m")
    }

    if 5*time.Minute != NewSlidingWindowLimiter("test", 1, time.Minute, LimiterConfig{}).cleanupInterval {
        t.Fatalf("expected the sliding window limiter's absent cleanup interval to read 5m")
    }

    if time.Second != NewSlidingWindowLimiter("test", 1, time.Minute, LimiterConfig{CleanupInterval: bound.Of(time.Second)}).cleanupInterval {
        t.Fatalf("expected the declared cleanup interval to be kept")
    }

    assertPanicsNamingField(t, func() {
        NewFixedWindowLimiter("test", 1, time.Minute, LimiterConfig{CleanupInterval: bound.Of(time.Microsecond)})
    }, "the limit is below its minimum", "LimiterConfig.CleanupInterval")
    assertPanicsNamingField(t, func() {
        NewSlidingWindowLimiter("test", 1, time.Minute, LimiterConfig{CleanupInterval: bound.UnlimitedDuration()})
    }, "the limit does not admit unlimited", "LimiterConfig.CleanupInterval")
}

func TestFixedWindowLimiter_ForgetsAnIdleKeyAtTheDeclaredCleanupInterval(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Now())
    limiter := NewFixedWindowLimiter("test", 1, time.Second, LimiterConfig{Clock: frozenClock, CleanupInterval: bound.Of(10 * time.Second)})

    allowedBy(limiter, "idle")
    frozenClock.Advance(11 * time.Second)
    allowedBy(limiter, "other")

    limiter.mutex.RLock()
    _, idleHeld := limiter.buckets["idle"]
    limiter.mutex.RUnlock()

    if true == idleHeld {
        t.Fatalf("expected the idle key forgotten after the declared cleanup interval")
    }
}

func TestFixedWindowLimiter_RestoresTheWholeAllowanceAtTheWindowEdge(t *testing.T) {
    clockInstance := clock.NewFrozenClock(time.Unix(0, 0).UTC())
    limiter := NewFixedWindowLimiter("test", 100, time.Minute, LimiterConfig{Clock: clockInstance})

    admittedBeforeEdge := 0
    for index := 0; index < 100; index++ {
        if true == allowedBy(limiter, "client") {
            admittedBeforeEdge++
        }
    }

    if 100 != admittedBeforeEdge {
        t.Fatalf("expected the whole allowance before the edge, got %d", admittedBeforeEdge)
    }

    if true == allowedBy(limiter, "client") {
        t.Fatalf("expected the allowance to be spent before the edge")
    }

    clockInstance.Advance(time.Minute)

    admittedAfterEdge := 0
    for index := 0; index < 100; index++ {
        if true == allowedBy(limiter, "client") {
            admittedAfterEdge++
        }
    }

    if 100 != admittedAfterEdge {
        t.Fatalf("expected the whole allowance to be restored at the edge, got %d", admittedAfterEdge)
    }
}

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
    fixedLimiter := NewFixedWindowLimiter("test", 100, time.Minute, LimiterConfig{Clock: fixedClock})
    fixedAdmitted := spendHalfThenCountAtEdge(func(key string) bool { return allowedBy(fixedLimiter, key) }, fixedClock.Advance)

    slidingClock := clock.NewFrozenClock(time.Unix(0, 0).UTC())
    slidingLimiter := NewSlidingWindowLimiter("test", 100, time.Minute, LimiterConfig{Clock: slidingClock})
    slidingAdmitted := spendHalfThenCountAtEdge(func(key string) bool { return allowedBy(slidingLimiter, key) }, slidingClock.Advance)

    if 100 != fixedAdmitted {
        t.Fatalf("expected the fixed window to restore the whole allowance at the edge, got %d", fixedAdmitted)
    }

    if 50 != slidingAdmitted {
        t.Fatalf("expected the sliding window to free only the aged-out half, got %d", slidingAdmitted)
    }
}

func allowingNext() httpcontract.Handler {
    return func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        return http.TextResponse(200, "ok"), nil
    }
}

func TestIpRateLimit_ChargesTheForwardedClient(t *testing.T) {
    resolver := NewForwardedClientIpResolver(trustingPolicy("10.0.0.0/8"))
    handler := IpRateLimit(1, QuickLimitConfig{ClientIpResolver: resolver})(allowingNext())

    _, firstErr := handler(rateLimitTestRuntime(), httptest.NewRecorder(), forwardedRequest("10.0.0.1:5555", "203.0.113.7"))
    if nil != firstErr {
        t.Fatalf("the first client's only request must pass, got: %v", firstErr)
    }

    _, secondErr := handler(rateLimitTestRuntime(), httptest.NewRecorder(), forwardedRequest("10.0.0.1:6666", "203.0.113.8"))
    if nil != secondErr {
        t.Fatalf("a different client behind the same proxy must have its own budget, got: %v", secondErr)
    }

    _, thirdErr := handler(rateLimitTestRuntime(), httptest.NewRecorder(), forwardedRequest("10.0.0.1:7777", "203.0.113.7"))
    if nil == thirdErr {
        t.Fatalf("the first client's second request must exhaust its own budget")
    }
}

func TestIpRateLimit_StillChargesTheDirectPeer(t *testing.T) {
    handler := IpRateLimit(1, QuickLimitConfig{})(allowingNext())

    _, firstErr := handler(rateLimitTestRuntime(), httptest.NewRecorder(), forwardedRequest("10.0.0.1:5555", "203.0.113.7"))
    if nil != firstErr {
        t.Fatalf("the first request must pass, got: %v", firstErr)
    }

    _, secondErr := handler(rateLimitTestRuntime(), httptest.NewRecorder(), forwardedRequest("10.0.0.1:6666", "203.0.113.8"))
    if nil == secondErr {
        t.Fatalf("without a resolver both clients key the direct peer and share one budget")
    }
}

func TestSimpleRateLimit_ChargesTheForwardedClient(t *testing.T) {
    resolver := NewForwardedClientIpResolver(trustingPolicy("10.0.0.0/8"))
    handler := SimpleRateLimit(1, QuickLimitConfig{ClientIpResolver: resolver})(allowingNext())

    _, firstErr := handler(rateLimitTestRuntime(), httptest.NewRecorder(), forwardedRequest("10.0.0.1:5555", "203.0.113.7"))
    if nil != firstErr {
        t.Fatalf("the first client's only request must pass, got: %v", firstErr)
    }

    _, secondErr := handler(rateLimitTestRuntime(), httptest.NewRecorder(), forwardedRequest("10.0.0.1:6666", "203.0.113.8"))
    if nil != secondErr {
        t.Fatalf("a different client behind the same proxy must have its own budget, got: %v", secondErr)
    }

    _, thirdErr := handler(rateLimitTestRuntime(), httptest.NewRecorder(), forwardedRequest("10.0.0.1:7777", "203.0.113.7"))
    if nil == thirdErr {
        t.Fatalf("the first client's second request must exhaust its own budget")
    }
}

func TestUserRateLimit_ChargesTheForwardedClientWhenAnonymous(t *testing.T) {
    resolver := NewForwardedClientIpResolver(trustingPolicy("10.0.0.0/8"))
    anonymous := func(request httpcontract.Request) string { return "" }
    handler := UserRateLimit(1, anonymous, UserLimitConfig{ClientIpResolver: resolver})(allowingNext())

    _, firstErr := handler(rateLimitTestRuntime(), httptest.NewRecorder(), forwardedRequest("10.0.0.1:5555", "203.0.113.7"))
    if nil != firstErr {
        t.Fatalf("the first client's only request must pass, got: %v", firstErr)
    }

    _, secondErr := handler(rateLimitTestRuntime(), httptest.NewRecorder(), forwardedRequest("10.0.0.1:6666", "203.0.113.8"))
    if nil != secondErr {
        t.Fatalf("a different anonymous client behind the same proxy must have its own budget, got: %v", secondErr)
    }

    _, thirdErr := handler(rateLimitTestRuntime(), httptest.NewRecorder(), forwardedRequest("10.0.0.1:7777", "203.0.113.7"))
    if nil == thirdErr {
        t.Fatalf("the first client's second request must exhaust its own budget")
    }
}

func TestUserRateLimit_KeepsKeyingIdentifiedUsersById(t *testing.T) {
    resolver := NewForwardedClientIpResolver(trustingPolicy("10.0.0.0/8"))
    identified := func(request httpcontract.Request) string { return "user-1" }
    handler := UserRateLimit(1, identified, UserLimitConfig{ClientIpResolver: resolver})(allowingNext())

    _, firstErr := handler(rateLimitTestRuntime(), httptest.NewRecorder(), forwardedRequest("10.0.0.1:5555", "203.0.113.7"))
    if nil != firstErr {
        t.Fatalf("the user's only request must pass, got: %v", firstErr)
    }

    _, secondErr := handler(rateLimitTestRuntime(), httptest.NewRecorder(), forwardedRequest("10.0.0.1:6666", "203.0.113.8"))
    if nil == secondErr {
        t.Fatalf("one user reaching the service from two addresses must still share one budget")
    }
}

func TestQuickRateLimits_NilResolverKeepsTheDirectPeer(t *testing.T) {
    handler := IpRateLimit(1, QuickLimitConfig{})(allowingNext())

    _, firstErr := handler(rateLimitTestRuntime(), httptest.NewRecorder(), forwardedRequest("10.0.0.1:5555", "203.0.113.7"))
    if nil != firstErr {
        t.Fatalf("the first request must pass, got: %v", firstErr)
    }

    _, secondErr := handler(rateLimitTestRuntime(), httptest.NewRecorder(), forwardedRequest("10.0.0.1:6666", "203.0.113.8"))
    if nil == secondErr {
        t.Fatalf("a nil resolver must key the direct peer, so both clients share one budget")
    }
}

func TestFixedWindowLimiter_MaxDurationWindowSurvivesIdlePrune(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Now())
    limiter := NewFixedWindowLimiter("test", 1, time.Duration(math.MaxInt64), LimiterConfig{Clock: frozenClock})

    if false == allowedBy(limiter, "client") {
        t.Fatalf("the single budgeted request must be allowed")
    }

    frozenClock.Advance(limiter.cleanupInterval + time.Second)

    if true == allowedBy(limiter, "client") {
        t.Fatalf("a never-refilling window must not refill after the idle cleanup")
    }
}

func TestSlidingWindowLimiter_MaxDurationWindowSurvivesIdlePrune(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Now())
    limiter := NewSlidingWindowLimiter("test", 1, time.Duration(math.MaxInt64), LimiterConfig{Clock: frozenClock})

    if false == allowedBy(limiter, "client") {
        t.Fatalf("the single budgeted request must be allowed")
    }

    frozenClock.Advance(limiter.cleanupInterval + time.Second)

    if true == allowedBy(limiter, "client") {
        t.Fatalf("a never-refilling window must not refill after the idle cleanup")
    }
}

func TestSimpleRateLimit_ChargesTheDirectPeer(t *testing.T) {
    handler := SimpleRateLimit(1, QuickLimitConfig{})(allowingNext())

    _, firstErr := handler(rateLimitTestRuntime(), httptest.NewRecorder(), forwardedRequest("10.0.0.1:5555", "203.0.113.7"))
    if nil != firstErr {
        t.Fatalf("the first request must pass, got: %v", firstErr)
    }

    _, secondErr := handler(rateLimitTestRuntime(), httptest.NewRecorder(), forwardedRequest("10.0.0.1:6666", "203.0.113.8"))
    if nil == secondErr {
        t.Fatalf("without a resolver both clients key the direct peer and share one budget")
    }
}

func TestSimpleRateLimit_RefusesWithTooManyRequests(t *testing.T) {
    handler := SimpleRateLimit(1, QuickLimitConfig{})(allowingNext())

    peer := func() httpcontract.Request {
        request := httptest.NewRequest(nethttp.MethodGet, "/x", nil)
        request.RemoteAddr = "203.0.113.9:5555"

        return testhelper.NewHttpTestRequestFromHttpRequest(request)
    }

    if _, firstErr := handler(rateLimitTestRuntime(), httptest.NewRecorder(), peer()); nil != firstErr {
        t.Fatalf("the first request must pass, got: %v", firstErr)
    }

    _, secondErr := handler(rateLimitTestRuntime(), httptest.NewRecorder(), peer())
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

func TestQuickRateLimits_RefuseAMissingRequiredArgumentByName(t *testing.T) {
    userId := func(request httpcontract.Request) string { return "" }

    cases := []struct {
        name     string
        callback func()
        message  string
        argument string
    }{
        {"simple budget of zero", func() { SimpleRateLimit(0, QuickLimitConfig{}) }, "the limit is below its minimum", "requestsPerMinute"},
        {"ip budget below zero", func() { IpRateLimit(-1, QuickLimitConfig{}) }, "the limit is below its minimum", "requestsPerMinute"},
        {"user budget of zero", func() { UserRateLimit(0, userId, UserLimitConfig{}) }, "the limit is below its minimum", "requestsPerMinute"},
        {"nil user id callback", func() { UserRateLimit(1, nil, UserLimitConfig{}) }, "get user id callback is required for user rate limit middleware", "userId"},
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            defer func() {
                var refusal *exception.Error
                recoveredErr, isError := recover().(error)
                if false == isError || false == errors.As(recoveredErr, &refusal) {
                    t.Fatalf("expected the refusal %q, got %v", testCase.message, recoveredErr)
                }

                if testCase.message != refusal.Message() || testCase.argument != refusal.Context()["argument"] {
                    t.Fatalf("expected %q naming the %s argument, got %q %v", testCase.message, testCase.argument, refusal.Message(), refusal.Context())
                }
            }()

            testCase.callback()
        })
    }
}

func TestRateLimitMiddleware_AResolverSetAfterTheBuildDoesNotChangeTheKey(t *testing.T) {
    limiter := NewFixedWindowLimiter("test", 1, time.Minute, LimiterConfig{Clock: clock.NewFrozenClock(time.Now())})
    config := RateLimitConfig{}
    handler := RateLimitMiddleware(limiter, config)(allowingNext())

    config.ClientIpResolver = func(request httpcontract.Request) string { return "203.0.113.9" }

    request := httptest.NewRequest(nethttp.MethodGet, "/x", nil)
    request.RemoteAddr = "10.0.0.1:1234"

    if _, err := handler(rateLimitTestRuntime(), httptest.NewRecorder(), testhelper.NewHttpTestRequestFromHttpRequest(request)); nil != err {
        t.Fatalf("expected the first request admitted, got %v", err)
    }

    if true == allowedBy(limiter, "10.0.0.1") {
        t.Fatalf("expected the request charged to the direct peer, the key the middleware was built with")
    }

    if false == allowedBy(limiter, "203.0.113.9") {
        t.Fatalf("expected the resolver written after the build never to have been charged")
    }
}

func TestLimiters_ANilClockReadsTheSystemClock(t *testing.T) {
    if nil == NewFixedWindowLimiter("test", 1, time.Minute, LimiterConfig{}).clockInstance {
        t.Fatalf("expected the fixed window limiter to read the system clock")
    }

    sliding := NewSlidingWindowLimiter("test", 1, time.Minute, LimiterConfig{})
    if nil == sliding.clockInstance {
        t.Fatalf("expected the sliding window limiter to read the system clock")
    }

    if false == allowedBy(sliding, "a") || true == allowedBy(sliding, "a") {
        t.Fatalf("expected the system-clock limiter to admit one request and refuse the next")
    }
}

func TestUserRateLimit_KeysOnTheIdentityRatherThanTheAddress(t *testing.T) {
    identity := "alice"
    getUserId := func(request httpcontract.Request) string { return identity }

    handler := UserRateLimit(1, getUserId, UserLimitConfig{})(allowingNext())

    _, firstErr := handler(rateLimitTestRuntime(), httptest.NewRecorder(), forwardedRequest("10.0.0.1:5555", ""))
    if nil != firstErr {
        t.Fatalf("the first request must pass, got: %v", firstErr)
    }

    _, sameUserErr := handler(rateLimitTestRuntime(), httptest.NewRecorder(), forwardedRequest("198.51.100.4:9999", ""))
    if nil == sameUserErr {
        t.Fatalf("the same identity arriving from another address must carry the same budget")
    }

    identity = "bob"

    _, otherUserErr := handler(rateLimitTestRuntime(), httptest.NewRecorder(), forwardedRequest("10.0.0.1:5555", ""))
    if nil != otherUserErr {
        t.Fatalf("a different identity on the same address must have its own budget, got: %v", otherUserErr)
    }
}

func TestUserRateLimit_FallsBackToTheAddressWhenAnonymous(t *testing.T) {
    anonymous := func(request httpcontract.Request) string { return "" }

    handler := UserRateLimit(1, anonymous, UserLimitConfig{})(allowingNext())

    _, firstErr := handler(rateLimitTestRuntime(), httptest.NewRecorder(), forwardedRequest("10.0.0.1:5555", ""))
    if nil != firstErr {
        t.Fatalf("the first anonymous request must pass, got: %v", firstErr)
    }

    _, otherAddressErr := handler(rateLimitTestRuntime(), httptest.NewRecorder(), forwardedRequest("198.51.100.4:9999", ""))
    if nil != otherAddressErr {
        t.Fatalf("another anonymous client must have its own budget rather than share one empty key, got: %v", otherAddressErr)
    }

    _, sameAddressErr := handler(rateLimitTestRuntime(), httptest.NewRecorder(), forwardedRequest("10.0.0.1:6666", ""))
    if nil == sameAddressErr {
        t.Fatalf("the same anonymous address must exhaust its own budget")
    }
}

func TestRateLimitMiddleware_AnswersTheSilentLimitHandlerWith429(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Now())
    limiter := NewFixedWindowLimiter("test", 1, time.Minute, LimiterConfig{Clock: frozenClock})

    silentHandler := func(request httpcontract.Request, decision httpcontract.RateLimitDecision) (httpcontract.Response, error) {
        return nil, nil
    }

    config := RateLimitConfig{OnLimitExceeded: silentHandler}
    middleware := RateLimitMiddleware(limiter, config)

    next := func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        return http.TextResponse(200, "ok"), nil
    }

    handler := middleware(next)

    request := httptest.NewRequest(nethttp.MethodGet, "/test", nil)
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    if _, firstErr := handler(rateLimitTestRuntime(), httptest.NewRecorder(), melodyRequest); nil != firstErr {
        t.Fatalf("unexpected error on the allowed request: %v", firstErr)
    }

    response, err := handler(rateLimitTestRuntime(), httptest.NewRecorder(), melodyRequest)
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

func TestRateLimitMiddleware_RefusesANilLimiterByName(t *testing.T) {
    assertPanicsNamingArgument(t, func() { RateLimitMiddleware(nil, RateLimitConfig{}) }, "limiter is required for rate limit middleware", "limiter", "RateLimitMiddleware")
}

func TestRateLimitMiddleware_ACancelledLimiterCallIsRecordedAtWarning(t *testing.T) {
    capture := &rateLimitCaptureLogger{}

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    scope := serviceContainer.NewScope()
    scope.MustOverrideProtectedInstance(logging.ServiceLogger, capture, container.OverrideOptions{})
    runtimeInstance := runtime.NewRequest(context.Background(), scope)

    limiter := &cancellingRuntimeLimiter{}
    config := RateLimitConfig{}
    middleware := RateLimitMiddleware(limiter, config)

    handler := middleware(
        func(
            innerRuntime containercontract.Runtime,
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

func (instance *cancellingRuntimeLimiter) Allow(runtimeInstance containercontract.Runtime, key string, _ httpcontract.AllowOptions) (httpcontract.RateLimitDecision, error) {
    return httpcontract.RateLimitDecision{Allowed: true}, context.Canceled
}

func (instance *cancellingRuntimeLimiter) Reset(runtimeInstance containercontract.Runtime, key string) error {
    return nil
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

func (instance *alreadyReportingRuntimeLimiter) Reset(runtimeInstance containercontract.Runtime, key string) error {
    return nil
}

func (instance *alreadyReportingRuntimeLimiter) Allow(runtimeInstance containercontract.Runtime, key string, _ httpcontract.AllowOptions) (httpcontract.RateLimitDecision, error) {
    instance.allowWithRuntimeCalls++

    return httpcontract.RateLimitDecision{Allowed: true}, exception.MarkLogged(
        exception.NewError("rate limiter store failure", exceptioncontract.Context{"key": "actor"}, nil),
    )
}

func TestRateLimitMiddleware_AFailureTheLimiterAlreadyRecordedIsNotRecordedAgain(t *testing.T) {
    capture := &rateLimitCaptureLogger{}

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    scope := serviceContainer.NewScope()
    scope.MustOverrideProtectedInstance(logging.ServiceLogger, capture, container.OverrideOptions{})
    runtimeInstance := runtime.NewRequest(context.Background(), scope)

    limiter := &alreadyReportingRuntimeLimiter{}
    middleware := RateLimitMiddleware(limiter, RateLimitConfig{})

    handler := middleware(
        func(
            innerRuntime containercontract.Runtime,
            writer nethttp.ResponseWriter,
            request httpcontract.Request,
        ) (httpcontract.Response, error) {
            return nil, nil
        },
    )

    request := testhelper.NewHttpTestRequest(nethttp.MethodGet, "http://example.com/limited")
    _, _ = handler(runtimeInstance, httptest.NewRecorder(), request)

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

func TestRateLimitMiddleware_RefusesATypedNilLimiterByName(t *testing.T) {
    assertPanicsNamingArgument(t, func() { RateLimitMiddleware((*FixedWindowLimiter)(nil), RateLimitConfig{}) }, "limiter is required for rate limit middleware", "limiter", "RateLimitMiddleware")
}

func TestRegisterRateLimitRequestListener_RefusesATypedNilLimiterByName(t *testing.T) {
    assertPanicsNamingArgument(t, func() { RegisterRateLimitRequestListener(nil, (*FixedWindowLimiter)(nil), RateLimitConfig{}) }, "limiter is required for rate limit request listener", "limiter", "RegisterRateLimitRequestListener")
}

func TestSlidingWindowLimiter_AClockMovedBackwardsNeverReplenishesTheBudget(t *testing.T) {
    startedAt := time.Now()
    frozenClock := clock.NewFrozenClock(startedAt)
    limiter := NewSlidingWindowLimiter("test", 2, 12 * time.Second, LimiterConfig{Clock: frozenClock})

    frozenClock.TravelTo(startedAt.Add(20 * time.Second))
    if false == allowedBy(limiter, "key1") {
        t.Fatal("expected the first request to be admitted")
    }

    frozenClock.TravelTo(startedAt.Add(5 * time.Second))
    if false == allowedBy(limiter, "key1") {
        t.Fatal("expected the second request to be admitted; the budget is two")
    }

    /* the mark at +20s is still inside the twelve second window that opens at +18s, so the budget is spent */
    frozenClock.TravelTo(startedAt.Add(30 * time.Second))
    if true == allowedBy(limiter, "key1") {
        t.Fatal("expected the refusal: the live mark must not be cut away by a search over unordered marks")
    }
}

func TestDefaultKeyExtractor_AggregatesAnIpv6ClientToItsSlash64(t *testing.T) {
    limiter := NewFixedWindowLimiter("test", 5, time.Minute, LimiterConfig{})
    config := RateLimitConfig{}
    allowedCount := 0
    for index := 1; index <= 100; index++ {
        request := httptest.NewRequest(nethttp.MethodGet, "/api/data", nil)
        request.RemoteAddr = fmt.Sprintf("[2001:db8:1:2::%x]:4711", index)

        if true == allowedBy(limiter, resolveRateLimitConfig(limiter, config, "", "").keyExtractor(testhelper.NewHttpTestRequestFromHttpRequest(request))) {
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

        key := resolveRateLimitConfig(limiter, config, "", "").keyExtractor(testhelper.NewHttpTestRequestFromHttpRequest(request))
        if testCase.expectedKey != key {
            t.Fatalf("expected the key of %q to be %q, got %q", testCase.remoteAddress, testCase.expectedKey, key)
        }
    }
}

func TestRateLimitConfig_ClientIpResolver_AnIpv6AddressItResolvesKeysOnItsSlash64(t *testing.T) {
    request := httptest.NewRequest(nethttp.MethodGet, "/api/data", nil)
    request.RemoteAddr = "10.0.0.1:4711"
    request.Header.Set("X-Forwarded-For", "2001:db8:aa:bb:cc:dd:ee:ff")

    limiter := NewFixedWindowLimiter("test", 5, time.Minute, LimiterConfig{})
    config := RateLimitConfig{}
    config.ClientIpResolver = NewForwardedClientIpResolver(httpcontract.ForwardedHeadersPolicy{
        TrustForwardedHeaders: true,
        TrustedProxyList:      []string{"10.0.0.0/8"},
    })
    key := resolveRateLimitConfig(limiter, config, "", "").keyExtractor(testhelper.NewHttpTestRequestFromHttpRequest(request))
    if "2001:db8:aa:bb::/64" != key {
        t.Fatalf("expected the forwarded IPv6 client to key on its /64, got %q", key)
    }
}

func TestLimiters_ResolveTheMaxKeys(t *testing.T) {
    if 1_000_000 != NewFixedWindowLimiter("test", 1, time.Minute, LimiterConfig{}).maxKeys {
        t.Fatalf("expected the fixed window limiter's absent ceiling to read 1,000,000")
    }

    if 1_000_000 != NewSlidingWindowLimiter("test", 1, time.Minute, LimiterConfig{}).maxKeys {
        t.Fatalf("expected the sliding window limiter's absent ceiling to read 1,000,000")
    }

    if 3 != NewFixedWindowLimiter("test", 1, time.Minute, LimiterConfig{MaxKeys: bound.Of(3)}).maxKeys {
        t.Fatalf("expected the declared ceiling to be kept")
    }
}

func TestUserRateLimit_AdmitsTheBudgetPerUser(t *testing.T) {
    identified := func(request httpcontract.Request) string { return "42" }
    handler := UserRateLimit(2, identified, UserLimitConfig{})(allowingNext())

    for attempt := 1; attempt <= 3; attempt++ {
        request := httptest.NewRequest(nethttp.MethodGet, "/x", nil)
        _, err := handler(rateLimitTestRuntime(), httptest.NewRecorder(), testhelper.NewHttpTestRequestFromHttpRequest(request))
        if (3 == attempt) != (nil != err) {
            t.Fatalf("expected requests 1 and 2 admitted and 3 refused, attempt %d answered %v", attempt, err)
        }
    }
}

func TestLimiters_AreContainerServicesThroughBothRegistrationForms(t *testing.T) {
    newLimiters := map[string]func() httpcontract.RateLimiter{
        "fixed":   func() httpcontract.RateLimiter { return NewFixedWindowLimiter("test", 5, time.Minute, LimiterConfig{}) },
        "sliding": func() httpcontract.RateLimiter { return NewSlidingWindowLimiter("test", 5, time.Minute, LimiterConfig{}) },
    }

    for name, newLimiter := range newLimiters {
        t.Run(name, func(t *testing.T) {
            serviceContainer := container.NewContainer(container.ContainerConfig{})

            registerErr := container.Register[httpcontract.RateLimiter](serviceContainer, "app.limiter", func(containercontract.Resolver) (httpcontract.RateLimiter, error) {
                return newLimiter(), nil
            }, containercontract.RegisterOptions{TypeRegistrationDisabled: true})
            if nil != registerErr {
                t.Fatalf("unexpected registration error: %v", registerErr)
            }

            if _, resolveErr := serviceContainer.Get(context.Background(), "app.limiter"); nil != resolveErr {
                t.Fatalf("unexpected resolution error: %v", resolveErr)
            }
        })
    }
}

type cyclicChainError struct {
    next error
}

func (instance *cyclicChainError) Error() string {
    return "cyclic link"
}

func (instance *cyclicChainError) Unwrap() error {
    return instance.next
}

func newCyclicChainError() *cyclicChainError {
    first := &cyclicChainError{}
    first.next = &cyclicChainError{next: first}

    return first
}

func assertReturnsOnACyclicChain(t *testing.T, callback func()) {
    t.Helper()

    done := make(chan struct{})
    go func() {
        defer close(done)
        callback()
    }()

    select {
    case <-done:
    case <-time.After(5 * time.Second):
        t.Fatal("expected the call to return on an error chain that reaches itself")
    }
}

type cyclicRuntimeLimiter struct{}

func (instance *cyclicRuntimeLimiter) Allow(runtimeInstance containercontract.Runtime, key string, _ httpcontract.AllowOptions) (httpcontract.RateLimitDecision, error) {
    return httpcontract.RateLimitDecision{Allowed: true}, newCyclicChainError()
}

func (instance *cyclicRuntimeLimiter) Reset(runtimeInstance containercontract.Runtime, key string) error {
    return nil
}

func TestRateLimitMiddleware_ACyclicChainFromTheLimiterIsRecordedAsAFailure(t *testing.T) {
    capture := &rateLimitCaptureLogger{Logger: logging.NewNopLogger()}

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    scope := serviceContainer.NewScope()
    scope.MustOverrideProtectedInstance(logging.ServiceLogger, capture, container.OverrideOptions{})
    runtimeInstance := runtime.NewRequest(context.Background(), scope)

    handler := RateLimitMiddleware(&cyclicRuntimeLimiter{}, RateLimitConfig{})(
        func(
            innerRuntime containercontract.Runtime,
            writer nethttp.ResponseWriter,
            request httpcontract.Request,
        ) (httpcontract.Response, error) {
            return nil, nil
        },
    )

    assertReturnsOnACyclicChain(t, func() {
        _, _ = handler(runtimeInstance, httptest.NewRecorder(), testhelper.NewHttpTestRequest(nethttp.MethodGet, "http://example.com/limited"))
    })

    if 0 != capture.warningCalls || 1 != capture.errorCalls {
        t.Fatalf("expected one error and no warning, got %d warnings %d errors", capture.warningCalls, capture.errorCalls)
    }
}

func newRateLimitCaptureRuntime(capture *rateLimitCaptureLogger) containercontract.Runtime {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    scope := serviceContainer.NewScope()
    scope.MustOverrideProtectedInstance(logging.ServiceLogger, capture, container.OverrideOptions{})

    return runtime.NewRequest(context.Background(), scope)
}

func TestRateLimitMiddleware_AMarkedDenialReturnsTheUnavailableErrorAndSkipsTheLimitHandler(t *testing.T) {
    capture := &rateLimitCaptureLogger{}
    limiter := &fakeRuntimeRateLimiter{allowed: false, err: exception.MarkInfrastructureFailure(exception.NewError("store unreachable", nil, nil))}

    limitHandlerCalls := 0
    nextCalls := 0
    handler := RateLimitMiddleware(limiter, RateLimitConfig{
        OnLimitExceeded: func(request httpcontract.Request, decision httpcontract.RateLimitDecision) (httpcontract.Response, error) {
            limitHandlerCalls++

            return nil, exception.TooManyRequests("too many")
        },
    })(func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        nextCalls++

        return http.TextResponse(200, "ok"), nil
    })

    request := testhelper.NewHttpTestRequest(nethttp.MethodGet, "http://example.com/limited")
    response, err := handler(newRateLimitCaptureRuntime(capture), httptest.NewRecorder(), request)

    if false == exception.AnswersUnavailable(err) || nil != response {
        t.Fatalf("expected the marked denial returned to be answered 503, got %v, %#v", err, response)
    }

    if 0 != limitHandlerCalls || 0 != nextCalls {
        t.Fatalf("expected neither the limit handler nor the handler to run, got %d and %d", limitHandlerCalls, nextCalls)
    }

    if 1 != capture.errorCalls || false == exception.IsAlreadyLogged(err) {
        t.Fatalf("expected the outage filed once and the returned error marked logged, got %d records, logged %v", capture.errorCalls, exception.IsAlreadyLogged(err))
    }
}

func TestRateLimitMiddleware_AnUnmarkedDenialBesideAnErrorStays429(t *testing.T) {
    limiter := &fakeRuntimeRateLimiter{allowed: false, err: exception.NewError("store unreachable", nil, nil)}

    handler := RateLimitMiddleware(limiter, RateLimitConfig{})(func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        return http.TextResponse(200, "ok"), nil
    })

    request := testhelper.NewHttpTestRequest(nethttp.MethodGet, "http://example.com/limited")
    _, err := handler(rateLimitTestRuntime(), httptest.NewRecorder(), request)

    if httpException := exception.AsHttpException(err); nil == httpException || nethttp.StatusTooManyRequests != httpException.StatusCode() || true == exception.AnswersUnavailable(err) {
        t.Fatalf("expected an unmarked error beside a denial to stay 429, got %v", err)
    }
}

func TestRateLimitMiddleware_AMarkedErrorBesideAnAllowanceServesTheRequest(t *testing.T) {
    limiter := &fakeRuntimeRateLimiter{allowed: true, err: exception.MarkInfrastructureFailure(exception.NewError("store unreachable", nil, nil))}

    nextCalls := 0
    handler := RateLimitMiddleware(limiter, RateLimitConfig{})(func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        nextCalls++

        return http.TextResponse(200, "ok"), nil
    })

    request := testhelper.NewHttpTestRequest(nethttp.MethodGet, "http://example.com/limited")
    if _, err := handler(rateLimitTestRuntime(), httptest.NewRecorder(), request); nil != err || 1 != nextCalls {
        t.Fatalf("expected the open mode to serve the request, got %v and %d calls", err, nextCalls)
    }
}

func TestRateLimitMiddleware_AMarkedDenialNobodyRecordedIsNotMarkedLogged(t *testing.T) {
    limiter := &fakeRuntimeRateLimiter{allowed: false, err: exception.MarkInfrastructureFailure(exception.NewError("store unreachable", nil, nil))}

    handler := RateLimitMiddleware(limiter, RateLimitConfig{})(func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        return http.TextResponse(200, "ok"), nil
    })

    request := testhelper.NewHttpTestRequest(nethttp.MethodGet, "http://example.com/limited")
    _, err := handler(rateLimitTestRuntime(), httptest.NewRecorder(), request)

    if false == exception.AnswersUnavailable(err) || true == exception.IsAlreadyLogged(err) {
        t.Fatalf("expected the unrecorded outage returned unavailable and not marked logged, so the kernel files it, got %v, logged %v", err, exception.IsAlreadyLogged(err))
    }
}

func BenchmarkLimiters_KnownKeys(b *testing.B) {
    keys := make([]string, 1_000)
    for index := range keys {
        keys[index] = strconv.Itoa(index)
    }

    for _, subject := range boundedLimiters(1_000_000_000, time.Hour, LimiterConfig{MaxKeys: bound.Of(10_000)}) {
        b.Run(subject.name, func(b *testing.B) {
            b.ReportAllocs()
            for index := 0; index < b.N; index++ {
                allowedBy(subject.limiter, keys[index%len(keys)])
            }
        })
    }
}

func BenchmarkLimiters_UnseenKeysAtAFullTable(b *testing.B) {
    for _, subject := range boundedLimiters(1_000_000_000, time.Hour, LimiterConfig{MaxKeys: bound.Of(10_000)}) {
        for index := 0; index < 10_000; index++ {
            allowedBy(subject.limiter, "warm" + strconv.Itoa(index))
        }

        b.Run(subject.name, func(b *testing.B) {
            b.ReportAllocs()
            for index := 0; index < b.N; index++ {
                allowedBy(subject.limiter, "flood" + strconv.Itoa(index))
            }
        })
    }
}

func TestRateLimiter_TheInProcessLimitersAnswerTheDecision(t *testing.T) {
    for name, build := range map[string]func(clockInstance *clock.FrozenClock) httpcontract.RateLimiter{
        "fixed": func(clockInstance *clock.FrozenClock) httpcontract.RateLimiter {
            return NewFixedWindowLimiter("test", 3, time.Minute, LimiterConfig{Clock: clockInstance})
        },
        "sliding": func(clockInstance *clock.FrozenClock) httpcontract.RateLimiter {
            return NewSlidingWindowLimiter("test", 3, time.Minute, LimiterConfig{Clock: clockInstance})
        },
    } {
        t.Run(name, func(t *testing.T) {
            frozenClock := clock.NewFrozenClock(time.Unix(1_700_000_000, 0))
            limiter := build(frozenClock)
            runtimeInstance := rateLimitTestRuntime()

            for index, expected := range []httpcontract.RateLimitDecision{
                {Allowed: true, Remaining: 2, Limit: 3},
                {Allowed: true, Remaining: 1, Limit: 3},
                {Allowed: true, Remaining: 0, Limit: 3},
            } {
                decision, allowErr := limiter.Allow(runtimeInstance, "client", httpcontract.AllowOptions{})
                if nil != allowErr || expected != decision {
                    t.Fatalf("call %d: expected %+v, got %+v %v", index+1, expected, decision, allowErr)
                }

                frozenClock.Advance(10 * time.Second)
            }

            decision, allowErr := limiter.Allow(runtimeInstance, "client", httpcontract.AllowOptions{})
            if nil != allowErr || false != decision.Allowed || 0 != decision.Remaining || 3 != decision.Limit {
                t.Fatalf("expected the fourth call denied, got %+v %v", decision, allowErr)
            }

            if 30*time.Second != decision.RetryAfter {
                t.Fatalf("expected the denial to answer the remainder of the window, 30s, got %s", decision.RetryAfter)
            }
        })
    }
}

func TestRateLimiter_ADenialAnswersTheRemainderOfTheWindow(t *testing.T) {
    fixedClock := clock.NewFrozenClock(time.Unix(1_700_000_000, 0))
    fixed := NewFixedWindowLimiter("test", 1, time.Minute, LimiterConfig{Clock: fixedClock})
    allowedBy(fixed, "client")
    fixedClock.Advance(45 * time.Second)

    if decision, _ := fixed.Allow(rateLimitTestRuntime(), "client", httpcontract.AllowOptions{}); 15*time.Second != decision.RetryAfter {
        t.Fatalf("expected the fixed window to answer 15s, got %s", decision.RetryAfter)
    }

    slidingClock := clock.NewFrozenClock(time.Unix(1_700_000_000, 0))
    sliding := NewSlidingWindowLimiter("test", 2, time.Minute, LimiterConfig{Clock: slidingClock})
    allowedBy(sliding, "client")
    slidingClock.Advance(20 * time.Second)
    allowedBy(sliding, "client")
    slidingClock.Advance(5 * time.Second)

    if decision, _ := sliding.Allow(rateLimitTestRuntime(), "client", httpcontract.AllowOptions{}); 35*time.Second != decision.RetryAfter {
        t.Fatalf("expected the sliding window to answer the oldest live mark's remainder, 35s, got %s", decision.RetryAfter)
    }
}

type typedNilRuntime struct {
    containercontract.Runtime
}

func TestRateLimiter_AllowRefusesANilRuntime(t *testing.T) {
    var typedNil *typedNilRuntime

    for name, limiter := range map[string]httpcontract.RateLimiter{
        "fixed":   NewFixedWindowLimiter("test", 1, time.Minute, LimiterConfig{}),
        "sliding": NewSlidingWindowLimiter("test", 1, time.Minute, LimiterConfig{}),
    } {
        for runtimeName, runtimeInstance := range map[string]containercontract.Runtime{"nil": nil, "typed nil": typedNil} {
            decision, allowErr := limiter.Allow(runtimeInstance, "client", httpcontract.AllowOptions{})
            if nil == allowErr || true == decision.Allowed || true == exception.AnswersUnavailable(allowErr) {
                t.Fatalf("%s, %s runtime: expected an unmarked refusal and a denial, got %+v %v", name, runtimeName, decision, allowErr)
            }

            if resetErr := limiter.Reset(runtimeInstance, "client"); nil == resetErr || true == exception.AnswersUnavailable(resetErr) {
                t.Fatalf("%s, %s runtime: expected Reset to refuse unmarked, got %v", name, runtimeName, resetErr)
            }
        }

        if true != allowedBy(limiter, "client") {
            t.Fatalf("%s: expected the refused calls to charge nothing", name)
        }
    }
}

func TestRateLimiterContract_NoImplementationIsProbedForAWidening(t *testing.T) {
    var _ httpcontract.RateLimiter = (*FixedWindowLimiter)(nil)
    var _ httpcontract.RateLimiter = (*SlidingWindowLimiter)(nil)

    for _, path := range []string{"rate_limit.go", "rate_limit_listener.go", "../contract/middleware.go"} {
        source, readErr := os.ReadFile(path)
        if nil != readErr {
            t.Fatalf("read %s: %v", path, readErr)
        }

        for _, gone := range []string{"RuntimeRateLimiter", "AllowWithRuntime"} {
            if true == strings.Contains(string(source), gone) {
                t.Fatalf("expected %s gone from %s", gone, path)
            }
        }
    }
}

func limitedRequest() httpcontract.Request {
    return testhelper.NewHttpTestRequestFromHttpRequest(httptest.NewRequest(nethttp.MethodGet, "/limited", nil))
}

func TestRateLimitMiddleware_ADenialAnswers429WithRetryAfter(t *testing.T) {
    limiter := &fakeRuntimeRateLimiter{allowed: false, retryAfter: 1500 * time.Millisecond}

    _, err := RateLimitMiddleware(limiter, RateLimitConfig{})(allowingNext())(rateLimitTestRuntime(), httptest.NewRecorder(), limitedRequest())

    httpException := exception.AsHttpException(err)
    if nil == httpException || nethttp.StatusTooManyRequests != httpException.StatusCode() || 1500*time.Millisecond != httpException.RetryAfter() {
        t.Fatalf("expected a 429 carrying the decision's 1.5s, got %v", err)
    }
}

func TestRateLimitGate_ADenialAnswers429WithRetryAfter(t *testing.T) {
    gate := NewRateLimitGate(&fakeRuntimeRateLimiter{allowed: false, retryAfter: 7 * time.Second}, RateLimitConfig{})
    requestEvent := newRateLimitListenerTestRequestEvent(newRateLimitListenerTestRuntime())

    if false == gate.Apply(newRateLimitListenerTestRuntime(), requestEvent) {
        t.Fatalf("expected the denial to stop the request")
    }

    httpException := exception.AsHttpException(requestEvent.Refusal())
    if nil == httpException || nethttp.StatusTooManyRequests != httpException.StatusCode() || 7*time.Second != httpException.RetryAfter() {
        t.Fatalf("expected a 429 carrying the decision's 7s, got %v", requestEvent.Refusal())
    }
}

func TestRateLimitMiddleware_ADenialTheLimiterCannotTimeCarriesNoRetryAfter(t *testing.T) {
    _, err := RateLimitMiddleware(&fakeRuntimeRateLimiter{allowed: false}, RateLimitConfig{})(allowingNext())(rateLimitTestRuntime(), httptest.NewRecorder(), limitedRequest())

    httpException := exception.AsHttpException(err)
    if nil == httpException || nethttp.StatusTooManyRequests != httpException.StatusCode() || 0 != httpException.RetryAfter() {
        t.Fatalf("expected a 429 with no Retry-After, got %v", err)
    }

    response, _ := RateLimitMiddleware(&fakeRuntimeRateLimiter{allowed: false}, RateLimitConfig{
        OnLimitExceeded: func(request httpcontract.Request, decision httpcontract.RateLimitDecision) (httpcontract.Response, error) {
            return http.TextResponse(nethttp.StatusTooManyRequests, "slow down"), nil
        },
    })(allowingNext())(rateLimitTestRuntime(), httptest.NewRecorder(), limitedRequest())

    if "" != response.Headers().Get("Retry-After") {
        t.Fatalf("expected no Retry-After header, got %q", response.Headers().Get("Retry-After"))
    }
}

func TestRateLimitMiddleware_AResponseOfTheLimitHandlerWithoutRetryAfterGetsTheDecisions(t *testing.T) {
    config := RateLimitConfig{
        OnLimitExceeded: func(request httpcontract.Request, decision httpcontract.RateLimitDecision) (httpcontract.Response, error) {
            return http.TextResponse(nethttp.StatusTooManyRequests, "slow down"), nil
        },
    }

    response, _ := RateLimitMiddleware(&fakeRuntimeRateLimiter{allowed: false, retryAfter: 2100 * time.Millisecond}, config)(allowingNext())(rateLimitTestRuntime(), httptest.NewRecorder(), limitedRequest())
    if "3" != response.Headers().Get("Retry-After") {
        t.Fatalf("expected the decision's Retry-After rounded up to 3, got %q", response.Headers().Get("Retry-After"))
    }

    gate := NewRateLimitGate(&fakeRuntimeRateLimiter{allowed: false, retryAfter: 2100 * time.Millisecond}, config)
    requestEvent := newRateLimitListenerTestRequestEvent(newRateLimitListenerTestRuntime())
    gate.Apply(newRateLimitListenerTestRuntime(), requestEvent)

    if nil == requestEvent.Response() || "3" != requestEvent.Response().Headers().Get("Retry-After") {
        t.Fatalf("expected the gate's response to carry the decision's Retry-After, got %v", requestEvent.Response())
    }

    silent := NewRateLimitGate(&fakeRuntimeRateLimiter{allowed: false, retryAfter: time.Second}, RateLimitConfig{
        OnLimitExceeded: func(request httpcontract.Request, decision httpcontract.RateLimitDecision) (httpcontract.Response, error) {
            return nil, nil
        },
    })
    silentEvent := newRateLimitListenerTestRequestEvent(newRateLimitListenerTestRuntime())
    silent.Apply(newRateLimitListenerTestRuntime(), silentEvent)

    if nil == silentEvent.Response() || "1" != silentEvent.Response().Headers().Get("Retry-After") {
        t.Fatalf("expected the fallback 429 to carry the decision's Retry-After, got %v", silentEvent.Response())
    }
}

func TestRateLimitMiddleware_AResponseOfTheLimitHandlerWithItsOwnRetryAfterKeepsIt(t *testing.T) {
    config := RateLimitConfig{
        OnLimitExceeded: func(request httpcontract.Request, decision httpcontract.RateLimitDecision) (httpcontract.Response, error) {
            response := http.TextResponse(nethttp.StatusTooManyRequests, "slow down")
            http.SetRetryAfter(response, 60*time.Second)

            return response, nil
        },
    }

    response, _ := RateLimitMiddleware(&fakeRuntimeRateLimiter{allowed: false, retryAfter: 2 * time.Second}, config)(allowingNext())(rateLimitTestRuntime(), httptest.NewRecorder(), limitedRequest())
    if "60" != response.Headers().Get("Retry-After") {
        t.Fatalf("expected the handler's own Retry-After kept, got %q", response.Headers().Get("Retry-After"))
    }

    gate := NewRateLimitGate(&fakeRuntimeRateLimiter{allowed: false, retryAfter: 2 * time.Second}, config)
    requestEvent := newRateLimitListenerTestRequestEvent(newRateLimitListenerTestRuntime())
    gate.Apply(newRateLimitListenerTestRuntime(), requestEvent)

    if "60" != requestEvent.Response().Headers().Get("Retry-After") {
        t.Fatalf("expected the gate to keep the handler's own Retry-After, got %q", requestEvent.Response().Headers().Get("Retry-After"))
    }
}

func TestRateLimitMiddleware_TheLimitHandlerReceivesTheDecision(t *testing.T) {
    var received []httpcontract.RateLimitDecision
    config := RateLimitConfig{
        OnLimitExceeded: func(request httpcontract.Request, decision httpcontract.RateLimitDecision) (httpcontract.Response, error) {
            received = append(received, decision)

            return nil, exception.TooManyRequests("")
        },
    }

    limiter := &fakeRuntimeRateLimiter{allowed: false, retryAfter: 4 * time.Second}
    _, _ = RateLimitMiddleware(limiter, config)(allowingNext())(rateLimitTestRuntime(), httptest.NewRecorder(), limitedRequest())
    NewRateLimitGate(limiter, config).Apply(newRateLimitListenerTestRuntime(), newRateLimitListenerTestRequestEvent(newRateLimitListenerTestRuntime()))

    expected := httpcontract.RateLimitDecision{RetryAfter: 4 * time.Second, Limit: 1}
    if 2 != len(received) || expected != received[0] || expected != received[1] {
        t.Fatalf("expected both doors to hand the handler %+v, got %+v", expected, received)
    }
}

func TestRateLimitMiddleware_ACallTheCallersOwnContextEndedIsRecordedAtWarning(t *testing.T) {
    capture := &rateLimitCaptureLogger{}
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    scope := serviceContainer.NewScope()
    scope.MustOverrideProtectedInstance(logging.ServiceLogger, capture, container.OverrideOptions{})
    deadlineContext, cancel := context.WithDeadline(context.Background(), time.Unix(0, 0))
    defer cancel()
    runtimeInstance := runtime.NewRequest(deadlineContext, scope)

    limiter := &fakeRuntimeRateLimiter{allowed: false, err: exception.NewError("deadline", nil, context.DeadlineExceeded)}
    _, _ = RateLimitMiddleware(limiter, RateLimitConfig{})(allowingNext())(runtimeInstance, httptest.NewRecorder(), limitedRequest())

    if 1 != capture.warningCalls || 0 != capture.errorCalls {
        t.Fatalf("expected the caller's own deadline at warning, got %d warnings %d errors", capture.warningCalls, capture.errorCalls)
    }
}

func TestRateLimitMiddleware_AnErrorOfTheLimitHandlerWithoutRetryAfterGetsTheDecisions(t *testing.T) {
    config := RateLimitConfig{
        OnLimitExceeded: func(request httpcontract.Request, decision httpcontract.RateLimitDecision) (httpcontract.Response, error) {
            return nil, exception.TooManyRequests("slow down")
        },
    }

    _, err := RateLimitMiddleware(&fakeRuntimeRateLimiter{allowed: false, retryAfter: 42 * time.Second}, config)(allowingNext())(rateLimitTestRuntime(), httptest.NewRecorder(), limitedRequest())

    httpException := exception.AsHttpException(err)
    if nil == httpException || nethttp.StatusTooManyRequests != httpException.StatusCode() || "slow down" != httpException.Message() || 42*time.Second != httpException.RetryAfter() {
        t.Fatalf("expected the handler's 429 carrying the decision's 42s, got %v", err)
    }

    gate := NewRateLimitGate(&fakeRuntimeRateLimiter{allowed: false, retryAfter: 42 * time.Second}, config)
    requestEvent := newRateLimitListenerTestRequestEvent(newRateLimitListenerTestRuntime())
    gate.Apply(newRateLimitListenerTestRuntime(), requestEvent)

    if gateException := exception.AsHttpException(requestEvent.Refusal()); nil == gateException || 42*time.Second != gateException.RetryAfter() {
        t.Fatalf("expected the gate's refusal carrying the decision's 42s, got %v", requestEvent.Refusal())
    }
}

func TestWithDecisionRetryAfterError_KeepsTheIdentityOfTheRebuiltException(t *testing.T) {
    cause := errors.New("budget spent")
    built := exception.NewHttpError(nethttp.StatusTooManyRequests, exception.HttpErrorConfig{
        Code:       "rate_limited",
        Key:        "error.rate_limited",
        Message:    "slow down",
        Parameters: map[string]any{"limit": 5},
        Cause:      cause,
    })
    built.SetContextValue("route", "login")
    built.MarkAsLogged()

    rebuilt := exception.AsHttpException(withDecisionRetryAfterError(built, httpcontract.RateLimitDecision{RetryAfter: 3 * time.Second}))

    if nil == rebuilt || 3*time.Second != rebuilt.RetryAfter() || "rate_limited" != rebuilt.Code() || "error.rate_limited" != rebuilt.Key() || "slow down" != rebuilt.Message() || 5 != rebuilt.Parameters()["limit"] {
        t.Fatalf("expected the identity kept beside the decision's delay, got %+v", rebuilt)
    }

    if false == errors.Is(rebuilt, cause) || "login" != rebuilt.Context()["route"] || false == rebuilt.AlreadyLogged() {
        t.Fatalf("expected the cause, the context and the logged mark kept, got %v %v %v", rebuilt.CauseErr(), rebuilt.Context(), rebuilt.AlreadyLogged())
    }

    unlogged := exception.AsHttpException(withDecisionRetryAfterError(exception.TooManyRequests("slow down"), httpcontract.RateLimitDecision{RetryAfter: time.Second}))
    if nil == unlogged || true == unlogged.AlreadyLogged() {
        t.Fatalf("expected an unlogged exception to stay unlogged")
    }
}

func TestWithDecisionRetryAfterError_LeavesAnErrorItDoesNotRebuildAsBuilt(t *testing.T) {
    decision := httpcontract.RateLimitDecision{RetryAfter: 3 * time.Second}

    ownDelay := exception.NewHttpError(nethttp.StatusTooManyRequests, exception.HttpErrorConfig{RetryAfter: time.Minute})
    marked := exception.MarkInfrastructureFailure(exception.TooManyRequests("store down"))
    plain := errors.New("refused")
    wrapped := fmt.Errorf("wrapped: %w", exception.TooManyRequests("slow down"))
    var typedNil *exception.HttpException

    for name, limitErr := range map[string]error{"own delay": ownDelay, "unavailable": marked, "plain": plain, "wrapped": wrapped, "typed nil": typedNil} {
        if answered := withDecisionRetryAfterError(limitErr, decision); answered != limitErr {
            t.Fatalf("%s: expected the error as built, got %v", name, answered)
        }
    }

    untimed := exception.TooManyRequests("slow down")
    if answered := withDecisionRetryAfterError(untimed, httpcontract.RateLimitDecision{}); answered != error(untimed) {
        t.Fatalf("expected a decision with no delay to leave the error as built, got %v", answered)
    }
}

func TestFixedWindowLimiter_AClockSteppedBackDeniesForAtMostOneWindow(t *testing.T) {
    startedAt := time.Now()
    frozenClock := clock.NewFrozenClock(startedAt)
    limiter := NewFixedWindowLimiter("test", 1, time.Minute, LimiterConfig{Clock: frozenClock})

    allowedBy(limiter, "client")
    frozenClock.TravelTo(startedAt.Add(-30 * time.Minute))

    decision, _ := limiter.Allow(rateLimitTestRuntime(), "client", httpcontract.AllowOptions{})
    if true == decision.Allowed || time.Minute != decision.RetryAfter {
        t.Fatalf("expected a denial answering one window, got %+v", decision)
    }

    frozenClock.Advance(time.Minute)

    if false == allowedBy(limiter, "client") {
        t.Fatalf("expected the budget restored one window after the moved refill instant")
    }
}

func TestSlidingWindowLimiter_AClockSteppedBackDeniesForAtMostOneWindow(t *testing.T) {
    startedAt := time.Now()
    frozenClock := clock.NewFrozenClock(startedAt)
    limiter := NewSlidingWindowLimiter("test", 1, time.Minute, LimiterConfig{Clock: frozenClock})

    allowedBy(limiter, "client")
    frozenClock.TravelTo(startedAt.Add(-30 * time.Minute))

    decision, _ := limiter.Allow(rateLimitTestRuntime(), "client", httpcontract.AllowOptions{})
    if true == decision.Allowed || time.Minute != decision.RetryAfter {
        t.Fatalf("expected a denial answering at most one window, got %+v", decision)
    }
}

func TestClampRetryAfter_HoldsTheDelayWithinTheWindow(t *testing.T) {
    for _, testCase := range []struct {
        retryAfter time.Duration
        expected   time.Duration
    }{
        {retryAfter: -time.Second, expected: 0},
        {retryAfter: 0, expected: 0},
        {retryAfter: 30 * time.Second, expected: 30 * time.Second},
        {retryAfter: time.Minute, expected: time.Minute},
        {retryAfter: time.Minute + time.Nanosecond, expected: time.Minute},
    } {
        if clamped := clampRetryAfter(testCase.retryAfter, time.Minute); testCase.expected != clamped {
            t.Fatalf("expected %s clamped to %s, got %s", testCase.retryAfter, testCase.expected, clamped)
        }
    }
}

func TestRateLimit_ADenialThroughTheKernelServesRetryAfterOnTheWire(t *testing.T) {
    for name, configure := range map[string]func(router *http.Router, dispatcher eventcontract.EventDispatcher){
        "middleware": func(router *http.Router, dispatcher eventcontract.EventDispatcher) {
            router.Handle("/limited", RateLimitMiddleware(&fakeRuntimeRateLimiter{allowed: false, retryAfter: 2100 * time.Millisecond}, RateLimitConfig{})(allowingNext()), http.RouteOptions{Methods: []string{nethttp.MethodGet}})
        },
        "listener": func(router *http.Router, dispatcher eventcontract.EventDispatcher) {
            router.Handle("/limited", allowingNext(), http.RouteOptions{Methods: []string{nethttp.MethodGet}})
            RegisterRateLimitRequestListener(dispatcher, &fakeRuntimeRateLimiter{allowed: false, retryAfter: 2100 * time.Millisecond}, RateLimitConfig{})
        },
        "handler error": func(router *http.Router, dispatcher eventcontract.EventDispatcher) {
            router.Handle("/limited", RateLimitMiddleware(&fakeRuntimeRateLimiter{allowed: false, retryAfter: 2100 * time.Millisecond}, RateLimitConfig{
                OnLimitExceeded: func(request httpcontract.Request, decision httpcontract.RateLimitDecision) (httpcontract.Response, error) {
                    return nil, exception.TooManyRequests("slow down")
                },
            })(allowingNext()), http.RouteOptions{Methods: []string{nethttp.MethodGet}})
        },
    } {
        handler := servedRateLimitKernel(t, configure)

        recorder := httptest.NewRecorder()
        handler.ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/limited", nil))

        if nethttp.StatusTooManyRequests != recorder.Code || "3" != recorder.Header().Get("Retry-After") {
            t.Fatalf("%s: expected 429 with Retry-After 3 on the wire, got %d %q", name, recorder.Code, recorder.Header().Get("Retry-After"))
        }
    }
}
