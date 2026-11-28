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
    "strconv"
    "sync"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/clock"
    clockcontract "github.com/precision-soft/melody/v4/clock/contract"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/http"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal"
    "github.com/precision-soft/melody/v4/logging"
)

const defaultMaxRateLimitKeys = 1_000_000

/* LimiterConfig is what an in-process limiter declares beside its name, limit and window; every field has a default. The limiter copies it when it is built. */
type LimiterConfig struct {
    /* MaxKeys bounds how many distinct keys the limiter tracks, so a caller varying the key cannot grow the table without bound: 1,000,000 by default, at least 1, unlimited refused. A full table gives the slot of the key whose last request is the oldest to an unseen key, never denies it: a caller minting keys gains nothing a fresh key does not already give it, and a caller who cannot mint the key it is limited by resets it only by sending MaxKeys requests under other keys after its last one. */
    MaxKeys bound.Count

    /* CleanupInterval is how often the limiter forgets the keys idle past their window: 5m by default, at least 1ms, unlimited refused, so an idle key is always forgotten. */
    CleanupInterval bound.Duration

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
    name            string
    limit           int
    window          time.Duration
    maxKeys         int
    cleanupInterval time.Duration
    clockInstance   clockcontract.Clock
}

func resolveLimiterConfig(name string, limit int, window time.Duration, config LimiterConfig) limiterSettings {
    refuseMalformedLimiterName(name)
    refuseArgumentBelowMinimum(1 > limit, "limit", name, strconv.Itoa(limit), "1")
    refuseArgumentBelowMinimum(bound.TimeoutMinimum > window, "window", name, window.String(), bound.TimeoutMinimum.String())

    maxKeys, maxKeysErr := bound.Resolve(config.MaxKeys, limiterMaxKeysRule)
    if nil != maxKeysErr {
        exception.Panic(exception.FromError(maxKeysErr))
    }

    cleanupInterval, cleanupIntervalErr := bound.Resolve(config.CleanupInterval, limiterCleanupIntervalRule)
    if nil != cleanupIntervalErr {
        exception.Panic(exception.FromError(cleanupIntervalErr))
    }

    clockInstance, clockErr := clock.ResolveOptionalClock(config.Clock, "rate limiter", "LimiterConfig.Clock")
    if nil != clockErr {
        exception.Panic(exception.FromError(clockErr))
    }

    return limiterSettings{
        name:            name,
        limit:           limit,
        window:          window,
        maxKeys:         maxKeys.Value,
        cleanupInterval: cleanupInterval.Value,
        clockInstance:   clockInstance,
    }
}

/* value and minimum are written as bound.Resolve writes them, the value named by its argument */
func refuseBelowMinimum(isBelow bool, argument string, value string, minimum string) {
    if false == isBelow {
        return
    }

    exception.Panic(
        exception.NewError(
            "the limit is below its minimum",
            exceptioncontract.Context{
                "argument": argument,
                "value":    value,
                "minimum":  minimum,
            },
            nil,
        ),
    )
}

const limiterNameMaximumLength = 64

/* the colon is left out so a counter key <name>:<key> splits one way only */
func refuseMalformedLimiterName(name string) {
    wellFormed := 0 < len(name) && limiterNameMaximumLength >= len(name)

    for index := 0; true == wellFormed && index < len(name); index++ {
        character := name[index]
        wellFormed = ('a' <= character && 'z' >= character) ||
            ('A' <= character && 'Z' >= character) ||
            ('0' <= character && '9' >= character) ||
            '.' == character || '_' == character || '-' == character
    }

    if true == wellFormed {
        return
    }

    exception.Panic(
        exception.NewError(
            "the limiter name is malformed",
            exceptioncontract.Context{
                "argument": "name",
                "value":    name,
                "grammar":  "[A-Za-z0-9._-]{1,64}",
            },
            nil,
        ),
    )
}

func refuseArgumentBelowMinimum(isBelow bool, argument string, name string, value string, minimum string) {
    if false == isBelow {
        return
    }

    exception.Panic(
        exception.NewError(
            "the limit is below its minimum",
            exceptioncontract.Context{
                "argument": argument,
                "limiter":  name,
                "value":    value,
                "minimum":  minimum,
            },
            nil,
        ),
    )
}

