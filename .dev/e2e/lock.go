package main

import (
    "context"
    "errors"
    "fmt"
    "sync"
    "sync/atomic"
    "time"

    melodyrueidis "github.com/precision-soft/melody/integrations/rueidis/v3"
    "github.com/precision-soft/melody/v3/container"
    "github.com/precision-soft/melody/v3/lock"
    lockcontract "github.com/precision-soft/melody/v3/lock/contract"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "github.com/redis/rueidis"
)

/* runRunExclusiveCheck exercises lock.RunExclusive over a live redis lease locker: a second caller is turned away while the first holds the lock, the lock is released as soon as the callback returns so the next tick runs, and an unreachable store fails closed rather than double-running the work. This is the primitive that keeps a cron-launched command running exactly once per tick across N application instances. */
func runRunExclusiveCheck(address string) {
    client := openRedis(address)
    defer client.Close()

    locker := melodyrueidis.NewLocker(client)
    name := fmt.Sprintf("melody:e2e:exclusive:%d", time.Now().UnixNano())

    /* while the first caller holds the lock, a second caller must be turned away without running the callback */
    var contenderRan atomic.Bool
    holderInside := make(chan struct{})
    holderMayFinish := make(chan struct{})

    var waitGroup sync.WaitGroup
    waitGroup.Add(1)

    var holderRan bool
    var holderErr error

    go func() {
        defer waitGroup.Done()

        holderRan, holderErr = lock.RunExclusive(
            newRuntime(),
            locker,
            name,
            30*time.Second,
            func(runtimecontract.Runtime) error {
                close(holderInside)
                <-holderMayFinish
                return nil
            },
        )
    }()

    <-holderInside

    contenderReported, contenderErr := lock.RunExclusive(
        newRuntime(),
        locker,
        name,
        30*time.Second,
        func(runtimecontract.Runtime) error {
            contenderRan.Store(true)
            return nil
        },
    )
    if nil != contenderErr {
        fail("run exclusive: the contender errored while the lock was held: %v", contenderErr)
    }
    if true == contenderReported {
        fail("run exclusive: the contender reported that it ran while the lock was held")
    }
    if true == contenderRan.Load() {
        fail("run exclusive: the contender's callback ran while another holder owned the lock")
    }
    pass("run exclusive turned the contender away while the lock was held")

    close(holderMayFinish)
    waitGroup.Wait()

    if nil != holderErr {
        fail("run exclusive: the holder errored: %v", holderErr)
    }
    if false == holderRan {
        fail("run exclusive: the holder did not report that it ran")
    }
    pass("run exclusive ran the holder's callback to completion")

    /* the lock is released when the callback returns, so the very next tick runs rather than waiting out the ttl */
    nextTickRan, nextTickErr := lock.RunExclusive(
        newRuntime(),
        locker,
        name,
        30*time.Second,
        func(runtimecontract.Runtime) error { return nil },
    )
    if nil != nextTickErr {
        fail("run exclusive: the next tick errored: %v", nextTickErr)
    }
    if false == nextTickRan {
        fail("run exclusive: the lock outlived the callback — the next tick was skipped")
    }
    pass("run exclusive released the lock when the callback returned (next tick ran)")

    /* the callback's error reaches the caller, and the lock is still released */
    sentinel := fmt.Errorf("work failed")
    failedRan, failedErr := lock.RunExclusive(
        newRuntime(),
        locker,
        name,
        30*time.Second,
        func(runtimecontract.Runtime) error { return sentinel },
    )
    if false == failedRan {
        fail("run exclusive: a failing callback was reported as not run")
    }
    if nil == failedErr {
        fail("run exclusive: the callback's error was swallowed")
    }
    if false == errors.Is(failedErr, sentinel) {
        fail("run exclusive: the callback's error reached the caller as %v, not as the error the callback returned", failedErr)
    }
    pass("run exclusive surfaced the callback's error")

    /* the claim above is "and the lock is still released": prove it, or a release that regressed would go unnoticed */
    afterFailureRan, afterFailureErr := lock.RunExclusive(
        newRuntime(),
        locker,
        name,
        30*time.Second,
        func(runtimecontract.Runtime) error { return nil },
    )
    if nil != afterFailureErr {
        fail("run exclusive: the tick after a failing callback errored: %v", afterFailureErr)
    }
    if false == afterFailureRan {
        fail("run exclusive: a failing callback left the lock held — the next tick was skipped")
    }
    pass("run exclusive released the lock even when the callback failed")

    /* an unreachable store must fail closed: no run, and the error reaches the caller */
    brokenClient := openRedis(address)
    brokenClient.Close()

    var brokenFnRan atomic.Bool
    brokenRan, brokenErr := lock.RunExclusive(
        newRuntime(),
        melodyrueidis.NewLocker(brokenClient),
        name,
        30*time.Second,
        func(runtimecontract.Runtime) error {
            brokenFnRan.Store(true)
            return nil
        },
    )
    if nil == brokenErr {
        fail("run exclusive: an unreachable store did not report an error")
    }
    if true == brokenRan || true == brokenFnRan.Load() {
        fail("run exclusive: an unreachable store double-ran the work (failed open)")
    }
    pass("run exclusive failed closed on an unreachable store")
}

