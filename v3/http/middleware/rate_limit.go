package middleware

import (
    "context"
    "errors"
    "fmt"
    "math"
    "net"
    nethttp "net/http"
    "sort"
    "sync"
    "time"

    "github.com/precision-soft/melody/v3/clock"
    clockcontract "github.com/precision-soft/melody/v3/clock/contract"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/precision-soft/melody/v3/http"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal"
    "github.com/precision-soft/melody/v3/logging"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

const defaultMaxRateLimitKeys = 1_000_000

/* NewFixedWindowLimiter builds a limiter whose counters live in THIS process and nowhere else, which is the one thing to weigh before it guards anything that matters. The map is built at construction and dies with the process, so every restart hands each caller a full budget back — under a supervisor that restarts quickly, a limit of five per hour becomes five per restart — and it is not shared across replicas, so a limit of five is five per instance and the deployment enforces five times the number of them. That is the right trade for shaping ordinary traffic and the wrong one for login, one-time-password or password-reset routes, where the limit is a security control: those want a store the whole deployment sees, which is what integrations/rueidis.RateLimiter is — the distributed drop-in for these limiters. Melody says the same thing at boot about the two other defaults that live in the process, its cache backend and its session storage; it cannot say it about a limiter, because a limiter is wired by the application rather than by the framework. */
func NewFixedWindowLimiter(rate int, window time.Duration) *FixedWindowLimiter {
    return NewFixedWindowLimiterWithClock(clock.NewSystemClock(), rate, window)
}

/* Deprecated: use FixedWindowLimiter. The limiter refills to full capacity at the window edge rather than proportionally to elapsed time, so it is a fixed-window counter and admits up to twice the rate across an instant straddling that edge; use SlidingWindowLimiter where the rate must hold over every trailing window. */
type TokenBucketLimiter = FixedWindowLimiter

/* Deprecated: use NewFixedWindowLimiter. */
func NewTokenBucketLimiter(rate int, window time.Duration) *FixedWindowLimiter {
    return NewFixedWindowLimiter(rate, window)
}

/* Deprecated: use NewFixedWindowLimiterWithClock. */
func NewTokenBucketLimiterWithClock(clockInstance clockcontract.Clock, rate int, window time.Duration) *FixedWindowLimiter {
    return NewFixedWindowLimiterWithClock(clockInstance, rate, window)
}

func NewFixedWindowLimiterWithClock(clockInstance clockcontract.Clock, rate int, window time.Duration) *FixedWindowLimiter {
    if true == internal.IsNilInterface(clockInstance) {
        exception.Panic(
            exception.NewError("clock is required for fixed window limiter", nil, nil),
        )
    }

    if 0 >= rate {
        rate = 1
    }
    if 0 >= window {
        window = time.Minute
    }

    limiter := &FixedWindowLimiter{
        buckets:         make(map[string]*fixedWindowBucket),
        rate:            rate,
        window:          window,
        capacity:        rate,
        clockInstance:   clockInstance,
        cleanupInterval: 5 * time.Minute,
        maxKeys:         defaultMaxRateLimitKeys,
    }

    return limiter
}

type FixedWindowLimiter struct {
    mutex              sync.RWMutex
    buckets            map[string]*fixedWindowBucket
    rate               int
    window             time.Duration
    capacity           int
    clockInstance      clockcontract.Clock
    cleanupInterval    time.Duration
    lastCleanupAt      time.Time
    maxKeys            int
    lastCeilingPruneAt time.Time
}

/* SetMaxKeys bounds how many distinct keys the limiter tracks. When the map is full and an idle-entry prune frees nothing, a request under an unseen key is denied rather than minting a bucket, so an attacker varying the key cannot grow the map without bound. A non-positive value is ignored. */
func (instance *FixedWindowLimiter) SetMaxKeys(maxKeys int) {
    if 0 >= maxKeys {
        return
    }

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.maxKeys = maxKeys
}

type fixedWindowBucket struct {
    tokens     int
    lastRefill time.Time
}

func (instance *FixedWindowLimiter) Allow(key string) bool {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    now := instance.clockInstance.Now()

    instance.cleanupIfNeededLocked(now)

    bucket, exists := instance.buckets[key]
    if false == exists {
        if instance.maxKeys <= len(instance.buckets) {
            instance.pruneAtCeilingLocked(now)
        }

        if instance.maxKeys <= len(instance.buckets) {
            return false
        }

        bucket = &fixedWindowBucket{
            tokens:     instance.capacity,
            lastRefill: now,
        }
        instance.buckets[key] = bucket
    }

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

func (instance *FixedWindowLimiter) Reset(key string) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    delete(instance.buckets, key)
}

func (instance *FixedWindowLimiter) Close() error {
    return nil
}

func (instance *FixedWindowLimiter) cleanupIfNeededLocked(now time.Time) {
    if true == instance.lastCleanupAt.IsZero() {
        instance.lastCleanupAt = now
        return
    }

    if instance.cleanupInterval > now.Sub(instance.lastCleanupAt) {
        return
    }

    instance.pruneIdleLocked(now)

    instance.lastCleanupAt = now
}

func (instance *FixedWindowLimiter) pruneAtCeilingLocked(now time.Time) {
    if false == instance.lastCeilingPruneAt.IsZero() && instance.window > now.Sub(instance.lastCeilingPruneAt) {
        return
    }

    instance.lastCeilingPruneAt = now

    instance.pruneIdleLocked(now)
}

func (instance *FixedWindowLimiter) pruneIdleLocked(now time.Time) {
    idleThreshold := idlePruneThreshold(instance.window)

    for key, bucket := range instance.buckets {
        if idleThreshold < now.Sub(bucket.lastRefill) {
            delete(instance.buckets, key)
        }
    }
}

func idlePruneThreshold(window time.Duration) time.Duration {
    if window > math.MaxInt64/2 {
        return math.MaxInt64
    }

    return window * 2
}

var _ httpcontract.RateLimiter = (*FixedWindowLimiter)(nil)

/* NewSlidingWindowLimiter holds its timestamps in THIS process, exactly as NewFixedWindowLimiter holds its counters: a restart returns every caller's full budget and each replica enforces the limit on its own, so the deployment allows the limit times the number of instances. Where the limit is a security control rather than traffic shaping, use the distributed drop-in in integrations/rueidis. */
func NewSlidingWindowLimiter(limit int, window time.Duration) *SlidingWindowLimiter {
    return NewSlidingWindowLimiterWithClock(clock.NewSystemClock(), limit, window)
}

func NewSlidingWindowLimiterWithClock(clockInstance clockcontract.Clock, limit int, window time.Duration) *SlidingWindowLimiter {
    if true == internal.IsNilInterface(clockInstance) {
        exception.Panic(
            exception.NewError("clock is required for sliding window limiter", nil, nil),
        )
    }

    if 0 >= limit {
        limit = 1
    }
    if 0 >= window {
        window = time.Minute
    }

    limiter := &SlidingWindowLimiter{
        windows:         make(map[string]*slidingWindow),
        limit:           limit,
        window:          window,
        clockInstance:   clockInstance,
        cleanupInterval: 5 * time.Minute,
        maxKeys:         defaultMaxRateLimitKeys,
    }

    return limiter
}

type SlidingWindowLimiter struct {
    mutex              sync.RWMutex
    windows            map[string]*slidingWindow
    limit              int
    window             time.Duration
    clockInstance      clockcontract.Clock
    cleanupInterval    time.Duration
    lastCleanupAt      time.Time
    maxKeys            int
    lastCeilingPruneAt time.Time
}

/* SetMaxKeys bounds how many distinct keys the limiter tracks. When the map is full and an idle-entry prune frees nothing, a request under an unseen key is denied rather than minting a window, so an attacker varying the key cannot grow the map without bound. A non-positive value is ignored. */
func (instance *SlidingWindowLimiter) SetMaxKeys(maxKeys int) {
    if 0 >= maxKeys {
        return
    }

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.maxKeys = maxKeys
}

type slidingWindow struct {
    requests []time.Time
}

func (instance *SlidingWindowLimiter) Allow(key string) bool {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    now := instance.clockInstance.Now()

    instance.cleanupIfNeededLocked(now)

    windowStart := now.Add(-instance.window)

    window, exists := instance.windows[key]
    if false == exists {
        if instance.maxKeys <= len(instance.windows) {
            instance.pruneAtCeilingLocked(now)
        }

        if instance.maxKeys <= len(instance.windows) {
            return false
        }

        window = &slidingWindow{
            requests: make([]time.Time, 0),
        }
        instance.windows[key] = window
    }

    liveFrom := sort.Search(
        len(window.requests),
        func(index int) bool {
            return window.requests[index].After(windowStart)
        },
    )

    if 0 < liveFrom {
        window.requests = window.requests[liveFrom:]
    }

    if instance.limit <= len(window.requests) {
        return false
    }

    recordedAt := now
    if 0 < len(window.requests) {
        lastMark := window.requests[len(window.requests)-1]
        if true == lastMark.After(recordedAt) {
            recordedAt = lastMark
        }
    }

    window.requests = append(window.requests, recordedAt)

    return true
}

func (instance *SlidingWindowLimiter) Reset(key string) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    delete(instance.windows, key)
}

