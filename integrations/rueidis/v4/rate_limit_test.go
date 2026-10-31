package rueidis

import (
    "context"
    "errors"
    "os"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/container"
    "github.com/precision-soft/melody/v4/exception"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    "github.com/precision-soft/melody/v4/runtime"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
    "github.com/redis/rueidis"
)

func rateLimiterTestClient(t *testing.T) rueidis.Client {
    t.Helper()

    address := os.Getenv("REDIS_ADDRESS")
    if "" == address {
        t.Skip("REDIS_ADDRESS not set; skipping redis rate limiter integration test")
    }

    client, clientErr := rueidis.NewClient(rueidis.ClientOption{InitAddress: []string{address}})
    if nil != clientErr {
        t.Fatalf("could not connect to redis: %v", clientErr)
    }

    t.Cleanup(client.Close)

    return client
}

func rateLimiterTestRuntime() runtimecontract.Runtime {
    serviceContainer := container.NewContainer()
    return runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)
}

func closedTestClient(t *testing.T) rueidis.Client {
    t.Helper()

    address := os.Getenv("REDIS_ADDRESS")
    if "" == address {
        t.Skip("REDIS_ADDRESS not set; skipping redis rate limiter integration test")
    }

    client, clientErr := rueidis.NewClient(rueidis.ClientOption{InitAddress: []string{address}})
    if nil != clientErr {
        t.Fatalf("could not connect to redis: %v", clientErr)
    }

    client.Close()

    return client
}

func TestRateLimiter_SharedLimitAcrossInstances(t *testing.T) {
    client := rateLimiterTestClient(t)

    key := "shared:" + t.Name()

    /* two limiter instances over one store: the limit is enforced across both, unlike the in-process limiters */
    first := NewRateLimiter(client, RateLimiterConfig{Limit: 3, Window: time.Minute, KeyPrefix: "melody:test:rate_limit:"})
    second := NewRateLimiter(client, RateLimiterConfig{Limit: 3, Window: time.Minute, KeyPrefix: "melody:test:rate_limit:"})
    defer first.Reset(key)

    if false == first.Allow(key) || false == second.Allow(key) || false == first.Allow(key) {
        t.Fatalf("expected the first three requests to be allowed")
    }

    if true == second.Allow(key) {
        t.Fatalf("expected the fourth request to be denied across instances")
    }
}

func TestRateLimiter_ResetClearsTheWindow(t *testing.T) {
    client := rateLimiterTestClient(t)

    key := "reset:" + t.Name()

    limiter := NewRateLimiter(client, RateLimiterConfig{Limit: 1, Window: time.Minute, KeyPrefix: "melody:test:rate_limit:"})
    defer limiter.Reset(key)

    if false == limiter.Allow(key) {
        t.Fatalf("expected the first request to be allowed")
    }

    if true == limiter.Allow(key) {
        t.Fatalf("expected the second request to be denied")
    }

    limiter.Reset(key)

    if false == limiter.Allow(key) {
        t.Fatalf("expected the request after reset to be allowed")
    }
}

func TestRateLimiter_WindowExpires(t *testing.T) {
    client := rateLimiterTestClient(t)

    key := "expiry:" + t.Name()

    limiter := NewRateLimiter(client, RateLimiterConfig{Limit: 1, Window: 150 * time.Millisecond, KeyPrefix: "melody:test:rate_limit:"})
    defer limiter.Reset(key)

    if false == limiter.Allow(key) {
        t.Fatalf("expected the first request to be allowed")
    }

    if true == limiter.Allow(key) {
        t.Fatalf("expected the second request to be denied inside the window")
    }

    time.Sleep(250 * time.Millisecond)

    if false == limiter.Allow(key) {
        t.Fatalf("expected a request in the next window to be allowed")
    }
}

func TestRateLimiter_AllowWithRuntimeSharesTheCounter(t *testing.T) {
    client := rateLimiterTestClient(t)
    runtimeInstance := rateLimiterTestRuntime()

    key := "runtime:" + t.Name()

    limiter := NewRateLimiter(client, RateLimiterConfig{Limit: 1, Window: time.Minute, KeyPrefix: "melody:test:rate_limit:"})
    defer limiter.Reset(key)

    allowed, allowErr := limiter.AllowWithRuntime(runtimeInstance, key)
    if nil != allowErr || false == allowed {
        t.Fatalf("expected the first request to be allowed: %v %v", allowed, allowErr)
    }

    if true == limiter.Allow(key) {
        t.Fatalf("expected the plain path to see the same counter")
    }
}

