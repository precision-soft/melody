package middleware

import (
    "container/list"
    "context"
    "errors"
    "fmt"
    "math"
    "net"
    nethttp "net/http"
    "net/netip"
    "sort"
    "strconv"
    "sync"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/clock"
    clockcontract "github.com/precision-soft/melody/v4/clock/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/http"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal"
    "github.com/precision-soft/melody/v4/logging"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
)

const defaultMaxRateLimitKeys = 1_000_000

/* LimiterConfig is what an in-process limiter declares. Limit and Window are required, the budget and the span it is counted over; the rest has a default. The limiter copies it when it is built. */
type LimiterConfig struct {
    /* Limit is the requests admitted per Window, at least 1; a limit of 0 or below is refused, since it would deny all traffic. */
    Limit int

    /* Window is the span the limit is counted over, at least 1ms; a window below it is refused, since it would refill on every request and disable the limiter. */
    Window time.Duration

    /* MaxKeys bounds how many distinct keys the limiter tracks, so a caller varying the key cannot grow the table without bound: 1,000,000 by default, at least 1, unlimited refused. When the table is full and the idle-entry prune, run at most once per window, frees nothing or is skipped, a request under an unseen key is denied, unless EvictOldestKeyAtCeiling is set. */
    MaxKeys bound.Count

    /* CleanupInterval is how often the limiter forgets the keys idle past their window: 5m by default, at least 1ms, unlimited refused, so an idle key is always forgotten. */
    CleanupInterval bound.Duration

    /* EvictOldestKeyAtCeiling decides what a full table answers an unseen key. Off, the default, the key is denied. On, the key whose last request is the oldest gives up its slot to it: a flood of unseen keys then costs an idle caller its remembered count, never a stranger its admission, which is what a long window needs, since an idle key is pruned only after twice the window. */
    EvictOldestKeyAtCeiling bool

    /* Clock is where the limiter reads the time; nil is the system clock. */
    Clock clockcontract.Clock
}

var limiterMaxKeysRule = bound.Rule[int]{
    Name:    "LimiterConfig.MaxKeys",
    Default: defaultMaxRateLimitKeys,
    Minimum: 1,
}

var limiterCleanupIntervalRule = bound.Rule[time.Duration]{
    Name:    "LimiterConfig.CleanupInterval",
    Default: 5 * time.Minute,
    Minimum: bound.TimeoutMinimum,
}

type limiterSettings struct {
    limit           int
    window          time.Duration
    maxKeys         int
    cleanupInterval time.Duration
    recency         *keyRecency
    clockInstance   clockcontract.Clock
}

func resolveLimiterConfig(config LimiterConfig) limiterSettings {
    refuseBelowMinimum(1 > config.Limit, "LimiterConfig.Limit", strconv.Itoa(config.Limit), "1")
    refuseBelowMinimum(bound.TimeoutMinimum > config.Window, "LimiterConfig.Window", config.Window.String(), bound.TimeoutMinimum.String())

    maxKeys, maxKeysErr := bound.Resolve(config.MaxKeys, limiterMaxKeysRule)
    if nil != maxKeysErr {
        exception.Panic(exception.FromError(maxKeysErr))
    }

    cleanupInterval, cleanupIntervalErr := bound.Resolve(config.CleanupInterval, limiterCleanupIntervalRule)
    if nil != cleanupIntervalErr {
        exception.Panic(exception.FromError(cleanupIntervalErr))
    }

    clockInstance := config.Clock
    if true == internal.IsNilInterface(clockInstance) {
        clockInstance = clock.NewSystemClock()
    }

    var recency *keyRecency
    if true == config.EvictOldestKeyAtCeiling {
        recency = newKeyRecency()
    }

    return limiterSettings{
        limit:           config.Limit,
        window:          config.Window,
        maxKeys:         maxKeys.Value,
        cleanupInterval: cleanupInterval.Value,
        recency:         recency,
        clockInstance:   clockInstance,
    }
}