/* runLeaderGateCheck exercises lock.LeaderGate over a live redis lease locker: exactly one of two competing gates is elected, the follower keeps campaigning, and when the leader shuts down the follower is promoted — the election the outbox relay and singleton workers rely on. */
func runLeaderGateCheck(address string) {
    client := openRedis(address)
    defer client.Close()

    locker := melodyrueidis.NewLocker(client)
    name := fmt.Sprintf("melody:e2e:leader:%d", time.Now().UnixNano())

    firstGate, firstCancel, firstElected := startLeaderGate(locker, name)
    secondGate, secondCancel, secondElected := startLeaderGate(locker, name)

    var leader *lock.LeaderGate
    var follower *lock.LeaderGate
    var followerElected <-chan struct{}
    var leaderCancel context.CancelFunc
    var followerCancel context.CancelFunc

    select {
    case <-firstElected:
        leader, follower = firstGate, secondGate
        leaderCancel, followerCancel = firstCancel, secondCancel
        followerElected = secondElected
    case <-secondElected:
        leader, follower = secondGate, firstGate
        leaderCancel, followerCancel = secondCancel, firstCancel
        followerElected = firstElected
    case <-time.After(10 * time.Second):
        firstCancel()
        secondCancel()
        fail("leader gate: neither gate was elected within 10s")
        return
    }

    defer followerCancel()

    /* give the follower a moment to lose its own campaign, then assert exactly one leader */
    time.Sleep(500 * time.Millisecond)

    if false == leader.IsLeader() {
        fail("leader gate: the elected gate does not report itself as leader")
    }
    if true == follower.IsLeader() {
        fail("leader gate: both gates believe they are the leader")
    }
    pass("leader gate elected exactly one leader of two contenders")

    /* the leader shuts down and releases; the follower must be promoted */
    leaderCancel()

    select {
    case <-followerElected:
        pass("leader gate promoted the follower after the leader shut down")
    case <-time.After(10 * time.Second):
        fail("leader gate: the follower was not promoted within 10s of the leader's shutdown")
    }

    if false == follower.IsLeader() {
        fail("leader gate: the promoted follower does not report itself as leader")
    }

    followerCancel()

    /* a demoted gate must stop claiming leadership once Run has returned */
    time.Sleep(500 * time.Millisecond)
    if true == follower.IsLeader() {
        fail("leader gate: a gate still claims leadership after its runtime was cancelled")
    }
    pass("leader gate dropped leadership on shutdown")
}

/* startLeaderGate runs a gate on its own cancellable runtime and reports election through a channel closed by OnElected, so a caller can wait on the outcome rather than poll IsLeader. */
func startLeaderGate(
    locker lockcontract.Locker,
    name string,
) (*lock.LeaderGate, context.CancelFunc, <-chan struct{}) {
    elected := make(chan struct{})
    var electedOnce sync.Once

    gate := lock.NewLeaderGateWithOptions(
        locker,
        name,
        4*time.Second,
        lock.LeaderGateOptions{
            RetryInterval:   200 * time.Millisecond,
            RefreshInterval: 1 * time.Second,
            OnElected: func(runtimecontract.Runtime) {
                electedOnce.Do(func() { close(elected) })
            },
        },
    )

    gateContext, cancel := context.WithCancel(context.Background())
    serviceContainer := container.NewContainer()
    gateRuntime := runtime.New(gateContext, serviceContainer.NewScope(), serviceContainer)

    go func() {
        if runErr := gate.Run(gateRuntime); nil != runErr {
            fail("leader gate: Run returned an error: %v", runErr)
        }
    }()

    return gate, cancel, elected
}

func openRedis(address string) rueidis.Client {
    provider := melodyrueidis.NewProvider()

    client, openErr := provider.Open(melodyrueidis.NewConnectionParameters(address, "", ""))
    if nil != openErr {
        fail("lock: open redis: %v", openErr)
    }

    return client
}