/* NewFixedWindowLimiter builds a limiter whose counters live in this process only: a restart hands every caller a full budget back, and each replica enforces the limit on its own. Where the limit is a security control, as on login, one-time-password or password-reset routes, use integrations/rueidis.RateLimiter, the distributed drop-in. The bucket refills to full capacity at the window edge, so it admits up to twice the limit across an instant straddling that edge; use NewSlidingWindowLimiter where the limit must hold over every trailing window. */
func NewFixedWindowLimiter(name string, limit int, window time.Duration, config LimiterConfig) *FixedWindowLimiter {
    settings := resolveLimiterConfig(name, limit, window, config)

    return &FixedWindowLimiter{
        name:            settings.name,
        buckets:         make(map[string]*fixedWindowBucket),
        rate:            settings.limit,
        window:          settings.window,
        capacity:        settings.limit,
        clockInstance:   settings.clockInstance,
        cleanupInterval: settings.cleanupInterval,
        maxKeys:         settings.maxKeys,
        recency:         newKeyRecency(),
    }
}

type FixedWindowLimiter struct {
    name            string
    mutex           sync.RWMutex
    buckets         map[string]*fixedWindowBucket
    rate            int
    window          time.Duration
    capacity        int
    clockInstance   clockcontract.Clock
    cleanupInterval time.Duration
    lastCleanupAt   time.Time
    maxKeys         int
    recency *keyRecency
}

type fixedWindowBucket struct {
    tokens     int
    lastRefill time.Time
}

func (instance *FixedWindowLimiter) Name() string {
    return instance.name
}

func (instance *FixedWindowLimiter) Allow(runtimeInstance containercontract.Runtime, key string, _ httpcontract.AllowOptions) (httpcontract.RateLimitDecision, error) {
    if refusalErr := containercontract.RefuseNilRuntime(runtimeInstance, "middleware.FixedWindowLimiter.Allow"); nil != refusalErr {
        return httpcontract.RateLimitDecision{Limit: instance.capacity}, refusalErr
    }

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    now := instance.clockInstance.Now()

    instance.cleanupIfNeededLocked(now)

    bucket, exists := instance.buckets[key]
    if false == exists {
        for instance.maxKeys <= len(instance.buckets) {
            oldestKey, held := instance.recency.oldest()
            if false == held {
                break
            }

            instance.deleteLocked(oldestKey)
        }

        bucket = &fixedWindowBucket{
            tokens:     instance.capacity,
            lastRefill: now,
        }
        instance.buckets[key] = bucket
    }

    instance.recency.touch(key)

    /* a fixed window: the allowance is restored whole at the edge, so up to twice the rate can pass across it */
    elapsed := now.Sub(bucket.lastRefill)

    /* a clock stepped back moves the refill instant to now, so a denial never waits past one window */
    if 0 > elapsed {
        bucket.lastRefill = now
        elapsed = 0
    }

    if instance.window <= elapsed {
        bucket.tokens = instance.capacity
        bucket.lastRefill = now
    }

    if 0 < bucket.tokens {
        bucket.tokens--

        return httpcontract.RateLimitDecision{Allowed: true, Remaining: bucket.tokens, Limit: instance.capacity}, nil
    }

    return httpcontract.RateLimitDecision{RetryAfter: clampRetryAfter(instance.window-elapsed, instance.window), Limit: instance.capacity}, nil
}

func (instance *FixedWindowLimiter) Reset(runtimeInstance containercontract.Runtime, key string) error {
    if refusalErr := containercontract.RefuseNilRuntime(runtimeInstance, "middleware.FixedWindowLimiter.Reset"); nil != refusalErr {
        return refusalErr
    }

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.deleteLocked(key)

    return nil
}

func (instance *FixedWindowLimiter) deleteLocked(key string) {
    delete(instance.buckets, key)

    instance.recency.forget(key)
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
func NewSlidingWindowLimiter(name string, limit int, window time.Duration, config LimiterConfig) *SlidingWindowLimiter {
    settings := resolveLimiterConfig(name, limit, window, config)

    return &SlidingWindowLimiter{
        name:            settings.name,
        windows:         make(map[string]*slidingWindow),
        limit:           settings.limit,
        window:          settings.window,
        clockInstance:   settings.clockInstance,
        cleanupInterval: settings.cleanupInterval,
        maxKeys:         settings.maxKeys,
        recency:         newKeyRecency(),
    }
}

type SlidingWindowLimiter struct {
    name            string
    mutex           sync.RWMutex
    windows         map[string]*slidingWindow
    limit           int
    window          time.Duration
    clockInstance   clockcontract.Clock
    cleanupInterval time.Duration
    lastCleanupAt   time.Time
    maxKeys         int
    recency *keyRecency
}

type slidingWindow struct {
    requests []time.Time
}

func (instance *SlidingWindowLimiter) Name() string {
    return instance.name
}

func (instance *SlidingWindowLimiter) Allow(runtimeInstance containercontract.Runtime, key string, _ httpcontract.AllowOptions) (httpcontract.RateLimitDecision, error) {
    if refusalErr := containercontract.RefuseNilRuntime(runtimeInstance, "middleware.SlidingWindowLimiter.Allow"); nil != refusalErr {
        return httpcontract.RateLimitDecision{Limit: instance.limit}, refusalErr
    }

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    now := instance.clockInstance.Now()

    instance.cleanupIfNeededLocked(now)

    windowStart := now.Add(-instance.window)

    window, exists := instance.windows[key]
    if false == exists {
        for instance.maxKeys <= len(instance.windows) {
            oldestKey, held := instance.recency.oldest()
            if false == held {
                break
            }

            instance.deleteLocked(oldestKey)
        }

        window = &slidingWindow{
            requests: make([]time.Time, 0),
        }
        instance.windows[key] = window
    }

    instance.recency.touch(key)

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
        /* the oldest mark may be the clamped later instant of a clock stepped back */
        return httpcontract.RateLimitDecision{RetryAfter: clampRetryAfter(window.requests[0].Add(instance.window).Sub(now), instance.window), Limit: instance.limit}, nil
    }

    recordedAt := now
    if 0 < len(window.requests) {
        lastMark := window.requests[len(window.requests)-1]
        if true == lastMark.After(recordedAt) {
            recordedAt = lastMark
        }
    }

    window.requests = append(window.requests, recordedAt)

    return httpcontract.RateLimitDecision{Allowed: true, Remaining: instance.limit - len(window.requests), Limit: instance.limit}, nil
}

