package rueidis

import (
    "context"
    "errors"
    "strconv"
    "time"

    "github.com/precision-soft/melody/v2/exception"
    httpcontract "github.com/precision-soft/melody/v2/http/contract"
    "github.com/precision-soft/melody/v2/logging"
    loggingcontract "github.com/precision-soft/melody/v2/logging/contract"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
    "github.com/redis/rueidis"
)

const defaultRateLimiterPrefix = "melody:rate_limit:"

const defaultRateLimiterCallTimeout = 250 * time.Millisecond

/* rateLimiterScript is the atomic fixed-window counter: one INCR per call, with the window expiry armed in the same round trip by the call that creates the counter and by any later call that finds the key without an expiry. A key that reached the store without a ttl, through a PERSIST, an eviction policy or a hand-written value, would otherwise never lapse and, under the fail-closed default, refuse its requests permanently; reading the ttl inside the script keeps the check and the arming atomic. */
var rateLimiterScript = rueidis.NewLuaScript(`local count = redis.call("incr", KEYS[1])
if count == 1 or redis.call("pttl", KEYS[1]) < 0 then
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

/* WithRateLimiterOnError observes store failures from the plain Allow path, which has no error return; AllowWithRuntime reports them to the caller as well. It replaces the record the limiter writes when no observer is given: the failure is handed over untouched and unmarked, since an observer may be a metric rather than a journal. */
func WithRateLimiterOnError(onError func(error)) RateLimiterOption {
    return func(instance *RateLimiter) {
        instance.onError = onError
    }
}

/* WithRateLimiterCallTimeout bounds the store round trip on both entry points: the plain Allow path, which carries no request context, and AllowWithRuntime, where it caps the runtime context so a request without a deadline, as melody's http kernel leaves it, fails fast, which fail-closed login and OTP routes depend on. A non-positive timeout falls back to the default; the cache subpackage reads non-positive as unbounded and says so on its own constructor. */
func WithRateLimiterCallTimeout(timeout time.Duration) RateLimiterOption {
    return func(instance *RateLimiter) {
        if 0 >= timeout {
            timeout = defaultRateLimiterCallTimeout
        }

        instance.callTimeout = timeout
    }
}

/* NewRateLimiter returns a Redis-backed fixed-window limiter shared by every application instance, implementing httpcontract.RateLimiter and httpcontract.RuntimeRateLimiter as the distributed drop-in for the in-process limiters. The counter is one atomic Lua round trip, so N instances enforce one limit, and the fixed window admits up to twice the limit across a window edge; store failures follow the failure mode, FailureModeClosed by default. Unlike the in-process limiter, which clamps a non-positive rate or window, it panics at boot, since a distributed limit disarmed by an unset environment key would be disarmed on every instance. */
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
        instance.reportError(nil, allowErr)
    }

    return allowed
}

func (instance *RateLimiter) AllowWithRuntime(runtimeInstance runtimecontract.Runtime, key string) (bool, error) {
    /* cap the runtime context with the call timeout; context.WithTimeout keeps the earlier deadline, so a tighter request deadline still wins */
    callContext, cancel := context.WithTimeout(runtimeInstance.Context(), instance.callTimeout)
    defer cancel()

    allowed, allowErr := instance.allow(callContext, key)
    if nil != allowErr {
        allowErr = instance.reportError(logging.LoggerFromRuntime(runtimeInstance), allowErr)
    }

    return allowed, allowErr
}

/* Reset drops the counter for one key best-effort. It returns nothing, so a store failure reports through the error observer, or a record when none was given, since a failed reset leaves an account locked after a successful login. */
func (instance *RateLimiter) Reset(key string) {
    callContext, cancel := context.WithTimeout(context.Background(), instance.callTimeout)
    defer cancel()

    command := instance.client.B().Del().Key(instance.prefix + key).Build()
    if resultErr := instance.client.Do(callContext, command).Error(); nil != resultErr {
        instance.reportError(
            nil,
            exception.NewError("redis rate limiter reset failed", map[string]any{"key": key}, resultErr),
        )
    }
}

/* allow runs the fixed-window counter; on a store failure the returned allowed value is the failure-mode default, so callers can honor it even when the error is also returned. */
func (instance *RateLimiter) allow(callContext context.Context, key string) (bool, error) {
    milliseconds := strconv.FormatInt(floorPositiveMilliseconds(instance.window), 10)

    result := rateLimiterScript.Exec(callContext, instance.client, []string{instance.prefix + key}, []string{milliseconds})

    count, resultErr := result.AsInt64()
    if nil != resultErr {
        /* the caller's own cancellation is not a store failure: a client that disconnected mid-round-trip surfaces as the context's cancellation and is named so, not as a redis outage. The failure-mode answer applies either way; the call-timeout deadline stays a store failure, since that budget exists to catch a slow store. */
        if true == errors.Is(resultErr, context.Canceled) {
            return FailureModeOpen == instance.failureMode, exception.NewError(
                "redis rate limiter call cancelled by the caller",
                map[string]any{"key": key, "failureMode": string(instance.failureMode)},
                resultErr,
            )
        }

        return FailureModeOpen == instance.failureMode, exception.NewError(
            "redis rate limiter store failure",
            map[string]any{"key": key, "failureMode": string(instance.failureMode)},
            resultErr,
        )
    }

    return count <= int64(instance.limit), nil
}

/* reportError delivers a store failure and answers the error the caller should carry on with. An observer given by the application gets the failure untouched and unmarked, since it may be a counter rather than a journal. With no observer the failure is recorded here, because Allow answers a bool and Reset nothing, so an outage would otherwise reach no channel; the record takes the level the http middleware picks, a caller's own cancellation not being an outage. The error is then marked already-logged, the framework's mark the exception listener and the http kernel honour, so the middleware files nothing a second time. */
func (instance *RateLimiter) reportError(logger loggingcontract.Logger, err error) error {
    if nil != instance.onError {
        instance.onError(err)

        return err
    }

    if nil == logger {
        logger = logging.EmergencyLogger()
    }

    /* the key and the failure mode already travel in the error's own context, put there where the call was made */
    recordContext := exception.LogContext(err)

    if true == errors.Is(err, context.Canceled) {
        logger.Warning("rate limiter call cancelled", recordContext)
    } else {
        logger.Error("rate limiter store failure", recordContext)
    }

    return exception.MarkLogged(err)
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