func (instance *SlidingWindowLimiter) Close() error {
    return nil
}

func (instance *SlidingWindowLimiter) cleanupIfNeededLocked(now time.Time) {
    if true == instance.lastCleanupAt.IsZero() {
        instance.lastCleanupAt = now
        return
    }

    if instance.cleanupInterval > now.Sub(instance.lastCleanupAt) {
        return
    }

    instance.pruneIdleLocked(now)

    instance.lastCleanupAt = now
}

func (instance *SlidingWindowLimiter) pruneAtCeilingLocked(now time.Time) {
    if false == instance.lastCeilingPruneAt.IsZero() && instance.window > now.Sub(instance.lastCeilingPruneAt) {
        return
    }

    instance.lastCeilingPruneAt = now

    instance.pruneIdleLocked(now)
}

func (instance *SlidingWindowLimiter) pruneIdleLocked(now time.Time) {
    idleThreshold := idlePruneThreshold(instance.window)

    for key, window := range instance.windows {
        if 0 == len(window.requests) {
            delete(instance.windows, key)
            continue
        }

        lastRequest := window.requests[len(window.requests)-1]
        if idleThreshold < now.Sub(lastRequest) {
            delete(instance.windows, key)
        }
    }
}

var _ httpcontract.RateLimiter = (*SlidingWindowLimiter)(nil)