func (instance *SlidingWindowLimiter) Reset(runtimeInstance containercontract.Runtime, key string) error {
    if refusalErr := containercontract.RefuseNilRuntime(runtimeInstance, "middleware.SlidingWindowLimiter.Reset"); nil != refusalErr {
        return refusalErr
    }

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.deleteLocked(key)

    return nil
}

func (instance *SlidingWindowLimiter) deleteLocked(key string) {
    delete(instance.windows, key)

    instance.recency.forget(key)
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

/* OnLimitExceeded answers a request the limiter denied, handed the decision; a response it answers without a Retry-After header is served with the decision's. */
type OnLimitExceeded = func(request httpcontract.Request, decision httpcontract.RateLimitDecision) (httpcontract.Response, error)

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

    /* OnLimitExceeded answers a request over budget; nil answers 429 through the framework's exception, with the decision's Retry-After. */
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

/* clientIpRateLimitKey aggregates an IPv6 address to its /64, the block one host is routinely handed: keyed on the /128, one host rotates through 2^64 addresses, each with a budget of its own, and fills the limiter's key table, evicting every other client's count. An IPv4 address, written bare or mapped into IPv6, keys on itself; a value that is not an address is kept as it is. */
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
        return func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            decision, unavailableErr := allowRequestUnderLimit(settings, runtimeInstance, request)

            /* a denial from a store that could not answer is the limiter's outage, not the client's budget: it is answered 503 with Retry-After and the limit handler is not called */
            if nil != unavailableErr {
                return nil, unavailableErr
            }

            if false == decision.Allowed {
                response, limitErr := settings.onLimitExceeded(request, decision)
                if nil != limitErr {
                    return response, withDecisionRetryAfterError(limitErr, decision)
                }

                /* a limit handler that answered neither response nor error still refused the request, as the listener door reads it */
                if true == internal.IsNilInterface(response) {
                    response = http.JsonErrorResponse(nethttp.StatusTooManyRequests, "too many requests")
                }

                return withDecisionRetryAfter(response, decision), nil
            }

            return next(runtimeInstance, writer, request)
        }
    }
}

/* QuickLimitConfig is what the two address-keyed quick doors declare beside their per-minute budget. */
type QuickLimitConfig struct {
    /* ClientIpResolver reads the address the budget is charged to, an IPv6 one on its /64; nil reads the direct peer, so behind a reverse proxy every client shares the proxy's budget: pass NewForwardedClientIpResolver with the kernel's policy there. */
    ClientIpResolver ClientIpResolver
}

/* UserLimitConfig is what the user-keyed quick door declares beside its budget and user id callback: the address an anonymous request falls back to. */
type UserLimitConfig struct {
    /* ClientIpResolver reads the address an anonymous request is charged to, as QuickLimitConfig.ClientIpResolver does. */
    ClientIpResolver ClientIpResolver
}

/* SimpleRateLimit charges each client address a fixed-window budget of requestsPerMinute, at least 1, refused below naming the argument. */
func SimpleRateLimit(requestsPerMinute int, config QuickLimitConfig) httpcontract.Middleware {
    refuseBelowMinimum(1 > requestsPerMinute, "requestsPerMinute", strconv.Itoa(requestsPerMinute), "1")

    return RateLimitMiddleware(NewFixedWindowLimiter("simple", requestsPerMinute, time.Minute, LimiterConfig{}), RateLimitConfig{
        ClientIpResolver: config.ClientIpResolver,
    })
}

