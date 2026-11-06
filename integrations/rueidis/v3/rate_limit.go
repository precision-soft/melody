package rueidis

import (
    "context"
    "errors"
    "fmt"
    "reflect"
    "strconv"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "github.com/redis/rueidis"
)

const defaultRateLimiterPrefix = "melody:rate_limit:"

const defaultRateLimiterCallTimeout = 250 * time.Millisecond

/* the expiry is also armed on a key found without one: such a key never lapses and, fail-closed, refuses forever */
var rateLimiterScript = rueidis.NewLuaScript(`local count = redis.call("incr", KEYS[1])
if count == 1 or redis.call("pttl", KEYS[1]) < 0 then
    redis.call("pexpire", KEYS[1], tonumber(ARGV[1]))
end
return count`)

type RateLimiterFailureMode string

const (
    /* FailureModeClosed denies the request when the store cannot be reached */
    FailureModeClosed RateLimiterFailureMode = "closed"

    /* FailureModeOpen allows the request when the store cannot be reached */
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

/* WithRateLimiterOnError replaces the limiter's own record: the failure is handed over untouched and unmarked */
func WithRateLimiterOnError(onError func(error)) RateLimiterOption {
    return func(instance *RateLimiter) {
        instance.onError = onError
    }
}

func WithRateLimiterLogger(logger loggingcontract.Logger) RateLimiterOption {
    return func(instance *RateLimiter) {
        instance.logger = logger
    }
}

/* WithRateLimiterCallTimeout bounds the store round trip on both entry points; non-positive takes the default */
func WithRateLimiterCallTimeout(timeout time.Duration) RateLimiterOption {
    return func(instance *RateLimiter) {
        instance.callTimeout = resolvedCallTimeout(timeout, defaultRateLimiterCallTimeout)
    }
}

/* NewRateLimiter panics at boot on a non-positive rate or window, and admits up to twice the limit across an edge */
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
    callContext, cancel := context.WithTimeout(runtimeInstance.Context(), instance.callTimeout)
    defer cancel()

    allowed, allowErr := instance.allow(runtimeInstance.Context(), callContext, key)
    if nil != allowErr {
        allowErr = instance.reportError(logging.LoggerFromRuntime(runtimeInstance), allowErr)
    }

    return allowed, allowErr
}

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

var errCallerDeadline = errors.New("the caller's deadline ended the rate limiter call")

func (instance *RateLimiter) allow(parentContext context.Context, callContext context.Context, key string) (bool, error) {
    milliseconds := strconv.FormatInt(floorPositiveMilliseconds(instance.window), 10)

    result := rateLimiterScript.Exec(callContext, instance.client, []string{instance.prefix + key}, []string{milliseconds})

    count, resultErr := result.AsInt64()
    if nil != resultErr {
        if true == exception.ChainIs(resultErr, context.Canceled) {
            return FailureModeOpen == instance.failureMode, exception.NewError(
                "redis rate limiter call cancelled by the caller",
                map[string]any{"key": key, "failureMode": string(instance.failureMode)},
                resultErr,
            )
        }

        /* the caller's own deadline also ends the round trip with DeadlineExceeded, and it is not a store failure */
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

/* reportError marks the error already-logged only when it recorded it; an observer gets it unmarked */
func (instance *RateLimiter) reportError(logger loggingcontract.Logger, err error) error {
    if nil != instance.onError {
        instance.onError(err)

        return err
    }

    /* a typed nil logger is read as absent: called, it would panic inside a best-effort release */
    if true == isAbsentLogger(logger) {
        logger = instance.logger
    }

    if true == isAbsentLogger(logger) {
        logger = logging.EmergencyLogger()
    }

    recordContext := exception.LogContext(err)

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
