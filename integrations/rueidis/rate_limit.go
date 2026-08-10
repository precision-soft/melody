package rueidis

import (
    "context"
    "strconv"
    "time"

    "github.com/precision-soft/melody/exception"
    httpcontract "github.com/precision-soft/melody/http/contract"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
    "github.com/redis/rueidis"
)

const defaultRateLimiterPrefix = "melody:rate_limit:"

const defaultRateLimiterCallTimeout = 250 * time.Millisecond

/* rateLimiterScript is the atomic fixed-window counter: one INCR per call, with the window expiry set only when the counter is created, all in a single round trip. */
var rateLimiterScript = rueidis.NewLuaScript(`local count = redis.call("incr", KEYS[1])
if count == 1 then
    redis.call("pexpire", KEYS[1], tonumber(ARGV[1]))
end
return count`)

type RateLimiterFailureMode string

const (
    /* FailureModeClosed denies the request when the store cannot be reached — the strict default, right for login/OTP-style endpoints where an outage must not lift the limit. */
    FailureModeClosed RateLimiterFailureMode = "closed"

    /* FailureModeOpen allows the request when the store cannot be reached — availability over strictness, right for plain traffic shaping where a store outage must not become an outage of every limited route. */
    FailureModeOpen RateLimiterFailureMode = "open"
)

type RateLimiterOption func(*RateLimiter)

func WithRateLimiterKeyPrefix(prefix string) RateLimiterOption {
    return func(instance *RateLimiter) {
        instance.prefix = prefix
    }
}

func WithRateLimiterFailureMode(mode RateLimiterFailureMode) RateLimiterOption {
    return func(instance *RateLimiter) {
        instance.failureMode = mode
    }
}

/* WithRateLimiterOnError observes store failures from the plain Allow path, which has no error return; AllowWithRuntime reports them to the caller as well. */
func WithRateLimiterOnError(onError func(error)) RateLimiterOption {
    return func(instance *RateLimiter) {
        instance.onError = onError
    }
}

/* WithRateLimiterCallTimeout bounds the store round trip on both entry points: the plain Allow path, which carries no request context, and AllowWithRuntime, where it caps the runtime context so a request whose context has no deadline — melody's http kernel attaches none — still fails fast instead of hanging on an unresponsive store (the whole point of fail-closed on login/OTP routes). A non-positive timeout falls back to the default, the way this limiter's other options and the provider's connect timeout read theirs, so a config-sourced unset value can never build an already-cancelled context that forces every call onto the store-failure path. The cache subpackage deliberately reads its command timeout the other way — non-positive means unbounded — and says so on its own constructor. */
func WithRateLimiterCallTimeout(timeout time.Duration) RateLimiterOption {
    return func(instance *RateLimiter) {
        if 0 >= timeout {
            timeout = defaultRateLimiterCallTimeout
        }

        instance.callTimeout = timeout
    }
}

/* NewRateLimiter returns a Redis-backed fixed-window limiter shared by every application instance, implementing both httpcontract.RateLimiter and httpcontract.RuntimeRateLimiter — the distributed drop-in for the in-process middleware limiters. The counter is atomic (one Lua round trip), so N instances enforce one shared limit; note the fixed window admits up to 2x the limit across a window edge. Store failures follow the configured failure mode and default to FailureModeClosed. Construction is deliberately stricter than the in-process middleware limiter it drops in for: the middleware clamps a non-positive rate or window to a safe value, while this panics at boot — a distributed limit disarmed by an unset environment key would be disarmed on every instance at once, and the boot is where that reads loudest. */
func NewRateLimiter(
    client rueidis.Client,
    limit int,
    window time.Duration,
    options ...RateLimiterOption,
) *RateLimiter {
    if nil == client {
        exception.Panic(exception.NewError("redis rate limiter client is nil", nil, nil))
    }

    if 0 >= limit {
        exception.Panic(exception.NewError("redis rate limiter limit must be positive", nil, nil))
    }

    if 0 >= window {
        exception.Panic(exception.NewError("redis rate limiter window must be positive", nil, nil))
    }

    instance := &RateLimiter{
        client:      client,
        limit:       limit,
        window:      window,
        prefix:      defaultRateLimiterPrefix,
        failureMode: FailureModeClosed,
        callTimeout: defaultRateLimiterCallTimeout,
    }

    for _, option := range options {
        option(instance)
    }

    return instance
}

type RateLimiter struct {
    client      rueidis.Client
    limit       int
    window      time.Duration
    prefix      string
    failureMode RateLimiterFailureMode
    onError     func(error)
    callTimeout time.Duration
}

func (instance *RateLimiter) Allow(key string) bool {
    callContext, cancel := context.WithTimeout(context.Background(), instance.callTimeout)
    defer cancel()

    allowed, allowErr := instance.allow(callContext, key)
    if nil != allowErr {
        instance.reportError(allowErr)
    }

    return allowed
}

func (instance *RateLimiter) AllowWithRuntime(runtimeInstance runtimecontract.Runtime, key string) (bool, error) {
    /* cap the runtime context with the call timeout: context.WithTimeout keeps whichever deadline is earlier, so a request that already carries a tighter deadline still wins, while a request whose context has no deadline — as melody's http kernel leaves it — is bounded here rather than hanging on an unresponsive store. */
    callContext, cancel := context.WithTimeout(runtimeInstance.Context(), instance.callTimeout)
    defer cancel()

    allowed, allowErr := instance.allow(callContext, key)
    if nil != allowErr {
        instance.reportError(allowErr)
    }

    return allowed, allowErr
}

/* Reset drops the counter for one key best-effort; a store failure only reports through the error observer, matching the interface's void signature. */
func (instance *RateLimiter) Reset(key string) {
    callContext, cancel := context.WithTimeout(context.Background(), instance.callTimeout)
    defer cancel()

    command := instance.client.B().Del().Key(instance.prefix + key).Build()
    if resultErr := instance.client.Do(callContext, command).Error(); nil != resultErr {
        instance.reportError(exception.NewError("redis rate limiter reset failed", map[string]any{"key": key}, resultErr))
    }
}

/* allow runs the fixed-window counter; on a store failure the returned allowed value is the failure-mode default, so callers can honor it even when the error is also returned. */
func (instance *RateLimiter) allow(callContext context.Context, key string) (bool, error) {
    milliseconds := strconv.FormatInt(floorPositiveMilliseconds(instance.window), 10)

    result := rateLimiterScript.Exec(callContext, instance.client, []string{instance.prefix + key}, []string{milliseconds})

    count, resultErr := result.AsInt64()
    if nil != resultErr {
        return FailureModeOpen == instance.failureMode, exception.NewError(
            "redis rate limiter store failure",
            map[string]any{"key": key, "failureMode": string(instance.failureMode)},
            resultErr,
        )
    }

    return count <= int64(instance.limit), nil
}

func (instance *RateLimiter) reportError(err error) {
    if nil == instance.onError {
        return
    }

    instance.onError(err)
}

/* floorPositiveMilliseconds guarantees a positive window never collapses to a 0 PEXPIRE argument, which Redis rejects. */
func floorPositiveMilliseconds(ttl time.Duration) int64 {
    milliseconds := ttl.Milliseconds()
    if 0 == milliseconds {
        return 1
    }

    return milliseconds
}

var _ httpcontract.RateLimiter = (*RateLimiter)(nil)
var _ httpcontract.RuntimeRateLimiter = (*RateLimiter)(nil)