/* the context keys are bound.Resolve's, so a required value is refused in the form a bound is */
func refuseBelowMinimum(isBelow bool, field string, value string, minimum string) {
    if false == isBelow {
        return
    }

    exception.Panic(
        exception.NewError(
            "the limit is below its minimum",
            exceptioncontract.Context{
                "field":   field,
                "value":   value,
                "minimum": minimum,
            },
            nil,
        ),
    )
}

/* NewFixedWindowLimiter builds a limiter whose counters live in this process only: a restart hands every caller a full budget back, and each replica enforces the limit on its own. Where the limit is a security control, as on login, one-time-password or password-reset routes, use integrations/rueidis.RateLimiter, the distributed drop-in. The bucket refills to full capacity at the window edge, so it admits up to twice the limit across an instant straddling that edge; use NewSlidingWindowLimiter where the limit must hold over every trailing window. */
func NewFixedWindowLimiter(config LimiterConfig) *FixedWindowLimiter {
    settings := resolveLimiterConfig(config)

    return &FixedWindowLimiter{
        buckets:         make(map[string]*fixedWindowBucket),
        rate:            settings.limit,
        window:          settings.window,
        capacity:        settings.limit,
        clockInstance:   settings.clockInstance,
        cleanupInterval: settings.cleanupInterval,
        maxKeys:         settings.maxKeys,
        recency:         settings.recency,
    }
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
    /* recency orders the keys by their last request when EvictOldestKeyAtCeiling is set, nil otherwise */
    recency *keyRecency
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

        /* a ceiling lowered under a full table is reached by evicting until a slot is free, so the stranger is admitted at once rather than one eviction per refused stranger */
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

/* pruneAtCeilingLocked reclaims idle entries when the map is full, at most once per window, so a flood of unseen keys does not pay a whole-map walk under the shared lock on every request; between prunes an unseen key is denied without a walk. */
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

/* idlePruneThreshold is twice the window, saturating at the top of the duration range so the doubling cannot wrap negative. */
func idlePruneThreshold(window time.Duration) time.Duration {
    if window > math.MaxInt64/2 {
        return math.MaxInt64
    }

    return window * 2
}

var _ httpcontract.RateLimiter = (*FixedWindowLimiter)(nil)

/* NewSlidingWindowLimiter holds its timestamps in this process only, as NewFixedWindowLimiter holds its counters. Where the limit is a security control, use the distributed drop-in in integrations/rueidis. */
func NewSlidingWindowLimiter(config LimiterConfig) *SlidingWindowLimiter {
    settings := resolveLimiterConfig(config)

    return &SlidingWindowLimiter{
        windows:         make(map[string]*slidingWindow),
        limit:           settings.limit,
        window:          settings.window,
        clockInstance:   settings.clockInstance,
        cleanupInterval: settings.cleanupInterval,
        maxKeys:         settings.maxKeys,
        recency:         settings.recency,
    }
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
    /* recency orders the keys by their last request when EvictOldestKeyAtCeiling is set, nil otherwise */
    recency *keyRecency
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

        /* a ceiling lowered under a full table is reached by evicting until a slot is free, so the stranger is admitted at once rather than one eviction per refused stranger */
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

    /* the marks are in clock order, so the expired ones are a prefix trimmed by index and append reclaims the vacated head. The recorded instant is clamped to the last mark, since a clock that steps back would break the order the search needs and could drop a live mark; the clamp can only shorten a caller's budget. */
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

/* pruneAtCeilingLocked reclaims idle entries when the map is full, at most once per window, so a flood of unseen keys does not pay a whole-map walk under the shared lock on every request; between prunes an unseen key is denied without a walk. */
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

/* keyRecency orders a limiter's keys by their last request, the most recent in front, so a full table gives up the key used longest ago without a walk of the map */
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

/* RateLimitConfig is what the rate-limit middleware and the request listener declare beside the limiter they take; each door reads it once, when it is built, so a write after that reaches neither. The zero value is the default. */
type RateLimitConfig struct {
    /* KeyExtractor names the budget a request is charged to; nil charges the client address, read through ClientIpResolver. */
    KeyExtractor KeyExtractor

    /* OnLimitExceeded answers a request over budget; nil answers 429 through the framework's exception. */
    OnLimitExceeded OnLimitExceeded

    /* ClientIpResolver reads the client address the default key charges, an IPv6 one on its /64; nil reads the direct peer, so behind a reverse proxy every client shares the proxy's budget: pass NewForwardedClientIpResolver with the kernel's policy there. */
    ClientIpResolver ClientIpResolver
}

type rateLimitSettings struct {
    limiter         httpcontract.RateLimiter
    keyExtractor    KeyExtractor
    onLimitExceeded OnLimitExceeded
}

func resolveRateLimitConfig(limiter httpcontract.RateLimiter, config RateLimitConfig, refusalMessage string, door string) rateLimitSettings {
    if true == internal.IsNilInterface(limiter) {
        exception.Panic(
            exception.NewError(
                refusalMessage,
                exceptioncontract.Context{
                    "argument": "limiter",
                    "door":     door,
                },
                nil,
            ),
        )
    }

    keyExtractor := config.KeyExtractor
    if nil == keyExtractor {
        clientIpResolver := config.ClientIpResolver
        keyExtractor = func(request httpcontract.Request) string {
            return clientIpKey(clientIpResolver, request)
        }
    }

    onLimitExceeded := config.OnLimitExceeded
    if nil == onLimitExceeded {
        onLimitExceeded = defaultOnLimitExceeded
    }

    return rateLimitSettings{
        limiter:         limiter,
        keyExtractor:    keyExtractor,
        onLimitExceeded: onLimitExceeded,
    }
}

/* clientIpKey answers the client key the limiter counts under: the resolved address, with an IPv6 address aggregated to its /64 by clientIpRateLimitKey */
func clientIpKey(clientIpResolver ClientIpResolver, request httpcontract.Request) string {
    if nil != clientIpResolver {
        return clientIpRateLimitKey(clientIpResolver(request))
    }

    return clientIpRateLimitKey(DefaultClientIp(request))
}

/* clientIpRateLimitKey aggregates an IPv6 address to its /64, the block one host is routinely handed: keyed on the /128, one host rotates through 2^64 addresses, each with a budget of its own, and fills the limiter's key table, which then refuses every new client. An IPv4 address, written bare or mapped into IPv6, keys on itself; a value that is not an address is kept as it is. */
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

/* RateLimitMiddleware meters the handler path; a nil or typed-nil limiter is refused, and the configuration is read when the middleware is built. */
func RateLimitMiddleware(limiter httpcontract.RateLimiter, config RateLimitConfig) httpcontract.Middleware {
    /* a typed-nil limiter is refused at boot by name, not dereferenced per request */
    settings := resolveRateLimitConfig(limiter, config, "limiter is required for rate limit middleware", "RateLimitMiddleware")

    return func(next httpcontract.Handler) httpcontract.Handler {
        return func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            allowed := allowRequestUnderLimit(settings, runtimeInstance, request)

            if false == allowed {
                response, limitErr := settings.onLimitExceeded(request)
                if nil != limitErr {
                    return response, limitErr
                }

                /* a limit handler that answered neither response nor error still refused the request, as the listener door reads it */
                if true == internal.IsNilInterface(response) {
                    return http.JsonErrorResponse(nethttp.StatusTooManyRequests, "too many requests"), nil
                }

                return response, nil
            }

            return next(runtimeInstance, writer, request)
        }
    }
}

/* QuickLimitConfig is what the two address-keyed quick doors declare: a budget per client address per minute. */
type QuickLimitConfig struct {
    /* RequestsPerMinute is the budget per client address, at least 1; 0 or below is refused. */
    RequestsPerMinute int

    /* ClientIpResolver reads the address the budget is charged to, an IPv6 one on its /64; nil reads the direct peer, so behind a reverse proxy every client shares the proxy's budget: pass NewForwardedClientIpResolver with the kernel's policy there. */
    ClientIpResolver ClientIpResolver
}

/* UserLimitConfig is what the user-keyed quick door declares: the budget, the user id a request carries, and the address an anonymous request falls back to. */
type UserLimitConfig struct {
    /* RequestsPerMinute is the budget per user, or per client address for a request with no user id, at least 1; 0 or below is refused. */
    RequestsPerMinute int

    /* UserId reads the user id a request is charged to; required, an empty id charges the address. */
    UserId KeyExtractor

    /* ClientIpResolver reads the address an anonymous request is charged to, as QuickLimitConfig.ClientIpResolver does. */
    ClientIpResolver ClientIpResolver
}

func quickLimiterConfig(requestsPerMinute int) LimiterConfig {
    return LimiterConfig{Limit: requestsPerMinute, Window: time.Minute}
}

/* SimpleRateLimit charges each client address a fixed-window budget per minute. */
func SimpleRateLimit(config QuickLimitConfig) httpcontract.Middleware {
    refuseBelowMinimum(1 > config.RequestsPerMinute, "QuickLimitConfig.RequestsPerMinute", strconv.Itoa(config.RequestsPerMinute), "1")

    return RateLimitMiddleware(NewFixedWindowLimiter(quickLimiterConfig(config.RequestsPerMinute)), RateLimitConfig{
        ClientIpResolver: config.ClientIpResolver,
    })
}

/* IpRateLimit charges each client address a sliding-window budget per minute. */
func IpRateLimit(config QuickLimitConfig) httpcontract.Middleware {
    refuseBelowMinimum(1 > config.RequestsPerMinute, "QuickLimitConfig.RequestsPerMinute", strconv.Itoa(config.RequestsPerMinute), "1")

    return RateLimitMiddleware(NewSlidingWindowLimiter(quickLimiterConfig(config.RequestsPerMinute)), RateLimitConfig{
        ClientIpResolver: config.ClientIpResolver,
    })
}

/* UserRateLimit charges each user a sliding-window budget per minute, under the key "user:<id>"; a request with no user id is charged to its client address. */
func UserRateLimit(config UserLimitConfig) httpcontract.Middleware {
    refuseBelowMinimum(1 > config.RequestsPerMinute, "UserLimitConfig.RequestsPerMinute", strconv.Itoa(config.RequestsPerMinute), "1")

    /* a nil callback is refused at construction, as the middleware constructor refuses a missing limiter */
    if nil == config.UserId {
        exception.Panic(
            exception.NewError(
                "get user id callback is required for user rate limit middleware",
                exceptioncontract.Context{
                    "field": "UserLimitConfig.UserId",
                },
                nil,
            ),
        )
    }

    getUserId := config.UserId
    clientIpResolver := config.ClientIpResolver

    return RateLimitMiddleware(NewSlidingWindowLimiter(quickLimiterConfig(config.RequestsPerMinute)), RateLimitConfig{
        KeyExtractor: func(request httpcontract.Request) string {
            userId := getUserId(request)

            if "" == userId {
                return clientIpKey(clientIpResolver, request)
            }

            return fmt.Sprintf("user:%s", userId)
        },
        ClientIpResolver: clientIpResolver,
    })
}

func defaultOnLimitExceeded(request httpcontract.Request) (httpcontract.Response, error) {
    return nil, exception.TooManyRequests("Rate limit exceeded. Please try again later.")
}

/* allowRequestUnderLimit is the metering step the middleware and the listener doors share: the key is extracted, the limiter asked, and a store failure reported once, unless the limiter already marked it logged. The caller's own cancellation is recorded at warning. The returned value already reflects the limiter's failure policy. */
func allowRequestUnderLimit(settings rateLimitSettings, runtimeInstance runtimecontract.Runtime, request httpcontract.Request) bool {
    key := settings.keyExtractor(request)

    runtimeLimiter, isRuntimeLimiter := settings.limiter.(httpcontract.RuntimeRateLimiter)
    if false == isRuntimeLimiter {
        return settings.limiter.Allow(key)
    }

    allowed, allowErr := runtimeLimiter.AllowWithRuntime(runtimeInstance, key)
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

    return allowed
}
