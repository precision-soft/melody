package middleware

import (
    "container/list"
    "context"
    "fmt"
    "math"
    "net"
    nethttp "net/http"
    "net/netip"
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

/* NewFixedWindowLimiter counts in this process only; for a security control use integrations/rueidis.RateLimiter */
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
    recency            *keyRecency
}

/* SetEvictOldestKeyAtCeiling on gives the oldest key's bucket to an unseen key; off, the default, it is denied */
func (instance *FixedWindowLimiter) SetEvictOldestKeyAtCeiling(evict bool) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if false == evict {
        instance.recency = nil

        return
    }

    if nil != instance.recency {
        return
    }

    keyList := make([]string, 0, len(instance.buckets))
    for key := range instance.buckets {
        keyList = append(keyList, key)
    }
    sort.Slice(keyList, func(left int, right int) bool {
        return instance.buckets[keyList[left]].lastRefill.Before(instance.buckets[keyList[right]].lastRefill)
    })

    instance.recency = newKeyRecency()
    for _, key := range keyList {
        instance.recency.touch(key)
    }
}

/* SetMaxKeys bounds the tracked keys, so varying the key cannot grow the map; a non-positive value is ignored */
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

        for instance.maxKeys <= len(instance.buckets) && nil != instance.recency {
            oldestKey, held := instance.recency.oldest()
            if false == held {
                break
            }

            instance.deleteLocked(oldestKey)
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

    if nil != instance.recency {
        instance.recency.touch(key)
    }

    /* a fixed window: the allowance is restored whole at the edge, so up to twice the rate can pass across it */
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

    instance.deleteLocked(key)
}

func (instance *FixedWindowLimiter) deleteLocked(key string) {
    delete(instance.buckets, key)

    if nil != instance.recency {
        instance.recency.forget(key)
    }
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
            instance.deleteLocked(key)
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

/* NewSlidingWindowLimiter holds its marks in this process only; for a security control use integrations/rueidis */
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
    recency            *keyRecency
}

/* SetEvictOldestKeyAtCeiling on gives the oldest key's window to an unseen key; off, the default, it is denied */
func (instance *SlidingWindowLimiter) SetEvictOldestKeyAtCeiling(evict bool) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if false == evict {
        instance.recency = nil

        return
    }

    if nil != instance.recency {
        return
    }

    lastMarkOf := func(key string) time.Time {
        requests := instance.windows[key].requests
        if 0 == len(requests) {
            return time.Time{}
        }

        return requests[len(requests)-1]
    }

    keyList := make([]string, 0, len(instance.windows))
    for key := range instance.windows {
        keyList = append(keyList, key)
    }
    sort.Slice(keyList, func(left int, right int) bool {
        return lastMarkOf(keyList[left]).Before(lastMarkOf(keyList[right]))
    })

    instance.recency = newKeyRecency()
    for _, key := range keyList {
        instance.recency.touch(key)
    }
}

/* SetMaxKeys bounds the tracked keys, so varying the key cannot grow the map; a non-positive value is ignored */
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

        for instance.maxKeys <= len(instance.windows) && nil != instance.recency {
            oldestKey, held := instance.recency.oldest()
            if false == held {
                break
            }

            instance.deleteLocked(oldestKey)
        }

        if instance.maxKeys <= len(instance.windows) {
            return false
        }

        window = &slidingWindow{
            requests: make([]time.Time, 0),
        }
        instance.windows[key] = window
    }

    if nil != instance.recency {
        instance.recency.touch(key)
    }

    /* the recorded instant is clamped to the last mark: a clock stepping back breaks the order the search needs */
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

    instance.deleteLocked(key)
}

func (instance *SlidingWindowLimiter) deleteLocked(key string) {
    delete(instance.windows, key)

    if nil != instance.recency {
        instance.recency.forget(key)
    }
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
            instance.deleteLocked(key)
            continue
        }

        lastRequest := window.requests[len(window.requests)-1]
        if idleThreshold < now.Sub(lastRequest) {
            instance.deleteLocked(key)
        }
    }
}

var _ httpcontract.RateLimiter = (*SlidingWindowLimiter)(nil)

type keyRecency struct {
    order    *list.List
    elements map[string]*list.Element
}

func newKeyRecency() *keyRecency {
    return &keyRecency{
        order:    list.New(),
        elements: make(map[string]*list.Element),
    }
}

func (instance *keyRecency) touch(key string) {
    if element, held := instance.elements[key]; true == held {
        instance.order.MoveToFront(element)

        return
    }

    instance.elements[key] = instance.order.PushFront(key)
}

func (instance *keyRecency) forget(key string) {
    element, held := instance.elements[key]
    if false == held {
        return
    }

    instance.order.Remove(element)
    delete(instance.elements, key)
}

func (instance *keyRecency) oldest() (string, bool) {
    element := instance.order.Back()
    if nil == element {
        return "", false
    }

    return element.Value.(string), true
}

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

/* RateLimitConfig is read on every request, not copied: a setter called afterwards changes the middleware in flight */
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
            allowed := allowRequestUnderLimit(config, runtimeInstance, request)

            if false == allowed {
                response, limitErr := config.OnLimitExceeded()(request)
                if nil != limitErr {
                    return response, limitErr
                }

                /* a limit handler that answered neither response nor error still refused the request */
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

func allowRequestUnderLimit(config *RateLimitConfig, runtimeInstance runtimecontract.Runtime, request httpcontract.Request) bool {
    key := config.KeyExtractor()(request)

    runtimeLimiter, isRuntimeLimiter := config.Limiter().(httpcontract.RuntimeRateLimiter)
    if false == isRuntimeLimiter {
        return config.Limiter().Allow(key)
    }

    allowed, allowErr := runtimeLimiter.AllowWithRuntime(runtimeInstance, key)
    if nil != allowErr && false == exception.IsAlreadyLogged(allowErr) {
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

    return allowed
}