/* the runtime context the http kernel hands a request carries no deadline, so the call timeout must bound AllowWithRuntime itself; a 1ns timeout must fail closed instead of riding the unbounded context */
func TestRateLimiter_AllowWithRuntimeAppliesCallTimeout(t *testing.T) {
    client := rateLimiterTestClient(t)
    runtimeInstance := rateLimiterTestRuntime()

    key := "timeout:" + t.Name()

    limiter := NewRateLimiter(client, RateLimiterConfig{Limit: 100, Window: time.Minute, KeyPrefix: "melody:test:rate_limit:"})
    /* below the rule's 1ms floor, set past the door so the deadline fires before any store answers */
    limiter.callTimeout = time.Nanosecond
    defer limiter.Reset(key)

    allowed, allowErr := limiter.AllowWithRuntime(runtimeInstance, key)
    if nil == allowErr {
        t.Fatalf("expected the 1ns call timeout to bound AllowWithRuntime and surface a deadline error")
    }

    if true == allowed {
        t.Fatalf("expected the fail-closed default to deny when the call timeout is exceeded")
    }
}

func TestRateLimiter_FailClosedDeniesOnStoreFailure(t *testing.T) {
    client := closedTestClient(t)

    var observed error
    limiter := NewRateLimiter(client, RateLimiterConfig{Limit: 100, Window: time.Minute, OnError: func(err error) {
        observed = err
    }})

    if true == limiter.Allow("failure:closed") {
        t.Fatalf("expected the fail-closed default to deny on a store failure")
    }

    if nil == observed {
        t.Fatalf("expected the store failure to reach the error observer")
    }
}

func TestRateLimiter_FailOpenAllowsOnStoreFailure(t *testing.T) {
    client := closedTestClient(t)

    limiter := NewRateLimiter(client, RateLimiterConfig{Limit: 100, Window: time.Minute, FailureMode: FailureModeOpen})

    if false == limiter.Allow("failure:open") {
        t.Fatalf("expected fail-open to allow on a store failure")
    }
}

func TestRateLimiter_AllowWithRuntimeReportsStoreFailure(t *testing.T) {
    client := closedTestClient(t)
    runtimeInstance := rateLimiterTestRuntime()

    limiter := NewRateLimiter(client, RateLimiterConfig{Limit: 100, Window: time.Minute})

    allowed, allowErr := limiter.AllowWithRuntime(runtimeInstance, "failure:runtime")
    if nil == allowErr {
        t.Fatalf("expected the store failure to be returned")
    }

    if true == allowed {
        t.Fatalf("expected the fail-closed default to deny alongside the returned error")
    }
}

func TestNewRateLimiter_AnAbsentConfigTakesTheDefaults(t *testing.T) {
    limiter := NewRateLimiter(&stubClient{}, RateLimiterConfig{Limit: 1, Window: time.Second})

    if defaultRateLimiterPrefix != limiter.prefix || FailureModeClosed != limiter.failureMode || 250*time.Millisecond != limiter.callTimeout {
        t.Fatalf("expected the default prefix, failure mode and 250ms budget, got %q, %q, %v", limiter.prefix, limiter.failureMode, limiter.callTimeout)
    }

    limiter = NewRateLimiter(&stubClient{}, RateLimiterConfig{Limit: 1, Window: time.Millisecond, KeyPrefix: "melody:test:given:", CallTimeout: bound.Of(time.Millisecond)})
    if time.Millisecond != limiter.window || time.Millisecond != limiter.callTimeout || "melody:test:given:" != limiter.prefix {
        t.Fatalf("expected a 1ms window and budget and the given prefix, got %v, %v, %q", limiter.window, limiter.callTimeout, limiter.prefix)
    }
}

func TestNewRateLimiter_RefusesAValueOutsideItsRuleNamingTheField(t *testing.T) {
    cases := map[string]struct {
        config RateLimiterConfig
        field  string
    }{
        "zero limit":                   {config: RateLimiterConfig{Window: time.Second}, field: "RateLimiterConfig.Limit"},
        "negative limit":               {config: RateLimiterConfig{Limit: -1, Window: time.Second}, field: "RateLimiterConfig.Limit"},
        "zero window":                  {config: RateLimiterConfig{Limit: 1}, field: "RateLimiterConfig.Window"},
        "sub-millisecond window":       {config: RateLimiterConfig{Limit: 1, Window: 500 * time.Microsecond}, field: "RateLimiterConfig.Window"},
        "misspelt failure mode":        {config: RateLimiterConfig{Limit: 1, Window: time.Second, FailureMode: "opne"}, field: "RateLimiterConfig.FailureMode"},
        "unlimited call timeout":       {config: RateLimiterConfig{Limit: 1, Window: time.Second, CallTimeout: bound.UnlimitedDuration()}, field: "RateLimiterConfig.CallTimeout"},
        "sub-millisecond call timeout": {config: RateLimiterConfig{Limit: 1, Window: time.Second, CallTimeout: bound.Of(time.Microsecond)}, field: "RateLimiterConfig.CallTimeout"},
    }

    for name, testCase := range cases {
        t.Run(name, func(t *testing.T) {
            expectPanicNamingField(t, testCase.field, func() {
                NewRateLimiter(&stubClient{}, testCase.config)
            })
        })
    }
}

