package middleware

import (
    "fmt"
    nethttp "net/http"
    "sync"
    "time"

    "github.com/precision-soft/melody/clock"
    clockcontract "github.com/precision-soft/melody/clock/contract"
    "github.com/precision-soft/melody/exception"
    httpcontract "github.com/precision-soft/melody/http/contract"
    "github.com/precision-soft/melody/internal"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
)

func NewTokenBucketLimiter(rate int, window time.Duration) *TokenBucketLimiter {
    return NewTokenBucketLimiterWithClock(clock.NewSystemClock(), rate, window)
}

func NewTokenBucketLimiterWithClock(clockInstance clockcontract.Clock, rate int, window time.Duration) *TokenBucketLimiter {
    if true == internal.IsNilInterface(clockInstance) {
        exception.Panic(
            exception.NewError("clock is required for token bucket limiter", nil, nil),
        )
    }

    limiter := &TokenBucketLimiter{
        buckets:         make(map[string]*tokenBucket),
        rate:            rate,
        window:          window,
        capacity:        rate,
        clockInstance:   clockInstance,
        cleanupInterval: 5 * time.Minute,
    }

    return limiter
}

type TokenBucketLimiter struct {
    mutex           sync.RWMutex
    buckets         map[string]*tokenBucket
    rate            int
    window          time.Duration
    capacity        int
    clockInstance   clockcontract.Clock
    cleanupInterval time.Duration
    lastCleanupAt   time.Time
}

type tokenBucket struct {
    tokens     int
    lastRefill time.Time
}

func (instance *TokenBucketLimiter) Allow(key string) bool {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.cleanupIfNeededLocked()

    bucket, exists := instance.buckets[key]
    if false == exists {
        bucket = &tokenBucket{
            tokens:     instance.capacity,
            lastRefill: instance.clockInstance.Now(),
        }
        instance.buckets[key] = bucket
    }

    now := instance.clockInstance.Now()
    elapsed := now.Sub(bucket.lastRefill)

    if instance.window <= elapsed {
        bucket.tokens = instance.capacity
        bucket.lastRefill = now
    }

    if 0 < bucket.tokens {
        bucket.tokens--

        return true
    }

    return false
}

func (instance *TokenBucketLimiter) Reset(key string) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    delete(instance.buckets, key)
}

func (instance *TokenBucketLimiter) Close() error {
    return nil
}

func (instance *TokenBucketLimiter) cleanupIfNeededLocked() {
    now := instance.clockInstance.Now()

    if true == instance.lastCleanupAt.IsZero() {
        instance.lastCleanupAt = now
        return
    }

    if instance.cleanupInterval > now.Sub(instance.lastCleanupAt) {
        return
    }

    for key, bucket := range instance.buckets {
        if instance.window*2 < now.Sub(bucket.lastRefill) {
            delete(instance.buckets, key)
        }
    }

    instance.lastCleanupAt = now
}

var _ httpcontract.RateLimiter = (*TokenBucketLimiter)(nil)

func NewSlidingWindowLimiter(limit int, window time.Duration) *SlidingWindowLimiter {
    return NewSlidingWindowLimiterWithClock(clock.NewSystemClock(), limit, window)
}

func NewSlidingWindowLimiterWithClock(clockInstance clockcontract.Clock, limit int, window time.Duration) *SlidingWindowLimiter {
    if true == internal.IsNilInterface(clockInstance) {
        exception.Panic(
            exception.NewError("clock is required for sliding window limiter", nil, nil),
        )
    }

    limiter := &SlidingWindowLimiter{
        windows:         make(map[string]*slidingWindow),
        limit:           limit,
        window:          window,
        clockInstance:   clockInstance,
        cleanupInterval: 5 * time.Minute,
    }

    return limiter
}

type SlidingWindowLimiter struct {
    mutex           sync.RWMutex
    windows         map[string]*slidingWindow
    limit           int
    window          time.Duration
    clockInstance   clockcontract.Clock
    cleanupInterval time.Duration
    lastCleanupAt   time.Time
}

type slidingWindow struct {
    requests []time.Time
}

func (instance *SlidingWindowLimiter) Allow(key string) bool {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.cleanupIfNeededLocked()

    now := instance.clockInstance.Now()
    windowStart := now.Add(-instance.window)

    window, exists := instance.windows[key]
    if false == exists {
        window = &slidingWindow{
            requests: make([]time.Time, 0),
        }
        instance.windows[key] = window
    }

    validRequests := make([]time.Time, 0, len(window.requests))

    for _, requestTime := range window.requests {
        if requestTime.After(windowStart) {
            validRequests = append(validRequests, requestTime)
        }
    }

    window.requests = validRequests

    if instance.limit > len(window.requests) {
        window.requests = append(window.requests, now)

        return true
    }

    return false
}

func (instance *SlidingWindowLimiter) Reset(key string) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    delete(instance.windows, key)
}

