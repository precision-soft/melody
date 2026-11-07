package middleware

import (
    "context"
    "fmt"
    "math"
    "net"
    nethttp "net/http"
    "net/netip"
    "sort"
    "sync"
    "time"

    "github.com/precision-soft/melody/v2/clock"
    clockcontract "github.com/precision-soft/melody/v2/clock/contract"
    "github.com/precision-soft/melody/v2/exception"
    exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
    "github.com/precision-soft/melody/v2/http"
    httpcontract "github.com/precision-soft/melody/v2/http/contract"
    "github.com/precision-soft/melody/v2/internal"
    "github.com/precision-soft/melody/v2/logging"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

const defaultMaxRateLimitKeys = 1_000_000

/* NewFixedWindowLimiter counts in this process only; for a security limit use integrations/rueidis.RateLimiter. */
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

    /* clamped: unclamped, a non-positive window would disable the limiter and a non-positive rate deny all traffic */
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

/* SetMaxKeys denies an unseen key when the map is full and the prune frees nothing or is skipped; non-positive is ignored. */
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

    /* the window is fixed, not a token bucket: up to twice the rate can pass across an instant straddling the edge */
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

/* at most once per window: the prune walks the whole map under the lock all traffic shares */
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

/* saturates at the top of the duration range: past the midpoint the doubling wraps negative and every entry reads idle */
func idlePruneThreshold(window time.Duration) time.Duration {
    if window > math.MaxInt64/2 {
        return math.MaxInt64
    }

    return window * 2
}

var _ httpcontract.RateLimiter = (*FixedWindowLimiter)(nil)

/* NewSlidingWindowLimiter counts in this process only; for a security limit use the drop-in in integrations/rueidis. */
func NewSlidingWindowLimiter(limit int, window time.Duration) *SlidingWindowLimiter {
    return NewSlidingWindowLimiterWithClock(clock.NewSystemClock(), limit, window)
}

func NewSlidingWindowLimiterWithClock(clockInstance clockcontract.Clock, limit int, window time.Duration) *SlidingWindowLimiter {
    if true == internal.IsNilInterface(clockInstance) {
        exception.Panic(
            exception.NewError("clock is required for sliding window limiter", nil, nil),
        )
    }

    /* clamped: unclamped, a non-positive window would disable the limiter and a non-positive limit deny all traffic */
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

/* SetMaxKeys denies an unseen key when the map is full and the prune frees nothing or is skipped; non-positive is ignored. */
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

    /* the recorded instant is clamped to the last mark: a clock that steps back would let the binary search cut a live mark */
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

/* at most once per window: the prune walks the whole map under the lock all traffic shares */
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
        return clientIpRateLimitKey(instance.clientIpResolver(request))
    }

    return clientIpRateLimitKey(DefaultClientIp(request))
}

/* keyed on the /128, one host rotates through 2^64 addresses, each with a budget of its own, and fills the key table */
func clientIpRateLimitKey(clientIp string) string {
    address, parseErr := netip.ParseAddr(clientIp)
    if nil != parseErr {
        return clientIp
    }

    address = address.WithZone("").Unmap()
    if true == address.Is4() {
        return address.String()
    }

    return netip.PrefixFrom(address, 64).Masked().String()
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
                    /* the returned allowed value already reflects the limiter's failure policy; the middleware only reports the store failure */
                    logger := logging.LoggerFromRuntime(runtimeInstance)
                    if nil != logger {
                        if true == exception.ChainIs(allowErr, context.Canceled) {
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

                /* a limit handler with neither response nor error still refused the request: a nil response would be served as success */
                if true == internal.IsNilInterface(response) {
                    return http.JsonErrorResponse(nethttp.StatusTooManyRequests, "too many requests"), nil
                }

                return response, nil
            }

            return next(runtimeInstance, writer, request)
        }
    }
}

/* SimpleRateLimit keys on the direct peer address, so behind a reverse proxy every client shares the proxy's budget. */
func SimpleRateLimit(requestsPerMinute int) httpcontract.Middleware {
    return SimpleRateLimitWithResolver(requestsPerMinute, nil)
}

/* SimpleRateLimitWithResolver keeps the direct peer for a nil resolver. */
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

/* IpRateLimit keys on the direct peer address, so behind a reverse proxy every client shares the proxy's single budget. */
func IpRateLimit(requestsPerMinute int) httpcontract.Middleware {
    return IpRateLimitWithResolver(requestsPerMinute, nil)
}

/* IpRateLimitWithResolver keeps the direct peer for a nil resolver. */
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

/* UserRateLimit falls back to the direct peer address, so behind a reverse proxy anonymous clients share one budget. */
func UserRateLimit(
    requestsPerMinute int,
    getUserId KeyExtractor,
) httpcontract.Middleware {
    return UserRateLimitWithResolver(requestsPerMinute, getUserId, nil)
}

/* UserRateLimitWithResolver keeps the direct peer for a nil resolver. */
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