func TestRateLimiter_ReArmsTheWindowOnAKeyThatLostItsExpiry(t *testing.T) {
    client := rateLimiterTestClient(t)

    key := "persisted:" + t.Name()
    fullKey := defaultRateLimiterPrefix + key

    limiter := NewRateLimiter(client, RateLimiterConfig{Limit: 5, Window: time.Minute})

    t.Cleanup(func() {
        _ = client.Do(context.Background(), client.B().Del().Key(fullKey).Build()).Error()
    })

    if deleteErr := client.Do(context.Background(), client.B().Del().Key(fullKey).Build()).Error(); nil != deleteErr {
        t.Fatalf("could not clear the key: %v", deleteErr)
    }

    limiter.Allow(key)

    if persistErr := client.Do(context.Background(), client.B().Persist().Key(fullKey).Build()).Error(); nil != persistErr {
        t.Fatalf("could not strip the expiry: %v", persistErr)
    }

    limiter.Allow(key)

    remaining, remainingErr := client.Do(context.Background(), client.B().Pttl().Key(fullKey).Build()).AsInt64()
    if nil != remainingErr {
        t.Fatalf("could not read the remaining ttl: %v", remainingErr)
    }

    if 0 >= remaining {
        t.Fatalf("expected the window to be re-armed on a key carrying no expiry, got a ttl of %d", remaining)
    }
}

/* the caller's own cancellation is named apart from a store failure: labelled a store failure it read as a redis outage against a healthy store, and the operator chased an outage that was a client hanging up. */
func TestRateLimiter_TheCallersCancellationIsNotAStoreFailureAndTakesTheFailureModeVerdict(t *testing.T) {
    client := rateLimiterTestClient(t)

    limiter := NewRateLimiter(client, RateLimiterConfig{Limit: 5, Window: time.Minute})

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    allowed, allowErr := limiter.allow(cancelledContext, cancelledContext, "cancel-classification")
    if nil == allowErr {
        t.Fatal("expected the cancelled call to fail")
    }

    if false == strings.Contains(allowErr.Error(), "cancelled by the caller") {
        t.Fatalf("expected the cancellation named apart from a store failure, got %q", allowErr.Error())
    }

    if true == allowed {
        t.Fatal("expected the default limiter to refuse a cancelled call, as it refuses a store failure")
    }

    openLimiter := NewRateLimiter(client, RateLimiterConfig{Limit: 5, Window: time.Minute, FailureMode: FailureModeOpen})

    openAllowed, openErr := openLimiter.allow(cancelledContext, cancelledContext, "cancel-classification")
    if nil == openErr || false == openAllowed {
        t.Fatalf("expected a limiter that fails open to admit a cancelled call and still report it, got allowed %v error %v", openAllowed, openErr)
    }
}

/* with no observer the failure is recorded here, and marked, because two of the three doors return nothing at all: Allow answers a bool and Reset answers nothing, so without this record a store outage refuses every call and reaches no channel whatsoever. The mark is what lets the record be filed at the one place that knows the key and the failure mode without the http middleware writing a second copy beside it. */
func TestRateLimiter_WithoutAnObserverTheFailureIsRecordedAndMarked(t *testing.T) {
    for _, testCase := range []struct {
        name    string
        cause   error
        level   loggingcontract.Level
        message string
    }{
        {
            name:    "a store outage",
            cause:   errors.New("connection refused"),
            level:   loggingcontract.LevelError,
            message: "rate limiter store failure",
        },
        {
            name:    "the caller's own cancellation is not an outage",
            cause:   context.Canceled,
            level:   loggingcontract.LevelWarning,
            message: "rate limiter call cancelled",
        },
        {
            name:    "the caller's own deadline is not an outage",
            cause:   errCallerDeadline,
            level:   loggingcontract.LevelWarning,
            message: "rate limiter call ended by the caller's deadline",
        },
    } {
        t.Run(testCase.name, func(t *testing.T) {
            limiter := &RateLimiter{}
            logger := &capturingLogger{}

            failure := exception.NewError("redis rate limiter store failure", map[string]any{"key": "actor"}, testCase.cause)

            reported := limiter.reportError(logger, failure)

            if 1 != len(logger.records) {
                t.Fatalf("expected exactly one record, got %d: %v", len(logger.records), logger.records)
            }

            if testCase.message != logger.records[0].message {
                t.Fatalf("message = %q, want %q", logger.records[0].message, testCase.message)
            }

            if testCase.level != logger.records[0].level {
                t.Fatalf("level = %v, want %v", logger.records[0].level, testCase.level)
            }

            if false == exception.IsAlreadyLogged(reported) {
                t.Fatal("the failure must come back marked, or the http sites file it a second time")
            }
        })
    }
}