/* IpRateLimit charges each client address a sliding-window budget of requestsPerMinute, at least 1, refused below naming the argument. */
func IpRateLimit(requestsPerMinute int, config QuickLimitConfig) httpcontract.Middleware {
    refuseBelowMinimum(1 > requestsPerMinute, "requestsPerMinute", strconv.Itoa(requestsPerMinute), "1")

    return RateLimitMiddleware(NewSlidingWindowLimiter("ip", requestsPerMinute, time.Minute, LimiterConfig{}), RateLimitConfig{
        ClientIpResolver: config.ClientIpResolver,
    })
}

/* UserRateLimit charges each user userId reads a sliding-window budget of requestsPerMinute, under the key "user:<id>"; a request with an empty id is charged to its client address, and a budget below 1 or a nil userId is refused naming the argument. */
func UserRateLimit(requestsPerMinute int, userId KeyExtractor, config UserLimitConfig) httpcontract.Middleware {
    refuseBelowMinimum(1 > requestsPerMinute, "requestsPerMinute", strconv.Itoa(requestsPerMinute), "1")

    if nil == userId {
        exception.Panic(
            exception.NewError(
                "get user id callback is required for user rate limit middleware",
                exceptioncontract.Context{
                    "argument": "userId",
                },
                nil,
            ),
        )
    }

    getUserId := userId
    clientIpResolver := config.ClientIpResolver

    return RateLimitMiddleware(NewSlidingWindowLimiter("user", requestsPerMinute, time.Minute, LimiterConfig{}), RateLimitConfig{
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

func defaultOnLimitExceeded(request httpcontract.Request, decision httpcontract.RateLimitDecision) (httpcontract.Response, error) {
    return nil, exception.NewHttpError(
        nethttp.StatusTooManyRequests,
        exception.HttpErrorConfig{
            Message:    "rate limit exceeded",
            RetryAfter: max(decision.RetryAfter, 0),
        },
    )
}

func clampRetryAfter(retryAfter time.Duration, window time.Duration) time.Duration {
    if 0 > retryAfter {
        return 0
    }

    if window < retryAfter {
        return window
    }

    return retryAfter
}

func withDecisionRetryAfter(response httpcontract.Response, decision httpcontract.RateLimitDecision) httpcontract.Response {
    if "" == response.Headers().Get("Retry-After") {
        http.SetRetryAfter(response, decision.RetryAfter)
    }

    return response
}

/* matched on the error itself: an error answered unavailable is the mark wrapping the exception, never the exception */
func withDecisionRetryAfterError(limitErr error, decision httpcontract.RateLimitDecision) error {
    httpException, isHttpException := limitErr.(*exception.HttpException)
    if false == isHttpException || nil == httpException || 0 != httpException.RetryAfter() || 0 >= decision.RetryAfter {
        return limitErr
    }

    rebuilt := exception.NewHttpError(
        httpException.StatusCode(),
        exception.HttpErrorConfig{
            Code:       httpException.Code(),
            Key:        httpException.Key(),
            Message:    httpException.Message(),
            Parameters: httpException.Parameters(),
            Cause:      httpException.CauseErr(),
            RetryAfter: decision.RetryAfter,
        },
    )
    rebuilt.SetContext(httpException.Context())

    if true == httpException.AlreadyLogged() {
        rebuilt.MarkAsLogged()
    }

    return rebuilt
}

/* allowRequestUnderLimit is the metering step the middleware and the listener doors share: the key is extracted, the limiter asked, and a failure recorded once unless already marked logged, at warning when the caller's own context ended it. The decision already reflects the limiter's failure policy; a denial whose error is answered as unavailable is returned beside it, marked logged once recorded, and an unmarked error beside a denial stays a 429. */
func allowRequestUnderLimit(settings rateLimitSettings, runtimeInstance containercontract.Runtime, request httpcontract.Request) (httpcontract.RateLimitDecision, error) {
    key := settings.keyExtractor(request)

    decision, allowErr := settings.limiter.Allow(runtimeInstance, key, httpcontract.AllowOptions{})
    if nil != allowErr && false == exception.IsAlreadyLogged(allowErr) {
        logger := logging.LoggerFromRuntime(runtimeInstance)
        if nil != logger {
            recordContext := exception.LogContext(allowErr, exception.LogContextOptions{Extra: exceptioncontract.Context{"key": key}})

            if true == exception.ChainIs(allowErr, context.Canceled) || nil != runtimeInstance.Context().Err() {
                logger.Warning("rate limiter call ended by the caller", recordContext)
            } else {
                logger.Error("rate limiter store failure", recordContext)
            }

            allowErr = exception.MarkLogged(allowErr)
        }
    }

    if false == decision.Allowed && true == exception.AnswersUnavailable(allowErr) {
        return decision, allowErr
    }

    return decision, nil
}
