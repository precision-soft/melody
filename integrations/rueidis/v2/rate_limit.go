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

/* rateLimiterScript is the atomic fixed-window counter: one INCR per call, with the window expiry armed in the same round trip on the call that creates the counter and on any later call that finds the key carrying no expiry at all. Without the second half a key that reached the store without a ttl — a PERSIST, an eviction policy that dropped it, a writer that set it by hand — never lapsed: the counter climbed past the limit and stayed there, so every request keyed on it was refused for as long as the key lived, which under the fail-closed default is a permanent refusal nothing in the application can lift. Reading the ttl inside the script keeps the check and the arming atomic, which is the whole reason this is a script and not three commands. */
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

/* WithRateLimiterOnError observes store failures from the plain Allow path, which has no error return; AllowWithRuntime reports them to the caller as well. It replaces the record the limiter writes when no observer is given, rather than adding to it: an observer may be a metric rather than a journal, so the failure is handed over untouched and unmarked and whatever the caller does with the error stays what it was. An observer that wants both writes both. */
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
        instance.reportError(nil, allowErr)
    }

    return allowed
}

func (instance *RateLimiter) AllowWithRuntime(runtimeInstance runtimecontract.Runtime, key string) (bool, error) {
    /* cap the runtime context with the call timeout: context.WithTimeout keeps whichever deadline is earlier, so a request that already carries a tighter deadline still wins, while a request whose context has no deadline — as melody's http kernel leaves it — is bounded here rather than hanging on an unresponsive store. */
    callContext, cancel := context.WithTimeout(runtimeInstance.Context(), instance.callTimeout)
    defer cancel()

    allowed, allowErr := instance.allow(callContext, key)
    if nil != allowErr {
        allowErr = instance.reportError(logging.LoggerFromRuntime(runtimeInstance), allowErr)
    }

    return allowed, allowErr
}

/* Reset drops the counter for one key best-effort. The signature returns nothing, so a store failure reports through the error observer, and through a record when no observer was given: a successful login is meant to clear an account's lockout, and against a dead store the DEL fails, the account stays locked and nothing anywhere marks the attempt. */
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
        /* the caller's own cancellation is not a store failure: a client that disconnected while the round trip was in flight surfaces here as the context's cancellation, and labelled a store failure it read as a redis outage against a perfectly healthy store. The failure-mode answer applies either way — the request is already gone — but the error names what actually happened. The call-timeout deadline stays a store failure: that budget exists to catch the store being slow. */
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

/* reportError delivers a store failure and answers the error the caller should carry on with.

An observer given by the application is the application's channel and gets the failure untouched — it may be a counter rather than a journal, so nothing is recorded here and nothing is marked, leaving whatever the caller does with the error exactly as it was.

With no observer the failure is recorded here, because two of the three doors return nothing at all: Allow answers a bool and Reset answers nothing, so a store outage refused every call and reached no channel whatsoever — no record, no error, no metric — and the shipped default is precisely that, since the reference application wires no observer. The record carries the level the http middleware picks for the same failure: a caller's own cancellation is not an outage and would page an operator for a client that hung up.

The error is then marked already-logged and handed back, so the reader above files nothing a second time — the framework's own mark, which the exception listener and the five sites in the http kernel already honour. That is what lets the record be filed here, at the one place that knows the key and the failure mode, without the middleware writing its own beside it. */
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
