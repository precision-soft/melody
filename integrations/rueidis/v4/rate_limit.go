package rueidis

import (
    "context"
    "strconv"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/redis/rueidis"
)

const rateLimiterPrefixSuffix = "rate_limit:"

const defaultRateLimiterCallTimeout = 250 * time.Millisecond

/* rateLimiterScript is the atomic fixed-window counter: one INCR per call, answering the count and the counter's ttl, with the window expiry armed in the same round trip by the call that creates the counter and by any later call that finds the key without an expiry. A key that reached the store without a ttl, through a PERSIST, an eviction policy or a hand-written value, would otherwise never lapse and, under the fail-closed default, refuse its requests permanently. */
var rateLimiterScript = rueidis.NewLuaScript(`local count = redis.call("incr", KEYS[1])
local ttl = -1
if count ~= 1 then
    ttl = redis.call("pttl", KEYS[1])
end
if count == 1 or ttl < 0 then
    redis.call("pexpire", KEYS[1], tonumber(ARGV[1]))
end
return {count, ttl}`)

type RateLimiterFailureMode string

const (
    /* FailureModeClosed denies the request when the store cannot be reached — the strict default, right for login/OTP-style endpoints where an outage must not lift the limit. */
    FailureModeClosed RateLimiterFailureMode = "closed"

    /* FailureModeOpen allows the request when the store cannot be reached — availability over strictness, right for plain traffic shaping where a store outage must not become an outage of every limited route. */
    FailureModeOpen RateLimiterFailureMode = "open"
)

/* RateLimiterConfig declares the limiter beside its name, limit and window; every field has a default. */
type RateLimiterConfig struct {
    /* KeyPrefix opens every counter, used verbatim: <namespace>:rate_limit: when empty, the connection's namespace; a counter is keyed <KeyPrefix><name>:<key>, so a prefix ends with its separator, or two limiters can share a counter. */
    KeyPrefix string

    /* FailureMode is the answer when the store cannot be reached, FailureModeClosed when left out; any other value is refused, so a misspelt mode is not read as closed. */
    FailureMode RateLimiterFailureMode

    /* CallTimeout caps every round trip on top of the caller's runtime context, so a request without a deadline, as melody's http kernel leaves it, fails fast, which fail-closed login and OTP routes depend on. 250ms by default, at least 1ms, unlimited refused. */
    CallTimeout bound.Duration
}

var rateLimiterCallTimeoutRule = callTimeoutRule("RateLimiterConfig.CallTimeout", defaultRateLimiterCallTimeout)

/* NewRateLimiter returns a Redis-backed fixed-window limiter shared by every application instance, the distributed drop-in for the in-process limiters. The counter is one atomic Lua round trip, so N instances enforce one limit, and the fixed window admits up to twice the limit across a window edge; store failures follow the failure mode. The name keys the counters, so two limiters over one client never share one and a limiter keeps its counters when its limit or window changes: 1 to 64 bytes of [A-Za-z0-9._-]. A nil client, a malformed name, a limit below 1, a window below 1ms or a configuration value outside its rule panics naming the argument or the field. */
func NewRateLimiter(connection *Connection, name string, limit int, window time.Duration, config RateLimiterConfig) *RateLimiter {
    refuseNilConnection(connection)

    refuseMalformedLimiterName(name)
    refuseArgumentBelowMinimum(1 > limit, "limit", name, strconv.Itoa(limit), "1")
    refuseArgumentBelowMinimum(bound.TimeoutMinimum > window, "window", name, window.String(), bound.TimeoutMinimum.String())

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
        prefix = connection.namespacedName(rateLimiterPrefixSuffix)
    }

    return &RateLimiter{
        client:      connection.Client(),
        name:        name,
        limit:       limit,
        window:      window,
        prefix:      prefix,
        failureMode: failureMode,
        callTimeout: callTimeout.Value,
    }
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

type RateLimiter struct {
    client      rueidis.Client
    name        string
    limit       int
    window      time.Duration
    prefix      string
    failureMode RateLimiterFailureMode
    callTimeout time.Duration
}

func (instance *RateLimiter) Name() string {
    return instance.name
}