func (instance *SlidingWindowLimiter) Close() error {
    return nil
}

func (instance *SlidingWindowLimiter) cleanupIfNeededLocked() {
    now := instance.clockInstance.Now()

    if true == instance.lastCleanupAt.IsZero() {
        instance.lastCleanupAt = now
        return
    }

    if instance.cleanupInterval > now.Sub(instance.lastCleanupAt) {
        return
    }

    for key, window := range instance.windows {
        if 0 == len(window.requests) {
            delete(instance.windows, key)
            continue
        }

        lastRequest := window.requests[len(window.requests)-1]
        if instance.window*2 < now.Sub(lastRequest) {
            delete(instance.windows, key)
        }
    }

    instance.lastCleanupAt = now
}

var _ httpcontract.RateLimiter = (*SlidingWindowLimiter)(nil)

type KeyExtractor = func(httpcontract.Request) string

type OnLimitExceeded = func(httpcontract.Request) (httpcontract.Response, error)

type RateLimitConfig struct {
    limiter         httpcontract.RateLimiter
    keyExtractor    KeyExtractor
    onLimitExceeded OnLimitExceeded
}

func NewRateLimitConfig(
    limiter httpcontract.RateLimiter,
    keyExtractor KeyExtractor,
    onLimitExceeded OnLimitExceeded,
) *RateLimitConfig {
    return &RateLimitConfig{limiter: limiter, keyExtractor: keyExtractor, onLimitExceeded: onLimitExceeded}
}

func (instance *RateLimitConfig) Limiter() httpcontract.RateLimiter { return instance.limiter }

func (instance *RateLimitConfig) KeyExtractor() KeyExtractor {
    return instance.keyExtractor
}

func (instance *RateLimitConfig) SetKeyExtractor(keyExtractor KeyExtractor) {
    instance.keyExtractor = keyExtractor
}

func (instance *RateLimitConfig) OnLimitExceeded() OnLimitExceeded {
    return instance.onLimitExceeded
}

func (instance *RateLimitConfig) SetOnLimitExceeded(onLimitExceeded OnLimitExceeded) {
    instance.onLimitExceeded = onLimitExceeded
}

func RateLimitMiddleware(config *RateLimitConfig) httpcontract.Middleware {
    if nil == config.Limiter() {
        exception.Panic(
            exception.NewError("limiter is required for rate limit middleware", nil, nil),
        )
    }

    if nil == config.KeyExtractor() {
        config.SetKeyExtractor(defaultKeyExtractor)
    }

    if nil == config.OnLimitExceeded() {
        config.SetOnLimitExceeded(defaultOnLimitExceeded)
    }

    return func(next httpcontract.Handler) httpcontract.Handler {
        return func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            key := config.KeyExtractor()(request)

            if false == config.Limiter().Allow(key) {
                return config.OnLimitExceeded()(request)
            }

            return next(runtimeInstance, writer, request)
        }
    }
}

func SimpleRateLimit(requestsPerMinute int) httpcontract.Middleware {
    limiter := NewTokenBucketLimiter(requestsPerMinute, time.Minute)

    return RateLimitMiddleware(
        NewRateLimitConfig(
            limiter,
            defaultKeyExtractor,
            nil,
        ),
    )
}

func IpRateLimit(requestsPerMinute int) httpcontract.Middleware {
    limiter := NewSlidingWindowLimiter(requestsPerMinute, time.Minute)

    return RateLimitMiddleware(
        NewRateLimitConfig(
            limiter,
            ipKeyExtractor,
            nil,
        ),
    )
}

func UserRateLimit(
    requestsPerMinute int,
    getUserId KeyExtractor,
) httpcontract.Middleware {
    limiter := NewSlidingWindowLimiter(requestsPerMinute, time.Minute)

    return RateLimitMiddleware(
        NewRateLimitConfig(
            limiter,
            func(request httpcontract.Request) string {
                userId := getUserId(request)

                if "" == userId {
                    return ipKeyExtractor(request)
                }

                return fmt.Sprintf("user:%s", userId)
            },
            nil,
        ),
    )
}

func defaultKeyExtractor(request httpcontract.Request) string {
    ip := getClientIp(request)

    return fmt.Sprintf("%s:%s", ip, request.HttpRequest().URL.Path)
}

func ipKeyExtractor(request httpcontract.Request) string {
    return getClientIp(request)
}

func getClientIp(request httpcontract.Request) string {
    forwarded := request.HttpRequest().Header.Get("X-Forwarded-For")
    if "" != forwarded {
        return forwarded
    }

    realIp := request.HttpRequest().Header.Get("X-Real-IP")
    if "" != realIp {
        return realIp
    }

    return request.HttpRequest().RemoteAddr
}

func defaultOnLimitExceeded(request httpcontract.Request) (httpcontract.Response, error) {
    return nil, exception.TooManyRequests("Rate limit exceeded. Please try again later.")
}
