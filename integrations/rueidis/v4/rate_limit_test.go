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
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/redis/rueidis"
)

const rateLimiterTestPrefix = "melody:test:rate_limit:"

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

func rateLimiterTestRuntime() containercontract.Runtime {
    return rateLimiterTestRuntimeWith(context.Background())
}

func rateLimiterTestRuntimeWith(ctx context.Context) containercontract.Runtime {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    return runtime.NewRun(ctx, serviceContainer.NewScope())
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

func allowedBy(t *testing.T, limiter *RateLimiter, key string) bool {
    t.Helper()

    decision, allowErr := limiter.Allow(rateLimiterTestRuntime(), key)
    if nil != allowErr {
        t.Fatalf("unexpected limiter failure: %v", allowErr)
    }

    return decision.Allowed
}

func clearCounter(t *testing.T, client rueidis.Client, fullKey string) {
    t.Helper()

    if deleteErr := client.Do(context.Background(), client.B().Del().Key(fullKey).Build()).Error(); nil != deleteErr {
        t.Fatalf("could not clear %s: %v", fullKey, deleteErr)
    }

    t.Cleanup(func() {
        _ = client.Do(context.Background(), client.B().Del().Key(fullKey).Build()).Error()
    })
}

func TestRateLimiter_SharedLimitAcrossInstances(t *testing.T) {
    client := rateLimiterTestClient(t)
    clearCounter(t, client, rateLimiterTestPrefix+"shared:"+t.Name())

    first := NewRateLimiter(newTestConnection(client), "shared", 3, time.Minute, RateLimiterConfig{KeyPrefix: rateLimiterTestPrefix})
    second := NewRateLimiter(newTestConnection(client), "shared", 3, time.Minute, RateLimiterConfig{KeyPrefix: rateLimiterTestPrefix})

    if false == allowedBy(t, first, t.Name()) || false == allowedBy(t, second, t.Name()) || false == allowedBy(t, first, t.Name()) {
        t.Fatalf("expected the first three requests to be allowed")
    }

    if true == allowedBy(t, second, t.Name()) {
        t.Fatalf("expected the fourth request to be denied across instances")
    }
}

func TestRateLimiter_ResetClearsTheWindow(t *testing.T) {
    client := rateLimiterTestClient(t)
    clearCounter(t, client, rateLimiterTestPrefix+"reset:"+t.Name())

    limiter := NewRateLimiter(newTestConnection(client), "reset", 1, time.Minute, RateLimiterConfig{KeyPrefix: rateLimiterTestPrefix})

    if false == allowedBy(t, limiter, t.Name()) || true == allowedBy(t, limiter, t.Name()) {
        t.Fatalf("expected the first request allowed and the second denied")
    }

    if resetErr := limiter.Reset(rateLimiterTestRuntime(), t.Name()); nil != resetErr {
        t.Fatalf("unexpected reset failure: %v", resetErr)
    }

    if false == allowedBy(t, limiter, t.Name()) {
        t.Fatalf("expected the request after reset to be allowed")
    }
}

func TestRateLimiter_WindowExpires(t *testing.T) {
    client := rateLimiterTestClient(t)
    clearCounter(t, client, rateLimiterTestPrefix+"expiry:"+t.Name())

    limiter := NewRateLimiter(newTestConnection(client), "expiry", 1, 150*time.Millisecond, RateLimiterConfig{KeyPrefix: rateLimiterTestPrefix})

    if false == allowedBy(t, limiter, t.Name()) || true == allowedBy(t, limiter, t.Name()) {
        t.Fatalf("expected the first request allowed and the second denied inside the window")
    }

    time.Sleep(250 * time.Millisecond)

    if false == allowedBy(t, limiter, t.Name()) {
        t.Fatalf("expected a request in the next window to be allowed")
    }
}

func TestRateLimiter_TheRedisLimiterAnswersTheDecision(t *testing.T) {
    client := rateLimiterTestClient(t)
    clearCounter(t, client, rateLimiterTestPrefix+"decision:"+t.Name())

    limiter := NewRateLimiter(newTestConnection(client), "decision", 2, time.Minute, RateLimiterConfig{KeyPrefix: rateLimiterTestPrefix})

    for index, expected := range []httpcontract.RateLimitDecision{
        {Allowed: true, Remaining: 1, Limit: 2},
        {Allowed: true, Remaining: 0, Limit: 2},
    } {
        decision, allowErr := limiter.Allow(rateLimiterTestRuntime(), t.Name())
        if nil != allowErr || expected != decision {
            t.Fatalf("call %d: expected %+v, got %+v %v", index+1, expected, decision, allowErr)
        }
    }

    decision, allowErr := limiter.Allow(rateLimiterTestRuntime(), t.Name())
    if nil != allowErr || true == decision.Allowed || 0 != decision.Remaining || 2 != decision.Limit {
        t.Fatalf("expected the third call denied, got %+v %v", decision, allowErr)
    }

    if 0 >= decision.RetryAfter || time.Minute < decision.RetryAfter {
        t.Fatalf("expected a RetryAfter inside the window, got %s", decision.RetryAfter)
    }
}

func TestRateLimiter_RedisRetryAfterIsTheCountersTtl(t *testing.T) {
    client := rateLimiterTestClient(t)
    fullKey := rateLimiterTestPrefix + "ttl:" + t.Name()
    clearCounter(t, client, fullKey)

    limiter := NewRateLimiter(newTestConnection(client), "ttl", 1, time.Minute, RateLimiterConfig{KeyPrefix: rateLimiterTestPrefix})
    allowedBy(t, limiter, t.Name())

    if expireErr := client.Do(context.Background(), client.B().Pexpire().Key(fullKey).Milliseconds(7_000).Build()).Error(); nil != expireErr {
        t.Fatalf("could not set the counter's ttl: %v", expireErr)
    }

    decision, allowErr := limiter.Allow(rateLimiterTestRuntime(), t.Name())
    if nil != allowErr || true == decision.Allowed {
        t.Fatalf("expected a denial, got %+v %v", decision, allowErr)
    }

    if 6*time.Second > decision.RetryAfter || 7*time.Second < decision.RetryAfter {
        t.Fatalf("expected the counter's 7s ttl as RetryAfter, got %s", decision.RetryAfter)
    }
}

func TestRateLimiter_AKeyWithoutATtlAnswersNoRetryAfterAndIsRearmed(t *testing.T) {
    client := rateLimiterTestClient(t)
    fullKey := rateLimiterTestPrefix + "persisted:" + t.Name()
    clearCounter(t, client, fullKey)

    limiter := NewRateLimiter(newTestConnection(client), "persisted", 1, time.Minute, RateLimiterConfig{KeyPrefix: rateLimiterTestPrefix})
    allowedBy(t, limiter, t.Name())

    if persistErr := client.Do(context.Background(), client.B().Persist().Key(fullKey).Build()).Error(); nil != persistErr {
        t.Fatalf("could not strip the expiry: %v", persistErr)
    }

    decision, allowErr := limiter.Allow(rateLimiterTestRuntime(), t.Name())
    if nil != allowErr || true == decision.Allowed || 0 != decision.RetryAfter {
        t.Fatalf("expected a denial with no RetryAfter, got %+v %v", decision, allowErr)
    }

    remaining, remainingErr := client.Do(context.Background(), client.B().Pttl().Key(fullKey).Build()).AsInt64()
    if nil != remainingErr {
        t.Fatalf("could not read the remaining ttl: %v", remainingErr)
    }

    if 0 >= remaining {
        t.Fatalf("expected the window to be re-armed on a key carrying no expiry, got a ttl of %d", remaining)
    }
}

func TestRateLimiter_TwoLimitersOverOneClientDoNotShareACounter(t *testing.T) {
    client := rateLimiterTestClient(t)
    clearCounter(t, client, "melody:rate_limit:"+"login:"+t.Name())
    clearCounter(t, client, "melody:rate_limit:"+"api:"+t.Name())

    strict := NewRateLimiter(newTestConnection(client), "login", 1, time.Minute, RateLimiterConfig{})
    loose := NewRateLimiter(newTestConnection(client), "api", 100, time.Minute, RateLimiterConfig{})

    for index := 0; index < 5; index++ {
        if false == allowedBy(t, loose, t.Name()) {
            t.Fatalf("expected the loose limiter to admit call %d", index+1)
        }
    }

    if false == allowedBy(t, strict, t.Name()) {
        t.Fatalf("expected the strict limiter's first call admitted: the loose limiter's calls charged its counter")
    }
}

func TestRateLimiter_TheCounterKeyIsPrefixNameKey(t *testing.T) {
    client := rateLimiterTestClient(t)
    fullKey := "melody:test:frozen:" + "login" + ":" + t.Name()
    clearCounter(t, client, fullKey)

    limiter := NewRateLimiter(newTestConnection(client), "login", 5, time.Minute, RateLimiterConfig{KeyPrefix: "melody:test:frozen:"})
    allowedBy(t, limiter, t.Name())

    count, countErr := client.Do(context.Background(), client.B().Get().Key(fullKey).Build()).AsInt64()
    if nil != countErr || 1 != count {
        t.Fatalf("expected the counter under %s, got %d %v", fullKey, count, countErr)
    }

    if "login" != limiter.Name() {
        t.Fatalf("expected the limiter to answer its name, got %q", limiter.Name())
    }
}

func TestRateLimiter_ALimitersCountersSurviveAChangeOfItsLimitAndWindow(t *testing.T) {
    client := rateLimiterTestClient(t)
    clearCounter(t, client, rateLimiterTestPrefix+"survive:"+t.Name())

    before := NewRateLimiter(newTestConnection(client), "survive", 2, time.Minute, RateLimiterConfig{KeyPrefix: rateLimiterTestPrefix})
    allowedBy(t, before, t.Name())
    allowedBy(t, before, t.Name())

    after := NewRateLimiter(newTestConnection(client), "survive", 3, 2*time.Minute, RateLimiterConfig{KeyPrefix: rateLimiterTestPrefix})
    decision, allowErr := after.Allow(rateLimiterTestRuntime(), t.Name())
    if nil != allowErr || false == decision.Allowed || 0 != decision.Remaining {
        t.Fatalf("expected the third call charged to the same counter, the last of the new limit, got %+v %v", decision, allowErr)
    }
}

/* the runtime context the http kernel hands a request carries no deadline, so the call timeout must bound Allow itself; a 1ns timeout must fail closed instead of riding the unbounded context */
func TestRateLimiter_ACallCutByTheCallTimeoutNamesTheBound(t *testing.T) {
    client := rateLimiterTestClient(t)

    limiter := NewRateLimiter(newTestConnection(client), "timeout", 100, time.Minute, RateLimiterConfig{KeyPrefix: rateLimiterTestPrefix})
    limiter.callTimeout = time.Nanosecond

    decision, allowErr := limiter.Allow(rateLimiterTestRuntime(), t.Name())
    if nil == allowErr || true == decision.Allowed {
        t.Fatalf("expected the 1ns call timeout to deny with an error, got %+v %v", decision, allowErr)
    }

    if false == exception.IsInfrastructureFailure(allowErr) || "RateLimiterConfig.CallTimeout" != exceptionContextOf(allowErr)["bound"] {
        t.Fatalf("expected a marked failure naming the bound, got %v %v", allowErr, exceptionContextOf(allowErr))
    }
}

func TestRateLimiter_AStoreFailureIsReturnedMarked(t *testing.T) {
    for _, failureMode := range []RateLimiterFailureMode{FailureModeClosed, FailureModeOpen} {
        limiter := NewRateLimiter(newTestConnection(closedTestClient(t)), "failure", 100, time.Minute, RateLimiterConfig{FailureMode: failureMode})

        decision, allowErr := limiter.Allow(rateLimiterTestRuntime(), "failure:marked")
        if false == exception.IsInfrastructureFailure(allowErr) {
            t.Fatalf("%s: expected the store failure marked, got %v", failureMode, allowErr)
        }

        if (FailureModeOpen == failureMode) != decision.Allowed || 100 != decision.Limit {
            t.Fatalf("%s: expected the failure-mode answer, got %+v", failureMode, decision)
        }

        if true == exception.IsAlreadyLogged(allowErr) {
            t.Fatalf("%s: expected the failure returned unlogged, for the caller to record", failureMode)
        }
    }
}

func TestRateLimiter_ResetReportsAStoreFailureMarked(t *testing.T) {
    limiter := NewRateLimiter(newTestConnection(closedTestClient(t)), "reset", 1, time.Minute, RateLimiterConfig{})

    resetErr := limiter.Reset(rateLimiterTestRuntime(), "account:user-2")
    if false == exception.IsInfrastructureFailure(resetErr) || false == strings.Contains(resetErr.Error(), "redis rate limiter reset failed") {
        t.Fatalf("expected the failed reset returned marked, got %v", resetErr)
    }
}

func TestRateLimiter_ResetIsCancelledWithTheRuntimesContext(t *testing.T) {
    client, commands := newCommandCountingClient(t)
    limiter := NewRateLimiter(newTestConnection(client), "reset", 1, time.Minute, RateLimiterConfig{})

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    before := commands.Load()
    resetErr := limiter.Reset(rateLimiterTestRuntimeWith(cancelledContext), "account:user-2")
    if nil == resetErr || false == errors.Is(resetErr, context.Canceled) {
        t.Fatalf("expected the reset cancelled with the runtime's context, got %v", resetErr)
    }

    if before != commands.Load() {
        t.Fatalf("expected no round trip for a cancelled reset, got %d", commands.Load()-before)
    }
}

func TestRateLimiter_AllowRefusesANilRuntime(t *testing.T) {
    limiter := NewRateLimiter(newTestConnection(&stubClient{}), "nil", 1, time.Minute, RateLimiterConfig{FailureMode: FailureModeOpen})

    var typedNil *contextOnlyRuntime
    for name, runtimeInstance := range map[string]containercontract.Runtime{
        "nil":         nil,
        "typed nil":   typedNil,
        "nil context": &contextOnlyRuntime{},
    } {
        decision, allowErr := limiter.Allow(runtimeInstance, "key")
        if nil == allowErr || true == decision.Allowed || true == exception.AnswersUnavailable(allowErr) {
            t.Fatalf("%s: expected an unmarked refusal and a denial, got %+v %v", name, decision, allowErr)
        }

        if resetErr := limiter.Reset(runtimeInstance, "key"); nil == resetErr || true == exception.AnswersUnavailable(resetErr) {
            t.Fatalf("%s: expected Reset to refuse unmarked, got %v", name, resetErr)
        }
    }
}

func TestNewRateLimiter_AnAbsentConfigTakesTheDefaults(t *testing.T) {
    limiter := NewRateLimiter(newTestConnection(&stubClient{}), "defaults", 1, time.Second, RateLimiterConfig{})

    if "melody:rate_limit:" != limiter.prefix || FailureModeClosed != limiter.failureMode || 250*time.Millisecond != limiter.callTimeout {
        t.Fatalf("expected the default prefix, failure mode and 250ms budget, got %q, %q, %v", limiter.prefix, limiter.failureMode, limiter.callTimeout)
    }

    limiter = NewRateLimiter(newTestConnection(&stubClient{}), "given", 1, time.Millisecond, RateLimiterConfig{KeyPrefix: "melody:test:given:", CallTimeout: bound.Of(time.Millisecond)})
    if time.Millisecond != limiter.window || time.Millisecond != limiter.callTimeout || "melody:test:given:" != limiter.prefix {
        t.Fatalf("expected a 1ms window and budget and the given prefix, got %v, %v, %q", limiter.window, limiter.callTimeout, limiter.prefix)
    }
}

func TestNewRateLimiter_RefusesAValueOutsideItsRuleNamingTheField(t *testing.T) {
    cases := map[string]struct {
        config RateLimiterConfig
        field  string
    }{
        "misspelt failure mode":        {config: RateLimiterConfig{FailureMode: "opne"}, field: "RateLimiterConfig.FailureMode"},
        "unlimited call timeout":       {config: RateLimiterConfig{CallTimeout: bound.UnlimitedDuration()}, field: "RateLimiterConfig.CallTimeout"},
        "sub-millisecond call timeout": {config: RateLimiterConfig{CallTimeout: bound.Of(time.Microsecond)}, field: "RateLimiterConfig.CallTimeout"},
    }

    for name, testCase := range cases {
        t.Run(name, func(t *testing.T) {
            expectPanicNamingField(t, testCase.field, func() {
                NewRateLimiter(newTestConnection(&stubClient{}), "test", 1, time.Second, testCase.config)
            })
        })
    }
}

func expectPanicNamingArgument(t *testing.T, message string, argument string, callback func()) {
    t.Helper()

    defer func() {
        t.Helper()

        recoveredErr, isError := recover().(error)
        if false == isError {
            t.Fatalf("expected a refusal naming %s, got no error panic", argument)
        }

        if false == strings.Contains(recoveredErr.Error(), message) || argument != exceptionContextOf(recoveredErr)["argument"] {
            t.Fatalf("expected %q naming the argument %s, got %v %v", message, argument, recoveredErr, exceptionContextOf(recoveredErr))
        }
    }()

    callback()
}

func TestRateLimiter_ALimitBelowOneAndAWindowBelowAMillisecondAreRefusedNamingTheArgument(t *testing.T) {
    for name, testCase := range map[string]struct {
        limit    int
        window   time.Duration
        argument string
    }{
        "zero limit":             {limit: 0, window: time.Second, argument: "limit"},
        "negative limit":         {limit: -1, window: time.Second, argument: "limit"},
        "zero window":            {limit: 1, window: 0, argument: "window"},
        "sub-millisecond window": {limit: 1, window: 500 * time.Microsecond, argument: "window"},
    } {
        t.Run(name, func(t *testing.T) {
            expectPanicNamingArgument(t, "the limit is below its minimum", testCase.argument, func() {
                NewRateLimiter(newTestConnection(&stubClient{}), "test", testCase.limit, testCase.window, RateLimiterConfig{})
            })
        })
    }
}

func TestRateLimiter_AnEmptyOrMalformedNameIsRefusedAtConstruction(t *testing.T) {
    for _, refused := range []string{"", "a:b", "a b", strings.Repeat("a", 65), "é", "a/b"} {
        expectPanicNamingArgument(t, "the limiter name is malformed", "name", func() {
            NewRateLimiter(newTestConnection(&stubClient{}), refused, 1, time.Second, RateLimiterConfig{})
        })
    }

    for _, admitted := range []string{"a", "Login-2fa_budget.v1", strings.Repeat("z", 64)} {
        NewRateLimiter(newTestConnection(&stubClient{}), admitted, 1, time.Second, RateLimiterConfig{})
    }
}

/* the caller's own cancellation is named apart from a store failure: labelled a store failure it read as a redis outage against a healthy store, and the operator chased an outage that was a client hanging up. */
func TestRateLimiter_TheCallersCancellationIsNotAStoreFailureAndTakesTheFailureModeVerdict(t *testing.T) {
    client := rateLimiterTestClient(t)

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    limiter := NewRateLimiter(newTestConnection(client), "cancel", 5, time.Minute, RateLimiterConfig{KeyPrefix: rateLimiterTestPrefix})

    decision, allowErr := limiter.Allow(rateLimiterTestRuntimeWith(cancelledContext), "cancel-classification")
    if nil == allowErr || false == strings.Contains(allowErr.Error(), "cancelled by the caller") {
        t.Fatalf("expected the cancellation named apart from a store failure, got %v", allowErr)
    }

    if true == decision.Allowed || true == exception.IsInfrastructureFailure(allowErr) {
        t.Fatalf("expected the default limiter to refuse a cancelled call unmarked, got %+v %v", decision, allowErr)
    }

    openLimiter := NewRateLimiter(newTestConnection(client), "cancel", 5, time.Minute, RateLimiterConfig{KeyPrefix: rateLimiterTestPrefix, FailureMode: FailureModeOpen})

    openDecision, openErr := openLimiter.Allow(rateLimiterTestRuntimeWith(cancelledContext), "cancel-classification")
    if nil == openErr || false == openDecision.Allowed {
        t.Fatalf("expected a limiter that fails open to admit a cancelled call and still report it, got %+v %v", openDecision, openErr)
    }
}

/* a request deadline tighter than the call timeout ends the round trip with DeadlineExceeded as the call timeout would; the caller's context says whose deadline it was, and it is named so, not as a store failure. */
func TestRateLimiter_TheCallersDeadlineIsNotAStoreFailure(t *testing.T) {
    client := rateLimiterTestClient(t)

    limiter := NewRateLimiter(newTestConnection(client), "deadline", 5, time.Minute, RateLimiterConfig{KeyPrefix: rateLimiterTestPrefix})

    expiredParent, cancelParent := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
    defer cancelParent()

    _, allowErr := limiter.Allow(rateLimiterTestRuntimeWith(expiredParent), "deadline-classification")
    if nil == allowErr || false == strings.Contains(allowErr.Error(), "caller's deadline") || true == exception.IsInfrastructureFailure(allowErr) {
        t.Fatalf("expected the caller's deadline named apart from a store failure and unmarked, got %v", allowErr)
    }

    if false == errors.Is(allowErr, context.DeadlineExceeded) {
        t.Fatalf("expected the deadline to stay reachable through errors.Is, got %v", allowErr)
    }
}

func TestRateLimiter_FailureReturnsOnACyclicChainAsAStoreFailure(t *testing.T) {
    first := &rateLimiterCyclicError{}
    first.next = &rateLimiterCyclicError{next: first}

    endedContext, cancel := context.WithCancel(context.Background())
    cancel()

    limiter := &RateLimiter{failureMode: FailureModeClosed}

    done := make(chan struct{})
    go func() {
        defer close(done)

        for _, parentContext := range []context.Context{context.Background(), endedContext} {
            failure := limiter.failure(parentContext, context.Background(), "key", first, "cancelled", "deadline", "redis rate limiter store failure")
            if nil == failure || false == strings.Contains(failure.Error(), "redis rate limiter store failure") {
                t.Errorf("expected a store failure, got %v", failure)
            }
        }
    }()

    select {
    case <-done:
    case <-time.After(5 * time.Second):
        t.Fatal("expected the call to return on an error chain that reaches itself")
    }
}

type rateLimiterCyclicError struct {
    next error
}

func (instance *rateLimiterCyclicError) Error() string {
    return "cyclic link"
}

func (instance *rateLimiterCyclicError) Unwrap() error {
    return instance.next
}

func TestRateLimiter_TheCallersOwnEndIsNotMarked(t *testing.T) {
    limiter := &RateLimiter{failureMode: FailureModeClosed}

    cancelled := limiter.failure(context.Background(), context.Background(), "key", context.Canceled, "cancelled", "deadline", "store")
    if nil == cancelled || true == exception.IsInfrastructureFailure(cancelled) {
        t.Fatalf("expected the caller's cancellation unmarked, got %v", cancelled)
    }

    endedContext, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
    defer cancel()
    <-endedContext.Done()

    deadline := limiter.failure(endedContext, endedContext, "key", context.DeadlineExceeded, "cancelled", "deadline", "store")
    if nil == deadline || true == exception.IsInfrastructureFailure(deadline) {
        t.Fatalf("expected the caller's own deadline unmarked, got %v", deadline)
    }

    passedDeadline := lapsedDeadlineContext{Context: context.Background()}
    readEarly := limiter.failure(passedDeadline, passedDeadline, "key", context.DeadlineExceeded, "cancelled", "deadline", "store")
    if nil == readEarly || true == exception.IsInfrastructureFailure(readEarly) {
        t.Fatalf("expected a caller's deadline already passed, not yet recorded, unmarked, got %v", readEarly)
    }

    callTimeout := limiter.failure(context.Background(), context.Background(), "key", context.DeadlineExceeded, "cancelled", "deadline", "store")
    if false == exception.IsInfrastructureFailure(callTimeout) {
        t.Fatalf("expected the call timeout of a slow store marked, got %v", callTimeout)
    }
}
