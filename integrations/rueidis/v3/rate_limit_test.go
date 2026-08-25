package rueidis

import (
    "context"
    "errors"
    "os"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/container"
    "github.com/precision-soft/melody/v3/exception"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
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
    first := NewRateLimiter(client, 3, time.Minute, WithRateLimiterKeyPrefix("melody:test:rate_limit:"))
    second := NewRateLimiter(client, 3, time.Minute, WithRateLimiterKeyPrefix("melody:test:rate_limit:"))
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

    limiter := NewRateLimiter(client, 1, time.Minute, WithRateLimiterKeyPrefix("melody:test:rate_limit:"))
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

    limiter := NewRateLimiter(client, 1, 150*time.Millisecond, WithRateLimiterKeyPrefix("melody:test:rate_limit:"))
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

    limiter := NewRateLimiter(client, 1, time.Minute, WithRateLimiterKeyPrefix("melody:test:rate_limit:"))
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

    limiter := NewRateLimiter(
        client,
        100,
        time.Minute,
        WithRateLimiterKeyPrefix("melody:test:rate_limit:"),
        WithRateLimiterCallTimeout(time.Nanosecond),
    )
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
    limiter := NewRateLimiter(client, 100, time.Minute, WithRateLimiterOnError(func(err error) {
        observed = err
    }))

    if true == limiter.Allow("failure:closed") {
        t.Fatalf("expected the fail-closed default to deny on a store failure")
    }

    if nil == observed {
        t.Fatalf("expected the store failure to reach the error observer")
    }
}

func TestRateLimiter_FailOpenAllowsOnStoreFailure(t *testing.T) {
    client := closedTestClient(t)

    limiter := NewRateLimiter(client, 100, time.Minute, WithRateLimiterFailureMode(FailureModeOpen))

    if false == limiter.Allow("failure:open") {
        t.Fatalf("expected fail-open to allow on a store failure")
    }
}

func TestRateLimiter_AllowWithRuntimeReportsStoreFailure(t *testing.T) {
    client := closedTestClient(t)
    runtimeInstance := rateLimiterTestRuntime()

    limiter := NewRateLimiter(client, 100, time.Minute)

    allowed, allowErr := limiter.AllowWithRuntime(runtimeInstance, "failure:runtime")
    if nil == allowErr {
        t.Fatalf("expected the store failure to be returned")
    }

    if true == allowed {
        t.Fatalf("expected the fail-closed default to deny alongside the returned error")
    }
}

func TestRateLimiter_NonPositiveCallTimeoutFallsBackToTheDefault(t *testing.T) {
    /* a non-positive call timeout must not survive verbatim: context.WithTimeout(Background(), 0) is born cancelled, forcing every Allow/Reset onto the store-failure path forever */
    cases := map[string]time.Duration{
        "zero":     0,
        "negative": -1 * time.Second,
    }

    for name, timeout := range cases {
        t.Run(name, func(t *testing.T) {
            instance := &RateLimiter{callTimeout: defaultRateLimiterCallTimeout}

            WithRateLimiterCallTimeout(timeout)(instance)

            if defaultRateLimiterCallTimeout != instance.callTimeout {
                t.Fatalf("expected a %v call timeout to fall back to the default, got %v", timeout, instance.callTimeout)
            }
        })
    }
}

func TestRateLimiter_PositiveCallTimeoutIsKept(t *testing.T) {
    instance := &RateLimiter{callTimeout: defaultRateLimiterCallTimeout}

    WithRateLimiterCallTimeout(750 * time.Millisecond)(instance)

    if 750*time.Millisecond != instance.callTimeout {
        t.Fatalf("expected a positive call timeout to be kept, got %v", instance.callTimeout)
    }
}

type capturingLimiterLogger struct {
    records []capturedLimiterRecord
}

type capturedLimiterRecord struct {
    level   loggingcontract.Level
    message string
}

func (instance *capturingLimiterLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
    instance.records = append(instance.records, capturedLimiterRecord{level: level, message: message})
}

func (instance *capturingLimiterLogger) Debug(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelDebug, message, context)
}

func (instance *capturingLimiterLogger) Info(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelInfo, message, context)
}

func (instance *capturingLimiterLogger) Warning(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelWarning, message, context)
}

func (instance *capturingLimiterLogger) Error(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelError, message, context)
}

func (instance *capturingLimiterLogger) Emergency(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelEmergency, message, context)
}

func TestRateLimiter_ReArmsTheWindowOnAKeyThatLostItsExpiry(t *testing.T) {
    client := rateLimiterTestClient(t)

    key := "persisted:" + t.Name()
    fullKey := defaultRateLimiterPrefix + key

    limiter := NewRateLimiter(client, 5, time.Minute)

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
func TestRateLimiter_TheCallersCancellationIsNotAStoreFailure(t *testing.T) {
    client := rateLimiterTestClient(t)

    limiter := NewRateLimiter(client, 5, time.Minute)

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    _, allowErr := limiter.allow(cancelledContext, "cancel-classification")
    if nil == allowErr {
        t.Fatal("expected the cancelled call to fail")
    }

    if false == strings.Contains(allowErr.Error(), "cancelled by the caller") {
        t.Fatalf("expected the cancellation named apart from a store failure, got %q", allowErr.Error())
    }
}

/* with no observer the failure is recorded here, and marked, because two of the three doors return nothing at all: Allow answers a bool and Reset answers nothing, so a store outage refused every call and reached no channel whatsoever. The mark is what lets the record be filed at the one place that knows the key and the failure mode without the http middleware writing a second copy beside it. */
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
    } {
        t.Run(testCase.name, func(t *testing.T) {
            limiter := &RateLimiter{}
            logger := &capturingLimiterLogger{}

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
    logger := &capturingLimiterLogger{}

    limiter := &RateLimiter{}
    WithRateLimiterOnError(func(err error) {
        observed = append(observed, err)
    })(limiter)

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
