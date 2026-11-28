package lock

import (
    "context"
    "fmt"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    clicontract "github.com/precision-soft/melody/v4/cli/contract"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    lockcontract "github.com/precision-soft/melody/v4/lock/contract"
)

type recordingCommand struct {
    name   string
    flags  []clicontract.Flag
    calls  int
    result error
}

func (instance *recordingCommand) Name() string {
    return instance.name
}

func (instance *recordingCommand) Description() string {
    return "records invocations"
}

func (instance *recordingCommand) Flags() []clicontract.Flag {
    return instance.flags
}

func (instance *recordingCommand) Run(
    runtimeInstance containercontract.Runtime,
    commandContext *clicontract.CommandContext,
) error {
    instance.calls++
    return instance.result
}

func TestExclusiveCommand_DelegatesMetadata(t *testing.T) {
    inner := &recordingCommand{
        name:  "outbox:dispatch",
        flags: []clicontract.Flag{&clicontract.StringFlag{Name: "limit"}},
    }

    command := NewExclusiveCommand(inner, NewInMemoryLocker(InMemoryLockerConfig{}), ExclusiveCommandConfig{Ttl: bound.Of(time.Minute)})

    if "outbox:dispatch" != command.Name() {
        t.Fatalf("expected the name to delegate, got: %s", command.Name())
    }

    if "records invocations" != command.Description() {
        t.Fatalf("expected the description to delegate, got: %s", command.Description())
    }

    if 1 != len(command.Flags()) {
        t.Fatalf("expected the flags to delegate, got: %d", len(command.Flags()))
    }
}

func TestExclusiveCommand_RunsWhenFree(t *testing.T) {
    inner := &recordingCommand{name: "outbox:dispatch"}
    command := NewExclusiveCommand(inner, NewInMemoryLocker(InMemoryLockerConfig{}), ExclusiveCommandConfig{Ttl: bound.Of(time.Minute)})

    if runErr := command.Run(testRuntime(), nil); nil != runErr {
        t.Fatalf("unexpected run error: %v", runErr)
    }

    if 1 != inner.calls {
        t.Fatalf("expected exactly one inner run, got: %d", inner.calls)
    }
}

func TestExclusiveCommand_SkipsQuietlyWhenHeld(t *testing.T) {
    locker := NewInMemoryLocker(InMemoryLockerConfig{})
    runtimeInstance := testRuntime()

    holder := mustCreateLock(locker.CreateLock("melody:command:outbox:dispatch", lockcontract.CreateLockOptions{Ttl: bound.Of(time.Minute)}))
    acquired, _ := holder.Acquire(runtimeInstance)
    if false == acquired {
        t.Fatalf("expected external holder to acquire the default lock name")
    }

    inner := &recordingCommand{name: "outbox:dispatch"}
    command := NewExclusiveCommand(inner, locker, ExclusiveCommandConfig{Ttl: bound.Of(time.Minute)})

    if runErr := command.Run(runtimeInstance, nil); nil != runErr {
        t.Fatalf("expected a skipped run to exit clean, got: %v", runErr)
    }

    if 0 != inner.calls {
        t.Fatalf("expected the inner command to be skipped, got %d calls", inner.calls)
    }
}

func TestExclusiveCommand_PropagatesInnerError(t *testing.T) {
    expected := exception.NewError("dispatch failed", nil, nil)
    inner := &recordingCommand{name: "outbox:dispatch", result: expected}
    command := NewExclusiveCommand(inner, NewInMemoryLocker(InMemoryLockerConfig{}), ExclusiveCommandConfig{Ttl: bound.Of(time.Minute)})

    if runErr := command.Run(testRuntime(), nil); expected != runErr {
        t.Fatalf("expected the inner error to propagate, got: %v", runErr)
    }
}

func TestExclusiveCommand_CustomLockNameSharesTheLock(t *testing.T) {
    locker := NewInMemoryLocker(InMemoryLockerConfig{})
    runtimeInstance := testRuntime()

    holder := mustCreateLock(locker.CreateLock("custom:lease", lockcontract.CreateLockOptions{Ttl: bound.Of(time.Minute)}))
    acquired, _ := holder.Acquire(runtimeInstance)
    if false == acquired {
        t.Fatalf("expected external holder to acquire the custom lock name")
    }

    inner := &recordingCommand{name: "reconcile:run"}
    command := NewExclusiveCommand(inner, locker, ExclusiveCommandConfig{LockName: "custom:lease", Ttl: bound.Of(time.Minute)})

    if runErr := command.Run(runtimeInstance, nil); nil != runErr {
        t.Fatalf("expected a skipped run to exit clean, got: %v", runErr)
    }

    if 0 != inner.calls {
        t.Fatalf("expected the inner command to be skipped on the custom lock, got %d calls", inner.calls)
    }
}

