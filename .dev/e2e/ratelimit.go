package main

import (
    "fmt"
    "time"

    melodyrueidis "github.com/precision-soft/melody/integrations/rueidis/v3"
)

/* runRateLimitCheck exercises the redis-backed fixed-window rate limiter against a live redis: the shared
counter admits exactly the configured budget and denies the next call, a fresh window restores the budget,
Reset drops the counter, and — the behaviour a fake backend cannot prove — a store outage follows the
configured failure mode, denying by default and admitting only when the caller opts into fail-open. */
func runRateLimitCheck(address string) {
    provider := melodyrueidis.NewProvider()

    client, openErr := provider.Open(melodyrueidis.NewConnectionParams(address, "", ""))
    if nil != openErr {
        fail("rate limit: open redis: %v", openErr)
    }
    defer client.Close()

    prefix := fmt.Sprintf("melody-e2e:rate_limit:%d:", time.Now().UnixNano())

    /* the budget is spent exactly once, then the limiter denies */
    limiter := melodyrueidis.NewRateLimiter(client, 3, time.Minute, melodyrueidis.WithRateLimiterKeyPrefix(prefix))

    key := "actor-1"
    for attempt := 1; attempt <= 3; attempt++ {
        if false == limiter.Allow(key) {
            fail("rate limit: call %d of 3 was denied inside the budget", attempt)
        }
    }
    pass("rate limiter admitted the full budget (3 of 3)")

    if true == limiter.Allow(key) {
        fail("rate limit: the call past the budget was admitted")
    }
    pass("rate limiter denied the call past the budget")

    /* a distinct key carries its own budget */
    if false == limiter.Allow("actor-2") {
        fail("rate limit: a distinct key was denied on its first call")
    }
    pass("rate limiter budgets are per key")

    /* Reset drops the counter and restores the budget */
    limiter.Reset(key)

    if false == limiter.Allow(key) {
        fail("rate limit: the key was still denied after Reset")
    }
    pass("rate limiter Reset restored the budget")

    /* a window that lapses restores the budget without a Reset */
    windowLimiter := melodyrueidis.NewRateLimiter(
        client,
        1,
        300*time.Millisecond,
        melodyrueidis.WithRateLimiterKeyPrefix(prefix+"window:"),
    )

    windowKey := "actor-window"
    if false == windowLimiter.Allow(windowKey) {
        fail("rate limit: the first call of a fresh window was denied")
    }
    if true == windowLimiter.Allow(windowKey) {
        fail("rate limit: the second call inside the window was admitted")
    }

    time.Sleep(600 * time.Millisecond)

    if false == windowLimiter.Allow(windowKey) {
        fail("rate limit: the window lapsed but the key stayed denied")
    }
    pass("rate limiter window lapsed and restored the budget")

    /* an unreachable store denies by default (fail-closed) and admits only under an explicit fail-open */
    brokenClient, brokenOpenErr := provider.Open(melodyrueidis.NewConnectionParams(address, "", ""))
    if nil != brokenOpenErr {
        fail("rate limit: open the redis client to break: %v", brokenOpenErr)
    }
    brokenClient.Close()

    var observedFailure error
    closedLimiter := melodyrueidis.NewRateLimiter(
        brokenClient,
        10,
        time.Minute,
        melodyrueidis.WithRateLimiterKeyPrefix(prefix),
        melodyrueidis.WithRateLimiterOnError(func(err error) { observedFailure = err }),
    )

    if true == closedLimiter.Allow("actor-closed") {
        fail("rate limit: an unreachable store admitted the call under the fail-closed default")
    }
    if nil == observedFailure {
        fail("rate limit: the store failure was not reported to the error observer")
    }
    pass("rate limiter failed closed on an unreachable store (and reported the failure)")

    openLimiter := melodyrueidis.NewRateLimiter(
        brokenClient,
        10,
        time.Minute,
        melodyrueidis.WithRateLimiterKeyPrefix(prefix),
        melodyrueidis.WithRateLimiterFailureMode(melodyrueidis.FailureModeOpen),
    )

    if false == openLimiter.Allow("actor-open") {
        fail("rate limit: an unreachable store denied the call under an explicit fail-open")
    }
    pass("rate limiter failed open on an unreachable store when explicitly opted in")

    /* AllowWithRuntime threads the request context and surfaces the store error to the caller */
    _, runtimeErr := closedLimiter.AllowWithRuntime(newRuntime(), "actor-runtime")
    if nil == runtimeErr {
        fail("rate limit: AllowWithRuntime hid the store failure from the caller")
    }
    pass("rate limiter AllowWithRuntime surfaced the store failure")
}