/* an observer given by the application is the application's channel: it may be a counter rather than a journal, so the failure is handed over untouched and unmarked and whatever the caller does with it stays what it was */
func TestRateLimiter_AGivenObserverReplacesTheRecordAndLeavesTheErrorUnmarked(t *testing.T) {
    observed := make([]error, 0)
    logger := &capturingLogger{}

    limiter := &RateLimiter{onError: func(err error) {
        observed = append(observed, err)
    }}

    failure := exception.NewError("redis rate limiter store failure", map[string]any{"key": "actor"}, errors.New("connection refused"))

    reported := limiter.reportError(logger, failure)

    if 1 != len(observed) {
        t.Fatalf("the observer must receive the failure, got %d", len(observed))
    }

    if 0 != len(logger.records) {
        t.Fatalf("a given observer replaces the record rather than adding to it, got %v", logger.records)
    }

    if true == exception.IsAlreadyLogged(reported) {
        t.Fatal("a failure handed to the application's own observer must not be marked as journalled")
    }
}

/* Reset carries no runtime, so its failure is recorded in the logger the limiter was built with, inside the application's journal; a call's own logger still comes first, and without either the emergency logger is the last net */
func TestRateLimiter_RecordsAFailureWithoutACallLoggerInTheLoggerItWasBuiltWith(t *testing.T) {
    client := closedTestClient(t)
    configured := &capturingLogger{}

    limiter := NewRateLimiter(client, RateLimiterConfig{Limit: 100, Window: time.Minute, Logger: configured})
    limiter.Reset("account:user-2")

    if 1 != len(configured.records) || "rate limiter store failure" != configured.records[0].message {
        t.Fatalf("expected the failed reset recorded once in the configured logger, got %v", configured.records)
    }

    requestLogger := &capturingLogger{}
    failure := exception.NewError("redis rate limiter store failure", map[string]any{"key": "actor"}, errors.New("connection refused"))
    _ = limiter.reportError(requestLogger, failure)

    if 1 != len(requestLogger.records) || 1 != len(configured.records) {
        t.Fatalf("expected the call's own logger to take the record, got %v beside %v", requestLogger.records, configured.records)
    }
}

/* a typed nil handed through the config or by the call passes a plain nil check; read as absent, the record falls to the next logger rather than panicking inside the best-effort release that follows an accepted sign-in */
func TestRateLimiter_ReadsATypedNilLoggerAsAbsent(t *testing.T) {
    failure := exception.NewError("redis rate limiter store failure", map[string]any{"key": "actor"}, errors.New("connection refused"))

    configured := &capturingLogger{}
    limiter := &RateLimiter{logger: configured}
    _ = limiter.reportError((*capturingLogger)(nil), failure)

    if 1 != len(configured.records) {
        t.Fatalf("expected a typed-nil call logger to fall to the configured one, got %v", configured.records)
    }

    limiter = &RateLimiter{logger: (*capturingLogger)(nil)}
    if reported := limiter.reportError(nil, failure); false == exception.IsAlreadyLogged(reported) {
        t.Fatal("expected a typed-nil configured logger to fall to the emergency logger and mark the failure")
    }
}

/* a request deadline tighter than the call timeout ends the round trip with DeadlineExceeded as the call timeout would; the caller's context says whose deadline it was, and it is named so and recorded as a warning, not as a store failure. */
func TestRateLimiter_TheCallersDeadlineIsNotAStoreFailure(t *testing.T) {
    client := rateLimiterTestClient(t)

    limiter := NewRateLimiter(client, RateLimiterConfig{Limit: 5, Window: time.Minute})

    expiredParent, cancelParent := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
    defer cancelParent()

    callContext, cancelCall := context.WithTimeout(expiredParent, time.Minute)
    defer cancelCall()

    _, allowErr := limiter.allow(expiredParent, callContext, "deadline-classification")
    if nil == allowErr || false == strings.Contains(allowErr.Error(), "caller's deadline") {
        t.Fatalf("expected the caller's deadline named apart from a store failure, got %v", allowErr)
    }

    if false == errors.Is(allowErr, context.DeadlineExceeded) {
        t.Fatalf("expected the deadline to stay reachable through errors.Is, got %v", allowErr)
    }
}