type KeyExtractor = func(httpcontract.Request) string

type OnLimitExceeded = func(httpcontract.Request) (httpcontract.Response, error)

type ClientIpResolver = func(httpcontract.Request) string

func DefaultClientIp(request httpcontract.Request) string {
    host, _, splitErr := net.SplitHostPort(request.HttpRequest().RemoteAddr)
    if nil != splitErr {
        return request.HttpRequest().RemoteAddr
    }

    return host
}

type RateLimitConfig struct {
    limiter          httpcontract.RateLimiter
    keyExtractor     KeyExtractor
    onLimitExceeded  OnLimitExceeded
    clientIpResolver ClientIpResolver
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

func (instance *RateLimitConfig) ClientIpResolver() ClientIpResolver {
    return instance.clientIpResolver
}

func (instance *RateLimitConfig) SetClientIpResolver(resolver ClientIpResolver) {
    instance.clientIpResolver = resolver
}

func (instance *RateLimitConfig) clientIp(request httpcontract.Request) string {
    if nil != instance.clientIpResolver {
        return instance.clientIpResolver(request)
    }

    return DefaultClientIp(request)
}

func RateLimitMiddleware(config *RateLimitConfig) httpcontract.Middleware {

    if nil == config || true == internal.IsNilInterface(config.Limiter()) {
        exception.Panic(
            exception.NewError("limiter is required for rate limit middleware", nil, nil),
        )
    }

    if nil == config.KeyExtractor() {
        config.SetKeyExtractor(func(request httpcontract.Request) string {
            return config.clientIp(request)
        })
    }

    if nil == config.OnLimitExceeded() {
        config.SetOnLimitExceeded(defaultOnLimitExceeded)
    }

    return func(next httpcontract.Handler) httpcontract.Handler {
        return func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            key := config.KeyExtractor()(request)

            allowed := false
            if runtimeLimiter, isRuntimeLimiter := config.Limiter().(httpcontract.RuntimeRateLimiter); true == isRuntimeLimiter {
                var allowErr error
                allowed, allowErr = runtimeLimiter.AllowWithRuntime(runtimeInstance, key)
                if nil != allowErr && false == exception.IsAlreadyLogged(allowErr) {

                    logger := logging.LoggerFromRuntime(runtimeInstance)
                    if nil != logger {
                        if true == errors.Is(allowErr, context.Canceled) {
                            logger.Warning(
                                "rate limiter call cancelled",
                                exception.LogContext(allowErr, exceptioncontract.Context{"key": key}),
                            )
                        } else {
                            logger.Error(
                                "rate limiter store failure",
                                exception.LogContext(allowErr, exceptioncontract.Context{"key": key}),
                            )
                        }
                    }
                }
            } else {
                allowed = config.Limiter().Allow(key)
            }

            if false == allowed {
                response, limitErr := config.OnLimitExceeded()(request)
                if nil != limitErr {
                    return response, limitErr
                }

                if true == internal.IsNilInterface(response) {
                    return http.JsonErrorResponse(nethttp.StatusTooManyRequests, "too many requests"), nil
                }

                return response, nil
            }

            return next(runtimeInstance, writer, request)
        }
    }
}