func TestExclusiveCommand_TheDefaultLockNameCarriesTheNamespace(t *testing.T) {
    runtimeInstance := runtimeWithNamespace(t, "shop")

    for _, testCase := range []struct {
        name        string
        lockName    string
        heldName    string
        expectedRun int
    }{
        {name: "the namespace opens the default name", lockName: "", heldName: "shop:command:app:x", expectedRun: 0},
        {name: "the default namespace's name is another lock", lockName: "", heldName: "melody:command:app:x", expectedRun: 1},
        {name: "an explicit name is verbatim", lockName: "custom:lease", heldName: "custom:lease", expectedRun: 0},
    } {
        t.Run(testCase.name, func(t *testing.T) {
            locker := NewInMemoryLocker(InMemoryLockerConfig{})

            holder := mustCreateLock(locker.CreateLock(testCase.heldName, lockcontract.CreateLockOptions{Ttl: bound.Of(time.Minute)}))
            if acquired, _ := holder.Acquire(runtimeInstance); false == acquired {
                t.Fatalf("expected the external holder to acquire %q", testCase.heldName)
            }

            inner := &recordingCommand{name: "app:x"}
            command := NewExclusiveCommand(inner, locker, ExclusiveCommandConfig{LockName: testCase.lockName, Ttl: bound.Of(time.Minute)})

            if runErr := command.Run(runtimeInstance, nil); nil != runErr {
                t.Fatalf("unexpected run error: %v", runErr)
            }

            if testCase.expectedRun != inner.calls {
                t.Fatalf("expected %d runs beside a holder of %q, got %d", testCase.expectedRun, testCase.heldName, inner.calls)
            }
        })
    }
}

func TestExclusiveCommand_ShutdownSkipNamesTheShutdownNotAPhantomPeer(t *testing.T) {
    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    runtimeInstance, logger := runtimeWithRecordingLogger(cancelledContext)

    inner := &recordingCommand{name: "outbox:dispatch"}
    command := NewExclusiveCommand(inner, &contextCancelledAcquireLocker{}, ExclusiveCommandConfig{Ttl: bound.Of(time.Minute)})

    if runErr := command.Run(runtimeInstance, nil); nil != runErr {
        t.Fatalf("expected a shutdown-suppressed skip to exit clean, got: %v", runErr)
    }

    if 0 != inner.calls {
        t.Fatalf("expected the inner command to be skipped, got %d calls", inner.calls)
    }

    if false == logger.hasMessageContaining("shutdown was requested before the lock was acquired") {
        t.Fatalf("expected the skip record to name the shutdown")
    }

    if true == logger.hasMessageContaining("already in progress on another instance") {
        t.Fatalf("expected the shutdown skip not to blame a phantom peer instance")
    }
}

func TestExclusiveCommand_ContentionSkipNamesThePeerInstance(t *testing.T) {
    locker := NewInMemoryLocker(InMemoryLockerConfig{})
    runtimeInstance, logger := runtimeWithRecordingLogger(context.Background())

    holder := mustCreateLock(locker.CreateLock("melody:command:outbox:dispatch", lockcontract.CreateLockOptions{Ttl: bound.Of(time.Minute)}))
    if acquired, _ := holder.Acquire(runtimeInstance); false == acquired {
        t.Fatalf("expected the external holder to acquire the lock")
    }

    inner := &recordingCommand{name: "outbox:dispatch"}
    command := NewExclusiveCommand(inner, locker, ExclusiveCommandConfig{Ttl: bound.Of(time.Minute)})

    if runErr := command.Run(runtimeInstance, nil); nil != runErr {
        t.Fatalf("expected a contention skip to exit clean, got: %v", runErr)
    }

    if false == logger.hasMessageContaining("already in progress on another instance") {
        t.Fatalf("expected the contention skip to name the peer instance")
    }
}

func TestExclusiveCommand_HandsItsResolvedTtlToTheLocker(t *testing.T) {
    for _, testCase := range []struct {
        name   string
        ttl    bound.Duration
        wanted time.Duration
    }{
        {"left out, the 30s default", bound.Duration{}, 30 * time.Second},
        {"set", bound.Of(2 * time.Minute), 2 * time.Minute},
    } {
        t.Run(testCase.name, func(t *testing.T) {
            locker := &ttlRecordingLocker{inner: NewInMemoryLocker(InMemoryLockerConfig{})}
            inner := &recordingCommand{name: "report:build"}

            if runErr := NewExclusiveCommand(inner, locker, ExclusiveCommandConfig{Ttl: testCase.ttl}).Run(testRuntime(), nil); nil != runErr {
                t.Fatalf("unexpected run error: %v", runErr)
            }

            if ttl, isSet := locker.firstTtl(t).Value(); false == isSet || testCase.wanted != ttl {
                t.Fatalf("expected the locker to be handed %s, got %s", testCase.wanted, locker.firstTtl(t))
            }
        })
    }
}

