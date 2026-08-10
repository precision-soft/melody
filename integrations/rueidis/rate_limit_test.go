package rueidis

import (
    "context"
    "os"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/container"
    "github.com/precision-soft/melody/runtime"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
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