/* SimpleRateLimit keys on the direct peer address, so behind a reverse proxy every client shares the proxy's single budget. It builds its config internally and returns only the middleware, so the resolver cannot be set afterwards: use SimpleRateLimitWithResolver with NewForwardedClientIpResolver where a trusted edge sits in front. */
func SimpleRateLimit(requestsPerMinute int) httpcontract.Middleware {
    return SimpleRateLimitWithResolver(requestsPerMinute, nil)
}

/* SimpleRateLimitWithResolver is SimpleRateLimit with the client address read through the given resolver — pass NewForwardedClientIpResolver with the same policy handed to Kernel.SetForwardedHeadersPolicy so a per-IP budget behind a reverse proxy is charged to the client rather than to the proxy. A nil resolver keeps the direct peer. */
func SimpleRateLimitWithResolver(
    requestsPerMinute int,
    clientIpResolver ClientIpResolver,
) httpcontract.Middleware {
    limiter := NewFixedWindowLimiter(requestsPerMinute, time.Minute)

    config := NewRateLimitConfig(
        limiter,
        nil,
        nil,
    )

    config.SetClientIpResolver(clientIpResolver)

    return RateLimitMiddleware(config)
}

/* IpRateLimit keys on the direct peer address, so behind a reverse proxy every client shares the proxy's single budget. It builds its config internally and returns only the middleware, so the resolver cannot be set afterwards: use IpRateLimitWithResolver with NewForwardedClientIpResolver where a trusted edge sits in front. */
func IpRateLimit(requestsPerMinute int) httpcontract.Middleware {
    return IpRateLimitWithResolver(requestsPerMinute, nil)
}

/* IpRateLimitWithResolver is IpRateLimit with the client address read through the given resolver — pass NewForwardedClientIpResolver with the same policy handed to Kernel.SetForwardedHeadersPolicy so a per-IP budget behind a reverse proxy is charged to the client rather than to the proxy. A nil resolver keeps the direct peer. */
func IpRateLimitWithResolver(
    requestsPerMinute int,
    clientIpResolver ClientIpResolver,
) httpcontract.Middleware {
    limiter := NewSlidingWindowLimiter(requestsPerMinute, time.Minute)

    config := NewRateLimitConfig(
        limiter,
        nil,
        nil,
    )

    config.SetClientIpResolver(clientIpResolver)

    config.SetKeyExtractor(func(request httpcontract.Request) string {
        return config.clientIp(request)
    })

    return RateLimitMiddleware(config)
}

/* UserRateLimit falls back to the direct peer address for a request that carries no user id, so behind a reverse proxy every anonymous client shares the proxy's single budget. It builds its config internally and returns only the middleware, so the resolver cannot be set afterwards: use UserRateLimitWithResolver with NewForwardedClientIpResolver where a trusted edge sits in front. */
func UserRateLimit(
    requestsPerMinute int,
    getUserId KeyExtractor,
) httpcontract.Middleware {
    return UserRateLimitWithResolver(requestsPerMinute, getUserId, nil)
}

/* UserRateLimitWithResolver is UserRateLimit with the anonymous fallback address read through the given resolver — pass NewForwardedClientIpResolver with the same policy handed to Kernel.SetForwardedHeadersPolicy so unauthenticated traffic behind a reverse proxy is charged per client rather than to the proxy. A nil resolver keeps the direct peer. */
func UserRateLimitWithResolver(
    requestsPerMinute int,
    getUserId KeyExtractor,
    clientIpResolver ClientIpResolver,
) httpcontract.Middleware {

    if nil == getUserId {
        exception.Panic(
            exception.NewError("get user id callback is required for user rate limit middleware", nil, nil),
        )
    }

    limiter := NewSlidingWindowLimiter(requestsPerMinute, time.Minute)

    config := NewRateLimitConfig(
        limiter,
        nil,
        nil,
    )

    config.SetClientIpResolver(clientIpResolver)

    config.SetKeyExtractor(func(request httpcontract.Request) string {
        userId := getUserId(request)

        if "" == userId {
            return config.clientIp(request)
        }

        return fmt.Sprintf("user:%s", userId)
    })

    return RateLimitMiddleware(config)
}

func defaultOnLimitExceeded(request httpcontract.Request) (httpcontract.Response, error) {
    return nil, exception.TooManyRequests("Rate limit exceeded. Please try again later.")
}
