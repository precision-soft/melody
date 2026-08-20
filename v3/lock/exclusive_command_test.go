package lock

import (
    "context"
    "testing"
    "time"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/clock"
    "github.com/precision-soft/melody/v3/exception"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
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
    runtimeInstance runtimecontract.Runtime,
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

    command := NewExclusiveCommand(inner, NewInMemoryLocker(clock.NewSystemClock()), time.Minute)

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
    command := NewExclusiveCommand(inner, NewInMemoryLocker(clock.NewSystemClock()), time.Minute)

    if runErr := command.Run(testRuntime(), nil); nil != runErr {
        t.Fatalf("unexpected run error: %v", runErr)
    }

    if 1 != inner.calls {
        t.Fatalf("expected exactly one inner run, got: %d", inner.calls)
    }
}

func TestExclusiveCommand_SkipsQuietlyWhenHeld(t *testing.T) {
    locker := NewInMemoryLocker(clock.NewSystemClock())
    runtimeInstance := testRuntime()

    holder := locker.CreateLock(exclusiveCommandLockNamePrefix+"outbox:dispatch", time.Minute)
    acquired, _ := holder.Acquire(runtimeInstance)
    if false == acquired {
        t.Fatalf("expected external holder to acquire the default lock name")
    }

    inner := &recordingCommand{name: "outbox:dispatch"}
    command := NewExclusiveCommand(inner, locker, time.Minute)

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
    command := NewExclusiveCommand(inner, NewInMemoryLocker(clock.NewSystemClock()), time.Minute)

    if runErr := command.Run(testRuntime(), nil); expected != runErr {
        t.Fatalf("expected the inner error to propagate, got: %v", runErr)
    }
}

func TestExclusiveCommand_CustomLockNameSharesTheLock(t *testing.T) {
    locker := NewInMemoryLocker(clock.NewSystemClock())
    runtimeInstance := testRuntime()

    holder := locker.CreateLock("custom:lease", time.Minute)
    acquired, _ := holder.Acquire(runtimeInstance)
    if false == acquired {
        t.Fatalf("expected external holder to acquire the custom lock name")
    }

    inner := &recordingCommand{name: "reconcile:run"}
    command := NewExclusiveCommandWithName(inner, locker, "custom:lease", time.Minute)

    if runErr := command.Run(runtimeInstance, nil); nil != runErr {
        t.Fatalf("expected a skipped run to exit clean, got: %v", runErr)
    }

    if 0 != inner.calls {
        t.Fatalf("expected the inner command to be skipped on the custom lock, got %d calls", inner.calls)
    }
}

func TestExclusiveCommand_ShutdownSkipNamesTheShutdownNotAPhantomPeer(t *testing.T) {
    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    runtimeInstance, logger := runtimeWithRecordingLogger(cancelledContext)

    inner := &recordingCommand{name: "outbox:dispatch"}
    command := NewExclusiveCommand(inner, &contextCancelledAcquireLocker{}, time.Minute)

    if runErr := command.Run(runtimeInstance, nil); nil != runErr {
        t.Fatalf("expected a shutdown-suppressed skip to exit clean, got: %v", runErr)
    }

    if 0 != inner.calls {
        t.Fatalf("expected the inner command to be skipped, got %d calls", inner.calls)
    }

    /* the skip log is the single source of truth for "did not run" under the exit-zero design: a SIGTERM during the acquire must not write that another instance is running the work when none is */
    if false == logger.hasMessageContaining("shutdown was requested before the lock was acquired") {
        t.Fatalf("expected the skip record to name the shutdown")
    }

    if true == logger.hasMessageContaining("already in progress on another instance") {
        t.Fatalf("expected the shutdown skip not to blame a phantom peer instance")
    }
}

func TestExclusiveCommand_ContentionSkipNamesThePeerInstance(t *testing.T) {
    locker := NewInMemoryLocker(clock.NewSystemClock())
    runtimeInstance, logger := runtimeWithRecordingLogger(context.Background())

    holder := locker.CreateLock(exclusiveCommandLockNamePrefix+"outbox:dispatch", time.Minute)
    if acquired, _ := holder.Acquire(runtimeInstance); false == acquired {
        t.Fatalf("expected the external holder to acquire the lock")
    }

    inner := &recordingCommand{name: "outbox:dispatch"}
    command := NewExclusiveCommand(inner, locker, time.Minute)

    if runErr := command.Run(runtimeInstance, nil); nil != runErr {
        t.Fatalf("expected a contention skip to exit clean, got: %v", runErr)
    }

    if false == logger.hasMessageContaining("already in progress on another instance") {
        t.Fatalf("expected the contention skip to name the peer instance")
    }
}