/* Allow charges one call to the key under the runtime's context, capped by CallTimeout. On a store failure the decision is the failure mode's, answered beside the error. */
func (instance *RateLimiter) Allow(runtimeInstance containercontract.Runtime, key string, _ httpcontract.AllowOptions) (httpcontract.RateLimitDecision, error) {
    if refusalErr := containercontract.RefuseNilRuntime(runtimeInstance, "rueidis.RateLimiter.Allow"); nil != refusalErr {
        return httpcontract.RateLimitDecision{Limit: instance.limit}, refusalErr
    }

    parentContext := runtimeInstance.Context()
    callContext, cancel := context.WithTimeout(parentContext, instance.callTimeout)
    defer cancel()

    milliseconds := strconv.FormatInt(instance.window.Milliseconds(), 10)

    reply, resultErr := rateLimiterScript.Exec(callContext, instance.client, []string{instance.counterKey(key)}, []string{milliseconds}).AsIntSlice()
    if nil == resultErr && 2 != len(reply) {
        resultErr = exception.NewError("redis rate limiter script answered an unexpected reply", map[string]any{"length": len(reply)}, nil)
    }

    if nil != resultErr {
        failureErr := instance.failure(
            parentContext,
            callContext,
            key,
            resultErr,
            "redis rate limiter call cancelled by the caller",
            "redis rate limiter call ended by the caller's deadline",
            "redis rate limiter store failure",
        )

        return httpcontract.RateLimitDecision{Allowed: FailureModeOpen == instance.failureMode, Limit: instance.limit}, failureErr
    }

    count := reply[0]
    if int64(instance.limit) >= count {
        return httpcontract.RateLimitDecision{Allowed: true, Remaining: instance.limit - int(count), Limit: instance.limit}, nil
    }

    return httpcontract.RateLimitDecision{RetryAfter: time.Duration(max(reply[1], 0)) * time.Millisecond, Limit: instance.limit}, nil
}

/* Reset drops the key's counter under the runtime's context, capped by CallTimeout, and answers a failure to do so: a failed reset leaves an account locked after a successful login. */
func (instance *RateLimiter) Reset(runtimeInstance containercontract.Runtime, key string) error {
    if refusalErr := containercontract.RefuseNilRuntime(runtimeInstance, "rueidis.RateLimiter.Reset"); nil != refusalErr {
        return refusalErr
    }

    parentContext := runtimeInstance.Context()
    callContext, cancel := context.WithTimeout(parentContext, instance.callTimeout)
    defer cancel()

    command := instance.client.B().Del().Key(instance.counterKey(key)).Build()
    if resultErr := instance.client.Do(callContext, command).Error(); nil != resultErr {
        return instance.failure(
            parentContext,
            callContext,
            key,
            resultErr,
            "redis rate limiter reset cancelled by the caller",
            "redis rate limiter reset ended by the caller's deadline",
            "redis rate limiter reset failed",
        )
    }

    return nil
}

func (instance *RateLimiter) counterKey(key string) string {
    return instance.prefix + instance.name + ":" + key
}

/* the caller's own cancellation or deadline is not a store failure and stays unmarked; the call timeout's deadline is, since that budget exists to catch a slow store */
func (instance *RateLimiter) failure(
    parentContext context.Context,
    callContext context.Context,
    key string,
    resultErr error,
    cancelledMessage string,
    deadlineMessage string,
    storeMessage string,
) error {
    errorContext := map[string]any{"key": key, "limiter": instance.name, "failureMode": string(instance.failureMode)}

    if true == exception.ChainIs(resultErr, context.Canceled) {
        return exception.NewError(cancelledMessage, errorContext, resultErr)
    }

    if true == exception.ChainIs(resultErr, context.DeadlineExceeded) && (nil != parentContext.Err() || true == deadlinePassed(parentContext)) {
        return exception.NewError(deadlineMessage, errorContext, resultErr)
    }

    if true == exception.ChainIs(resultErr, context.DeadlineExceeded) && true == deadlinePassed(callContext) {
        errorContext["bound"] = rateLimiterCallTimeoutRule.Name
    }

    return exception.NewError(storeMessage, errorContext, exception.MarkInfrastructureFailure(resultErr))
}

var _ httpcontract.RateLimiter = (*RateLimiter)(nil)