func TestNewExclusiveCommand_RefusesAConfigurationTheRulesRefuseNamingTheField(t *testing.T) {
    for _, testCase := range []struct {
        config ExclusiveCommandConfig
        field  string
    }{
        {ExclusiveCommandConfig{Ttl: bound.Of(time.Microsecond)}, "ExclusiveCommandConfig.Ttl"},
    } {
        var recovered any
        func() {
            defer func() { recovered = recover() }()

            NewExclusiveCommand(&recordingCommand{name: "report:build"}, NewInMemoryLocker(InMemoryLockerConfig{}), testCase.config)
        }()

        refusal, isMelodyErr := recovered.(*exception.Error)
        if false == isMelodyErr || testCase.field != refusal.Context()["field"] {
            t.Fatalf("expected a panic naming %s, got %#v", testCase.field, recovered)
        }
    }
}

func TestExclusiveCommand_AnUnlimitedTtlHandsTheLockerASessionLock(t *testing.T) {
    locker := &ttlRecordingLocker{inner: &sessionStyleLocker{inner: NewInMemoryLocker(InMemoryLockerConfig{})}}
    inner := &recordingCommand{name: "report:build"}

    if runErr := NewExclusiveCommand(inner, locker, ExclusiveCommandConfig{Ttl: bound.UnlimitedDuration()}).Run(testRuntime(), nil); nil != runErr || 1 != inner.calls {
        t.Fatalf("expected the session-style command to run once, got %d calls and %v", inner.calls, runErr)
    }

    if false == locker.firstTtl(t).IsUnlimited() {
        t.Fatalf("expected the locker to be handed an unlimited ttl, got %s", locker.firstTtl(t))
    }
}

func TestNewExclusiveCommand_RefusesAStopRenewalLimitBelowTheRefreshInterval(t *testing.T) {
    defer func() {
        recovered := recover()
        if false == strings.Contains(fmt.Sprint(recovered), "stop renewal limit") {
            t.Fatalf("expected the limit refused at construction, got %v", recovered)
        }
    }()

    NewExclusiveCommand(&recordingCommand{name: "job"}, NewInMemoryLocker(InMemoryLockerConfig{}), ExclusiveCommandConfig{Ttl: bound.Of(time.Minute), StopRenewalLimit: bound.Of(time.Second)})
}

func TestNewExclusiveCommand_HandsItsStopRenewalLimitToTheRun(t *testing.T) {
    command := NewExclusiveCommand(&recordingCommand{name: "job"}, NewInMemoryLocker(InMemoryLockerConfig{}), ExclusiveCommandConfig{Ttl: bound.Of(time.Minute), StopRenewalLimit: bound.Of(5 * time.Minute)})

    if bound.Of(5*time.Minute) != command.exclusiveOptions.StopRenewalLimit {
        t.Fatalf("expected the limit handed to the run, got %v", command.exclusiveOptions.StopRenewalLimit)
    }
}

func TestNewExclusiveCommand_ALockerAnsweringNoLockIsRefusedAtConstruction(t *testing.T) {
    var recovered any
    func() {
        defer func() { recovered = recover() }()

        NewExclusiveCommand(&recordingCommand{name: "report:build"}, &noLockLocker{}, ExclusiveCommandConfig{})
    }()

    refusal, isMelodyErr := recovered.(*exception.Error)
    if false == isMelodyErr || false == namesNoLock(refusal) {
        t.Fatalf("expected a panic carrying the no-lock refusal, got %#v", recovered)
    }
}

func TestNewExclusiveCommand_HandsTheProbeIntervalOnAndRefusesItBesideALeaseTtl(t *testing.T) {
    command := NewExclusiveCommand(&recordingCommand{name: "probe:session"}, &sessionStyleLocker{inner: NewInMemoryLocker(InMemoryLockerConfig{})}, ExclusiveCommandConfig{Ttl: bound.UnlimitedDuration(), ProbeInterval: bound.Of(5 * time.Second)})
    if bound.Of(5*time.Second) != command.exclusiveOptions.ProbeInterval {
        t.Fatalf("expected the probe interval handed to the run, got %s", command.exclusiveOptions.ProbeInterval)
    }

    defer func() {
        recoveredErr, isError := recover().(error)
        if false == isError || "ExclusiveCommandConfig.ProbeInterval" != exception.LogContext(recoveredErr, exception.LogContextOptions{})["field"] {
            t.Fatalf("expected the refusal naming ExclusiveCommandConfig.ProbeInterval, got %v", recoveredErr)
        }
    }()

    NewExclusiveCommand(&recordingCommand{name: "probe:lease"}, NewInMemoryLocker(InMemoryLockerConfig{}), ExclusiveCommandConfig{Ttl: bound.Of(time.Minute), ProbeInterval: bound.Of(time.Second)})
}
