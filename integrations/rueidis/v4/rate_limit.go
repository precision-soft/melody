package rueidis

import (
    "context"
    "errors"
    "fmt"
    "reflect"
    "strconv"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
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

/* RateLimiterConfig declares the limiter. Limit and Window are required, as the in-process limiter's LimiterConfig has them; the rest has a default. */
type RateLimiterConfig struct {
    /* Limit is the calls admitted per Window, at least 1; a limit of 0 or below is refused, since a distributed limit disarmed by an unset environment key would be disarmed on every instance. */
    Limit int

    /* Window is the span the calls are counted over, at least 1ms, the resolution of the counter's expiry. */
    Window time.Duration

    /* KeyPrefix is the namespace of every counter, melody:rate_limit: when empty. */
    KeyPrefix string

    /* FailureMode is the answer when the store cannot be reached, FailureModeClosed when left out; any other value is refused, so a misspelt mode is not read as closed. */
    FailureMode RateLimiterFailureMode

    /* OnError observes store failures from the plain Allow path, which has no error return; AllowWithRuntime reports them to the caller as well. It replaces the record the limiter writes when no observer is given: the failure is handed over untouched and unmarked, since an observer may be a metric rather than a journal. */
    OnError func(error)

    /* Logger is the journal a store failure is recorded in when the call carries none: Reset and the plain Allow path take no runtime, so without it their failures reach the emergency logger, standard error, outside the application's journal. AllowWithRuntime records on the request's own logger first; nil or a typed nil is the emergency logger. An OnError observer replaces the record either way. */
    Logger loggingcontract.Logger

    /* CallTimeout bounds the store round trip on both entry points: the plain Allow path, which carries no request context, and AllowWithRuntime, where it caps the runtime context so a request without a deadline, as melody's http kernel leaves it, fails fast, which fail-closed login and OTP routes depend on. 250ms by default, at least 1ms, unlimited refused. */
    CallTimeout bound.Duration
}

var rateLimiterCallTimeoutRule = callTimeoutRule("RateLimiterConfig.CallTimeout", defaultRateLimiterCallTimeout)

/* NewRateLimiter returns a Redis-backed fixed-window limiter shared by every application instance, implementing httpcontract.RateLimiter and httpcontract.RuntimeRateLimiter as the distributed drop-in for the in-process limiters. The counter is one atomic Lua round trip, so N instances enforce one limit, and the fixed window admits up to twice the limit across a window edge; store failures follow the failure mode. The configuration is resolved once; a nil client or a value outside its rule panics naming the field. */
func NewRateLimiter(client rueidis.Client, config RateLimiterConfig) *RateLimiter {
    if nil == client {
        exception.Panic(exception.NewError("redis rate limiter client is nil", nil, nil))
    }

    refuseBelowMinimum(1 > config.Limit, "RateLimiterConfig.Limit", strconv.Itoa(config.Limit), "1")
    refuseBelowMinimum(bound.TimeoutMinimum > config.Window, "RateLimiterConfig.Window", config.Window.String(), bound.TimeoutMinimum.String())

    failureMode := config.FailureMode
    if "" == failureMode {
        failureMode = FailureModeClosed
    }

    if FailureModeClosed != failureMode && FailureModeOpen != failureMode {
        exception.Panic(
            exception.NewError(
                "the rate limiter failure mode is unknown",
                exceptioncontract.Context{
                    "field":   "RateLimiterConfig.FailureMode",
                    "value":   string(config.FailureMode),
                    "allowed": string(FailureModeClosed) + ", " + string(FailureModeOpen),
                },
                nil,
            ),
        )
    }

    callTimeout, callTimeoutErr := bound.Resolve(config.CallTimeout, rateLimiterCallTimeoutRule)
    if nil != callTimeoutErr {
        exception.Panic(exception.FromError(callTimeoutErr))
    }

    prefix := config.KeyPrefix
    if "" == prefix {
        prefix = defaultRateLimiterPrefix
    }

    return &RateLimiter{
        client:      client,
        limit:       config.Limit,
        window:      config.Window,
        prefix:      prefix,
        failureMode: failureMode,
        onError:     config.OnError,
        logger:      config.Logger,
        callTimeout: callTimeout.Value,
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

type RateLimiter struct {
    client      rueidis.Client
    limit       int
    window      time.Duration
    prefix      string
    failureMode RateLimiterFailureMode
    onError     func(error)
    logger      loggingcontract.Logger
    callTimeout time.Duration
}

func (instance *RateLimiter) Allow(key string) bool {
    callContext, cancel := context.WithTimeout(context.Background(), instance.callTimeout)
    defer cancel()

    allowed, allowErr := instance.allow(context.Background(), callContext, key)
    if nil != allowErr {
        instance.reportError(nil, allowErr)
    }

    return allowed
}

func (instance *RateLimiter) AllowWithRuntime(runtimeInstance runtimecontract.Runtime, key string) (bool, error) {
    /* cap the runtime context with the call timeout; context.WithTimeout keeps the earlier deadline, so a tighter request deadline still wins */
    callContext, cancel := context.WithTimeout(runtimeInstance.Context(), instance.callTimeout)
    defer cancel()

    allowed, allowErr := instance.allow(runtimeInstance.Context(), callContext, key)
    if nil != allowErr {
        allowErr = instance.reportError(logging.LoggerFromRuntime(runtimeInstance), allowErr)
    }

    return allowed, allowErr
}

/* Reset drops the counter for one key best-effort. It returns nothing, so a store failure reports through the error observer, or a record in the limiter's logger when none was given, since a failed reset leaves an account locked after a successful login. */
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
/* errCallerDeadline marks a round trip the caller's own deadline ended, told apart from a store failure by reportError. */
var errCallerDeadline = errors.New("the caller's deadline ended the rate limiter call")

/* allow runs the window script under callContext; parentContext is the caller's own, read to tell its deadline from the call timeout. */
func (instance *RateLimiter) allow(parentContext context.Context, callContext context.Context, key string) (bool, error) {
    milliseconds := strconv.FormatInt(instance.window.Milliseconds(), 10)

    result := rateLimiterScript.Exec(callContext, instance.client, []string{instance.prefix + key}, []string{milliseconds})

    count, resultErr := result.AsInt64()
    if nil != resultErr {
        /* the caller's own cancellation is not a store failure: a client that disconnected mid-round-trip surfaces as the context's cancellation and is named so, not as a redis outage. The failure-mode answer applies either way; the call-timeout deadline stays a store failure, since that budget exists to catch a slow store. */
        if true == exception.ChainIs(resultErr, context.Canceled) {
            return FailureModeOpen == instance.failureMode, exception.NewError(
                "redis rate limiter call cancelled by the caller",
                map[string]any{"key": key, "failureMode": string(instance.failureMode)},
                resultErr,
            )
        }

        /* the caller's own deadline, tighter than the call timeout, ends the round trip with DeadlineExceeded too; the parent context says whose it was, and it is not a store failure */
        if true == exception.ChainIs(resultErr, context.DeadlineExceeded) && nil != parentContext.Err() {
            return FailureModeOpen == instance.failureMode, exception.NewError(
                "redis rate limiter call ended by the caller's deadline",
                map[string]any{"key": key, "failureMode": string(instance.failureMode)},
                fmt.Errorf("%w: %w", errCallerDeadline, resultErr),
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

/* reportError delivers a store failure and answers the error the caller should carry on with. An observer given by the application gets the failure untouched and unmarked, since it may be a counter rather than a journal. With no observer the failure is recorded here, on the call's logger, else the limiter's, else the emergency logger, because Allow answers a bool and Reset nothing, so an outage would otherwise reach no channel; the record takes the level the http middleware picks, a caller's own cancellation not being an outage. The error is then marked already-logged, the framework's mark the exception listener and the http kernel honour, so the middleware files nothing a second time. */
func (instance *RateLimiter) reportError(logger loggingcontract.Logger, err error) error {
    if nil != instance.onError {
        instance.onError(err)

        return err
    }

    /* a typed nil, a nil pointer behind the interface, is read as absent: called, it would panic inside a best-effort release that runs after an accepted sign-in */
    if true == isAbsentLogger(logger) {
        logger = instance.logger
    }

    if true == isAbsentLogger(logger) {
        logger = logging.EmergencyLogger()
    }

    /* the key and the failure mode already travel in the error's own context, put there where the call was made */
    recordContext := exception.LogContext(err, exception.LogContextConfig{})

    if true == exception.ChainIs(err, errCallerDeadline) {
        logger.Warning("rate limiter call ended by the caller's deadline", recordContext)
    } else if true == exception.ChainIs(err, context.Canceled) {
        logger.Warning("rate limiter call cancelled", recordContext)
    } else {
        logger.Error("rate limiter store failure", recordContext)
    }

    return exception.MarkLogged(err)
}

var _ httpcontract.RateLimiter = (*RateLimiter)(nil)
var _ httpcontract.RuntimeRateLimiter = (*RateLimiter)(nil)

/* isAbsentLogger answers whether a logger is nil, the nil interface or a nil value of a nilable kind behind it */
func isAbsentLogger(logger loggingcontract.Logger) bool {
    if nil == logger {
        return true
    }

    value := reflect.ValueOf(logger)
    switch value.Kind() {
    case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan, reflect.Interface:
        return value.IsNil()
    default:
        return false
    }
}
