package lock

import (
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
