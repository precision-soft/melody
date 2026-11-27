package cron

import (
    "bytes"
    "context"
    "encoding/json"
    "errors"
    "strings"
    "sync/atomic"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    clicontract "github.com/precision-soft/melody/v4/cli/contract"
    "github.com/precision-soft/melody/v4/cli/output"
    "github.com/precision-soft/melody/v4/clock"
    melodyconfig "github.com/precision-soft/melody/v4/config"
    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/lock"
    lockcontract "github.com/precision-soft/melody/v4/lock/contract"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

var tickDedupMinute = time.Date(2026, time.July, 15, 9, 0, 0, 0, time.UTC)

func newTickDedupRunner(command clicontract.Command, entry EntryConfig, config RunnerConfig) *RunnerCommand {
    config.Commands = []clicontract.Command{command}

    return NewRunnerCommand(NewConfiguration().Schedule(command.Name(), entry), config)
}

func newSharedInMemoryLocker(frozenClock *clock.FrozenClock) *lock.InMemoryLocker {
    return lock.NewInMemoryLocker(lock.InMemoryLockerConfig{Clock: frozenClock})
}

func dispatchMinute(t *testing.T, runner *RunnerCommand, runtimeInstance containercontract.Runtime, at time.Time) (dueReport, error) {
    t.Helper()

    return runner.dispatchDue(runtimeInstance, at, true, true)()
}

func newLoggedRunnerRuntime(ctx context.Context, logger loggingcontract.Logger) containercontract.Runtime {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    serviceContainer.MustRegister(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return logger, nil
        },
        container.RegisterOptions{},
    )

    return runtime.NewRun(ctx, serviceContainer.NewScope())
}

func newLockerRegisteredRuntime(locker lockcontract.Locker, lockerErr error) containercontract.Runtime {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    serviceContainer.MustRegister(
        lock.ServiceLocker,
        func(resolver containercontract.Resolver) (lockcontract.Locker, error) {
            return locker, lockerErr
        },
        container.RegisterOptions{},
    )

    return runtime.NewRun(context.Background(), serviceContainer.NewScope())
}

func TestTickDedup_TwoRunnersSharingOneLockerRunATickOnce(t *testing.T) {
    job := &countingCommand{commandName: "job:every-minute"}
    locker := newSharedInMemoryLocker(clock.NewFrozenClock(tickDedupMinute))

    first := newTickDedupRunner(job, EntryConfig{}, RunnerConfig{TickDedup: TickDedupConfig{Locker: locker}})
    second := newTickDedupRunner(job, EntryConfig{}, RunnerConfig{TickDedup: TickDedupConfig{Locker: locker}})

    firstReport, firstErr := dispatchMinute(t, first, newRunnerTestRuntime(context.Background()), tickDedupMinute)
    secondReport, secondErr := dispatchMinute(t, second, newRunnerTestRuntime(context.Background()), tickDedupMinute)

    if nil != firstErr || nil != secondErr {
        t.Fatalf("expected no error, got %v and %v", firstErr, secondErr)
    }

    if 1 != job.runCount.Load() {
        t.Fatalf("expected the tick to run once across both runners, ran %d", job.runCount.Load())
    }

    if 1 != len(firstReport.Ran) || 0 != len(firstReport.Skipped) {
        t.Fatalf("expected the first runner to run the tick, got %+v", firstReport)
    }

    if 0 != len(secondReport.Ran) || 1 != len(secondReport.Skipped) {
        t.Fatalf("expected the second runner to skip the tick, got %+v", secondReport)
    }

    skip := secondReport.Skipped[0]
    if "job:every-minute" != skip.Command || "* * * * *" != skip.Schedule || TickSkipReasonHeldByAnotherInstance != skip.Reason {
        t.Fatalf("expected the skip row to name the command, the schedule and the reason, got %+v", skip)
    }
}

func TestTickDedup_SkewedReplicaSkipsTheTickTheFirstOneRan(t *testing.T) {
    testCases := []struct {
        name        string
        skew        time.Duration
        secondFirst bool
    }{
        {name: "three hundred milliseconds", skew: 300 * time.Millisecond},
        {name: "just inside the window", skew: 5*time.Minute - time.Second},
        {name: "the second clock runs fast", skew: 300 * time.Millisecond, secondFirst: true},
    }

    for _, testCase := range testCases {
        t.Run(testCase.name, func(t *testing.T) {
            job := &countingCommand{commandName: "job:every-minute"}
            frozenClock := clock.NewFrozenClock(tickDedupMinute)
            locker := &recordingTickLocker{inner: newSharedInMemoryLocker(frozenClock)}

            first := newTickDedupRunner(job, EntryConfig{}, RunnerConfig{TickDedup: TickDedupConfig{Locker: locker}})
            second := newTickDedupRunner(job, EntryConfig{}, RunnerConfig{TickDedup: TickDedupConfig{Locker: locker}})

            early, late := first, second
            if true == testCase.secondFirst {
                early, late = second, first
            }

            earlyReport, _ := dispatchMinute(t, early, newRunnerTestRuntime(context.Background()), tickDedupMinute)
            frozenClock.Advance(testCase.skew)
            lateReport, _ := dispatchMinute(t, late, newRunnerTestRuntime(context.Background()), tickDedupMinute)

            if 1 != job.runCount.Load() || 1 != len(earlyReport.Ran) || 1 != len(lateReport.Skipped) {
                t.Fatalf("expected one run by the earlier runner and one skip by the later, ran %d", job.runCount.Load())
            }

            names := locker.recordedNames()
            if 2 != len(names) || names[0] != names[1] {
                t.Fatalf("expected both runners to ask for one key, got %v", names)
            }
        })
    }
}

func TestTickDedup_DisabledRunsTheTickOnBothRunners(t *testing.T) {
    job := &countingCommand{commandName: "job:every-minute"}
    locker := &recordingTickLocker{inner: newSharedInMemoryLocker(clock.NewFrozenClock(tickDedupMinute))}

    for range 2 {
        runner := newTickDedupRunner(job, EntryConfig{}, RunnerConfig{TickDedup: TickDedupConfig{Disabled: true, Locker: locker}})
        _, _ = dispatchMinute(t, runner, newRunnerTestRuntime(context.Background()), tickDedupMinute)
    }

    if 2 != job.runCount.Load() {
        t.Fatalf("expected both runners to run the tick, ran %d", job.runCount.Load())
    }

    if 0 != len(locker.recordedNames()) {
        t.Fatalf("expected no lock to be created, got %v", locker.recordedNames())
    }
}

func TestTickDedup_EntryDisabledRunsThatEntryOnBothRunners(t *testing.T) {
    perReplica := &countingCommand{commandName: "job:per-replica"}
    shared := &countingCommand{commandName: "job:shared"}
    locker := &recordingTickLocker{inner: newSharedInMemoryLocker(clock.NewFrozenClock(tickDedupMinute))}

    for range 2 {
        configuration := NewConfiguration().
            Schedule("job:per-replica", EntryConfig{TickDedup: EntryTickDedupConfig{Disabled: true}}).
            Schedule("job:shared", EntryConfig{})

        runner := NewRunnerCommand(configuration, RunnerConfig{
            Commands:  []clicontract.Command{perReplica, shared},
            TickDedup: TickDedupConfig{Locker: locker},
        })
        _, _ = dispatchMinute(t, runner, newRunnerTestRuntime(context.Background()), tickDedupMinute)
    }

    if 2 != perReplica.runCount.Load() || 1 != shared.runCount.Load() {
        t.Fatalf("expected the per-replica entry twice and the shared one once, got %d and %d", perReplica.runCount.Load(), shared.runCount.Load())
    }

    for _, name := range locker.recordedNames() {
        if true == strings.Contains(name, "job:per-replica") {
            t.Fatalf("expected no key for the disabled entry, got %s", name)
        }
    }
}

func TestTickDedup_LockerFailureSkipsTheTickAndFailsTheRun(t *testing.T) {
    lockerErr := errors.New("the store is down")
    job := &countingCommand{commandName: "job:every-minute"}
    captured := &capturingRunnerLogger{}

    runner := newTickDedupRunner(job, EntryConfig{}, RunnerConfig{TickDedup: TickDedupConfig{Locker: &failingTickLocker{mode: failingTickLockerAnswersAnError, err: lockerErr}}})

    report, runErr := dispatchMinute(t, runner, newLoggedRunnerRuntime(context.Background(), captured), tickDedupMinute)

    if 0 != job.runCount.Load() {
        t.Fatalf("expected the tick not to run, ran %d", job.runCount.Load())
    }

    if 1 != len(report.Ran) || false == report.Ran[0].Failed || 0 != len(report.Skipped) {
        t.Fatalf("expected one failed row, got %+v", report)
    }

    if false == errors.Is(runErr, ErrTickDedupUnavailable) || false == errors.Is(runErr, lockerErr) {
        t.Fatalf("expected the failure to carry the sentinel and the cause, got %v", runErr)
    }

    record, found := captured.recordByContextValue("commandName", "job:every-minute")
    if false == found || false == strings.HasPrefix(record.context["tickDedupKey"].(string), "melody:cron:tick:job:every-minute:") {
        t.Fatalf("expected the error record to name the key, got %v", record.context)
    }
}

func TestTickDedup_LockerFailureRunsTheTickUnderTheRunMode(t *testing.T) {
    testCases := []struct {
        name   string
        runner TickDedupFailureMode
        entry  TickDedupFailureMode
    }{
        {name: "on the runner", runner: TickDedupFailureRun},
        {name: "on the entry only", entry: TickDedupFailureRun},
    }

    for _, testCase := range testCases {
        t.Run(testCase.name, func(t *testing.T) {
            job := &countingCommand{commandName: "job:every-minute"}
            captured := &capturingRunnerLogger{}
            locker := &failingTickLocker{mode: failingTickLockerAnswersAnError, err: errors.New("the store is down")}

            runner := newTickDedupRunner(
                job,
                EntryConfig{TickDedup: EntryTickDedupConfig{FailureMode: testCase.entry}},
                RunnerConfig{TickDedup: TickDedupConfig{Locker: locker, FailureMode: testCase.runner}},
            )

            _, runErr := dispatchMinute(t, runner, newLoggedRunnerRuntime(context.Background(), captured), tickDedupMinute)

            if nil != runErr || 1 != job.runCount.Load() {
                t.Fatalf("expected the tick to run with no error, ran %d, error %v", job.runCount.Load(), runErr)
            }

            record, found := captured.recordByMessageProbe("cron runner could not reach the tick dedup locker and runs the tick without its marker")
            if false == found || nil == record.context["tickDedupKey"] {
                t.Fatalf("expected one warning naming the key, got %v", record.context)
            }
        })
    }
}

func TestTickDedup_ALoggerPanickingAtASkippedTickIsTheEntrysFailedRow(t *testing.T) {
    job := &countingCommand{commandName: "job:every-minute"}
    locker := newSharedInMemoryLocker(clock.NewFrozenClock(tickDedupMinute))

    first := newTickDedupRunner(job, EntryConfig{}, RunnerConfig{TickDedup: TickDedupConfig{Locker: locker}})
    second := newTickDedupRunner(job, EntryConfig{}, RunnerConfig{TickDedup: TickDedupConfig{Locker: locker}})

    if _, firstErr := dispatchMinute(t, first, newRunnerTestRuntime(context.Background()), tickDedupMinute); nil != firstErr {
        t.Fatalf("first runner: %v", firstErr)
    }

    report, runErr := dispatchMinute(t, second, newLoggedRunnerRuntime(context.Background(), &panickingLogger{}), tickDedupMinute)
    if nil == runErr {
        t.Fatalf("expected the minute to fail")
    }

    if 1 != job.runCount.Load() {
        t.Fatalf("expected the skipped tick not to run, ran %d", job.runCount.Load())
    }

    if 1 != len(report.Ran) || false == report.Ran[0].Failed || false == strings.Contains(report.Ran[0].Error, "panicked gating a scheduled tick") {
        t.Fatalf("expected the panic filed as the entry's failed row, got %+v", report)
    }
}

func TestTickDedup_ALoggerPanickingAtTheRunModeWarningStillRunsTheTick(t *testing.T) {
    job := &countingCommand{commandName: "job:every-minute"}
    locker := &failingTickLocker{mode: failingTickLockerAnswersAnError, err: errors.New("the store is down")}

    runner := newTickDedupRunner(job, EntryConfig{}, RunnerConfig{TickDedup: TickDedupConfig{Locker: locker, FailureMode: TickDedupFailureRun}})

    report, _ := dispatchMinute(t, runner, newLoggedRunnerRuntime(context.Background(), &panickingLogger{}), tickDedupMinute)
    if 1 != job.runCount.Load() {
        t.Fatalf("expected the tick to run once though the warning's logger panicked, ran %d", job.runCount.Load())
    }

    if 1 != len(report.Ran) || true == report.Ran[0].Failed {
        t.Fatalf("expected the entry's run row, not a failed one, got %+v", report)
    }
}

func TestTickDedup_AcquireEndedByTheCallTimeoutIsALockerFailure(t *testing.T) {
    job := &countingCommand{commandName: "job:every-minute"}
    runner := newTickDedupRunner(job, EntryConfig{}, RunnerConfig{TickDedup: TickDedupConfig{
        Locker:      &failingTickLocker{mode: failingTickLockerBlocks},
        CallTimeout: bound.Of(20 * time.Millisecond),
    }})

    finished := make(chan error, 1)
    go func() {
        _, runErr := dispatchMinute(t, runner, newRunnerTestRuntime(context.Background()), tickDedupMinute)
        finished <- runErr
    }()

    select {
    case runErr := <-finished:
        if false == errors.Is(runErr, ErrTickDedupUnavailable) || false == errors.Is(runErr, context.DeadlineExceeded) {
            t.Fatalf("expected a locker failure ended by the call timeout, got %v", runErr)
        }
    case <-time.After(2 * time.Second):
        t.Fatal("expected the call timeout to end the acquire")
    }

    if 0 != job.runCount.Load() {
        t.Fatalf("expected the tick not to run, ran %d", job.runCount.Load())
    }
}

func TestTickDedup_LockerPanicIsALockerFailure(t *testing.T) {
    job := &countingCommand{commandName: "job:deduped"}
    other := &countingCommand{commandName: "job:other"}

    configuration := NewConfiguration().
        Schedule("job:deduped", EntryConfig{}).
        Schedule("job:other", EntryConfig{TickDedup: EntryTickDedupConfig{Disabled: true}})

    runner := NewRunnerCommand(configuration, RunnerConfig{
        Commands:  []clicontract.Command{job, other},
        TickDedup: TickDedupConfig{Locker: &failingTickLocker{mode: failingTickLockerPanics}},
    })

    _, runErr := dispatchMinute(t, runner, newRunnerTestRuntime(context.Background()), tickDedupMinute)

    if false == errors.Is(runErr, ErrTickDedupUnavailable) {
        t.Fatalf("expected the panic to be a locker failure, got %v", runErr)
    }

    if 0 != job.runCount.Load() || 1 != other.runCount.Load() {
        t.Fatalf("expected only the other entry to run, got %d and %d", job.runCount.Load(), other.runCount.Load())
    }
}

func TestTickDedup_AcquireFailingUnderAShutdownIsACancellation(t *testing.T) {
    job := &countingCommand{commandName: "job:every-minute"}
    runner := newTickDedupRunner(job, EntryConfig{}, RunnerConfig{TickDedup: TickDedupConfig{Locker: &failingTickLocker{mode: failingTickLockerBlocks}}})

    ctx, cancel := context.WithCancel(context.Background())
    cancel()

    report, runErr := dispatchMinute(t, runner, newRunnerTestRuntime(ctx), tickDedupMinute)

    if nil != runErr {
        t.Fatalf("expected no error under a shutdown, got %v", runErr)
    }

    if 1 != len(report.Ran) || false == report.Ran[0].Cancelled || true == report.Ran[0].Failed {
        t.Fatalf("expected one cancelled row, got %+v", report.Ran)
    }
}

func TestTickDedup_HoldExpiryLetsTheNextTickAndTheSameKeyRun(t *testing.T) {
    job := &countingCommand{commandName: "job:every-minute"}
    frozenClock := clock.NewFrozenClock(tickDedupMinute)
    locker := newSharedInMemoryLocker(frozenClock)

    first := newTickDedupRunner(job, EntryConfig{}, RunnerConfig{TickDedup: TickDedupConfig{Locker: locker}})
    second := newTickDedupRunner(job, EntryConfig{}, RunnerConfig{TickDedup: TickDedupConfig{Locker: locker}})

    _, _ = dispatchMinute(t, first, newRunnerTestRuntime(context.Background()), tickDedupMinute)

    nextReport, _ := dispatchMinute(t, second, newRunnerTestRuntime(context.Background()), tickDedupMinute.Add(time.Minute))
    if 1 != len(nextReport.Ran) {
        t.Fatalf("expected a held marker not to block the next tick, got %+v", nextReport)
    }

    frozenClock.Advance(5*time.Minute - time.Second)
    heldReport, _ := dispatchMinute(t, second, newRunnerTestRuntime(context.Background()), tickDedupMinute)
    if 1 != len(heldReport.Skipped) {
        t.Fatalf("expected the tick to stay held inside its hold, got %+v", heldReport)
    }

    frozenClock.Advance(2 * time.Second)
    lapsedReport, _ := dispatchMinute(t, second, newRunnerTestRuntime(context.Background()), tickDedupMinute)
    if 1 != len(lapsedReport.Ran) {
        t.Fatalf("expected the tick to run once its marker lapsed, got %+v", lapsedReport)
    }
}

func TestTickDedup_MarkerIsNotReleasedWhenTheCommandReturns(t *testing.T) {
    job := &countingCommand{commandName: "job:every-minute"}
    locker := &recordingTickLocker{inner: newSharedInMemoryLocker(clock.NewFrozenClock(tickDedupMinute))}

    runner := newTickDedupRunner(job, EntryConfig{}, RunnerConfig{TickDedup: TickDedupConfig{Locker: locker}})
    runtimeInstance := newRunnerTestRuntime(context.Background())

    _, _ = dispatchMinute(t, runner, runtimeInstance, tickDedupMinute)
    runner.sweepWonTicks(runtimeInstance, tickDedupMinute)

    if 0 != locker.releases.Load() {
        t.Fatalf("expected no release before the hold, got %d", locker.releases.Load())
    }

    runner.elapsedSince = func(time.Time) time.Duration {
        return 5 * time.Minute
    }

    runner.sweepWonTicks(runtimeInstance, tickDedupMinute)
    runner.sweepWonTicks(runtimeInstance, tickDedupMinute)

    if 1 != locker.releases.Load() {
        t.Fatalf("expected one release at the hold, got %d", locker.releases.Load())
    }

    stopping := newTickDedupRunner(job, EntryConfig{}, RunnerConfig{TickDedup: TickDedupConfig{Locker: locker}})
    _, _ = dispatchMinute(t, stopping, runtimeInstance, tickDedupMinute.Add(time.Minute))
    stopping.elapsedSince = runner.elapsedSince

    ctx, cancel := context.WithCancel(context.Background())
    cancel()
    stopping.sweepWonTicks(newRunnerTestRuntime(ctx), tickDedupMinute)

    if 1 != locker.releases.Load() {
        t.Fatalf("expected a stopping runner to release nothing, got %d", locker.releases.Load())
    }
}

func TestTickDedup_WithoutALockerDispatchesAsBefore(t *testing.T) {
    job := &countingCommand{commandName: "job:every-minute"}

    for range 2 {
        runner := newTickDedupRunner(job, EntryConfig{}, RunnerConfig{})
        _, _ = dispatchMinute(t, runner, newRunnerTestRuntime(context.Background()), tickDedupMinute)
    }

    if 2 != job.runCount.Load() {
        t.Fatalf("expected each runner to run the tick, ran %d", job.runCount.Load())
    }

    const warning = "cron runner has no tick dedup locker: on several replicas every tick runs once per replica; name RunnerConfig.TickDedup.Locker, register service.lock.locker, or set RunnerConfig.TickDedup.Disabled"

    testCases := []struct {
        name     string
        config   RunnerConfig
        expected bool
    }{
        {name: "no locker", config: RunnerConfig{}, expected: true},
        {name: "disabled", config: RunnerConfig{TickDedup: TickDedupConfig{Disabled: true}}, expected: false},
    }

    for _, testCase := range testCases {
        t.Run(testCase.name, func(t *testing.T) {
            captured := &capturingRunnerLogger{}
            runtimeInstance := newLoggedRunnerRuntime(context.Background(), captured)
            runner := newTickDedupRunner(job, EntryConfig{}, testCase.config)

            runner.warnWithoutTickDedupLocker(runtimeInstance, runner.resolveTickDedupRun(runtimeInstance))

            if _, found := captured.recordByMessageProbe(warning); testCase.expected != found {
                t.Fatalf("expected the warning %v, got %v", testCase.expected, found)
            }
        })
    }
}

func TestTickDedup_TheRegisteredLockerIsFoundThroughTheRunsScope(t *testing.T) {
    job := &countingCommand{commandName: "job:every-minute"}
    locker := newSharedInMemoryLocker(clock.NewFrozenClock(tickDedupMinute))

    for range 2 {
        runner := newTickDedupRunner(job, EntryConfig{}, RunnerConfig{})
        _, _ = dispatchMinute(t, runner, newLockerRegisteredRuntime(locker, nil), tickDedupMinute)
    }

    if 1 != job.runCount.Load() {
        t.Fatalf("expected the registered locker to deduplicate the tick, ran %d", job.runCount.Load())
    }

    providerErr := errors.New("the locker provider failed")
    runner := newTickDedupRunner(job, EntryConfig{}, RunnerConfig{})
    _, runErr := dispatchMinute(t, runner, newLockerRegisteredRuntime(nil, providerErr), tickDedupMinute.Add(time.Minute))

    if false == errors.Is(runErr, ErrTickDedupUnavailable) || 1 != job.runCount.Load() {
        t.Fatalf("expected a failing provider to be a locker failure, got %v", runErr)
    }
}

func TestTickDedup_ACreateLockErrorIsALockerFailure(t *testing.T) {
    createErr := errors.New("the locker refused the lock")

    for _, mode := range []TickDedupFailureMode{"", TickDedupFailureRun} {
        job := &countingCommand{commandName: "job:every-minute"}
        runner := newTickDedupRunner(job, EntryConfig{}, RunnerConfig{TickDedup: TickDedupConfig{
            Locker:      &failingTickLocker{mode: failingTickLockerRefusesCreateLock, err: createErr},
            FailureMode: mode,
        }})

        _, runErr := dispatchMinute(t, runner, newRunnerTestRuntime(context.Background()), tickDedupMinute)

        if TickDedupFailureRun == mode {
            if nil != runErr || 1 != job.runCount.Load() {
                t.Fatalf("expected the run mode to run the tick, got %v", runErr)
            }

            continue
        }

        if false == errors.Is(runErr, ErrTickDedupUnavailable) || false == errors.Is(runErr, createErr) || 0 != job.runCount.Load() {
            t.Fatalf("expected the skip mode to fail the tick, got %v", runErr)
        }
    }
}

func TestTickDedup_HoldFollowsTheEntryClass(t *testing.T) {
    wildcard := &countingCommand{commandName: "job:wildcard"}
    fixed := &countingCommand{commandName: "job:fixed"}
    overridden := &countingCommand{commandName: "job:overridden"}
    locker := &recordingTickLocker{inner: newSharedInMemoryLocker(clock.NewFrozenClock(tickDedupMinute))}

    configuration := NewConfiguration().
        Schedule("job:wildcard", EntryConfig{}).
        Schedule("job:fixed", EntryConfig{Schedule: &Schedule{Minute: "0", Hour: "9"}}).
        Schedule("job:overridden", EntryConfig{TickDedup: EntryTickDedupConfig{Window: bound.Of(10 * time.Second)}})

    runner := NewRunnerCommand(configuration, RunnerConfig{
        Commands:  []clicontract.Command{wildcard, fixed, overridden},
        TickDedup: TickDedupConfig{Locker: locker},
    })

    _, _ = dispatchMinute(t, runner, newRunnerTestRuntime(context.Background()), tickDedupMinute)

    expected := map[string]time.Duration{
        "job:wildcard":   5 * time.Minute,
        "job:fixed":      5*time.Minute + 3*time.Hour,
        "job:overridden": 10 * time.Second,
    }

    names := locker.recordedNames()
    ttls := locker.recordedTtls()
    if 3 != len(names) {
        t.Fatalf("expected three keys, got %v", names)
    }

    for index, name := range names {
        for commandName, hold := range expected {
            if false == strings.Contains(name, ":"+commandName+":") {
                continue
            }

            if bound.Of(hold) != ttls[index] {
                t.Fatalf("expected %s to be held %v, got %v", commandName, hold, ttls[index])
            }
        }
    }
}

func TestTickDedup_IrregularScheduleHoldsOnlyItsOwnTick(t *testing.T) {
    job := &countingCommand{commandName: "job:twice-a-month"}
    locker := &recordingTickLocker{inner: newSharedInMemoryLocker(clock.NewFrozenClock(tickDedupMinute))}
    entry := EntryConfig{Schedule: &Schedule{Minute: "0", Hour: "9", DayOfMonth: "1,15"}}

    first := newTickDedupRunner(job, entry, RunnerConfig{TickDedup: TickDedupConfig{Locker: locker}})
    second := newTickDedupRunner(job, entry, RunnerConfig{TickDedup: TickDedupConfig{Locker: locker}})

    firstOfMonth := time.Date(2026, time.July, 1, 9, 0, 0, 0, time.UTC)
    fifteenth := time.Date(2026, time.July, 15, 9, 0, 0, 0, time.UTC)

    _, _ = dispatchMinute(t, first, newRunnerTestRuntime(context.Background()), firstOfMonth)
    skipped, _ := dispatchMinute(t, second, newRunnerTestRuntime(context.Background()), firstOfMonth)
    ran, _ := dispatchMinute(t, second, newRunnerTestRuntime(context.Background()), fifteenth)

    if 1 != len(skipped.Skipped) || 1 != len(ran.Ran) || 2 != job.runCount.Load() {
        t.Fatalf("expected the 1st skipped and the 15th run, got %+v and %+v", skipped, ran)
    }

    for _, ttl := range locker.recordedTtls() {
        if bound.Of(5*time.Minute+3*time.Hour) != ttl {
            t.Fatalf("expected every tick held the window plus the catch-up horizon, got %v", ttl)
        }
    }
}

func TestTickDedup_CatchUpSkipsAFixedTimeTickAnotherRunnerRan(t *testing.T) {
    ranOnTime := &countingCommand{commandName: "job:on-time"}
    nobodyRan := &countingCommand{commandName: "job:missed"}
    locker := newSharedInMemoryLocker(clock.NewFrozenClock(tickDedupMinute))

    newRunner := func() *RunnerCommand {
        configuration := NewConfiguration().
            Schedule("job:on-time", EntryConfig{Schedule: &Schedule{Minute: "30", Hour: "9"}}).
            Schedule("job:missed", EntryConfig{Schedule: &Schedule{Minute: "15", Hour: "10"}})

        return NewRunnerCommand(configuration, RunnerConfig{
            Commands:  []clicontract.Command{ranOnTime, nobodyRan},
            TickDedup: TickDedupConfig{Locker: locker},
        })
    }

    first := newRunner()
    second := newRunner()

    onTime := time.Date(2026, time.July, 15, 9, 30, 0, 0, time.UTC)
    _, _ = first.dispatchDue(newRunnerTestRuntime(context.Background()), onTime, true, false)()

    evaluations, _, _ := reconcileWallClock(tickDedupMinute, tickDedupMinute.Add(2*time.Hour), 3*time.Hour)
    for _, evaluation := range evaluations {
        _, _ = second.dispatchDue(newRunnerTestRuntime(context.Background()), evaluation.at, evaluation.runFixedTime, evaluation.runWildcard)()
    }

    if 1 != ranOnTime.runCount.Load() || 1 != nobodyRan.runCount.Load() {
        t.Fatalf("expected the catch-up to skip the tick ran on time and run the missed one once, got %d and %d", ranOnTime.runCount.Load(), nobodyRan.runCount.Load())
    }
}

func TestTickDedup_ARunnerNeverRefusesItself(t *testing.T) {
    job := &countingCommand{commandName: "job:every-minute"}
    frozenClock := clock.NewFrozenClock(tickDedupMinute)
    locker := newSharedInMemoryLocker(frozenClock)

    runner := newTickDedupRunner(job, EntryConfig{}, RunnerConfig{TickDedup: TickDedupConfig{Locker: locker}})

    _, _ = dispatchMinute(t, runner, newRunnerTestRuntime(context.Background()), tickDedupMinute)
    frozenClock.TravelTo(tickDedupMinute.Add(-30 * time.Second))
    _, _ = dispatchMinute(t, runner, newRunnerTestRuntime(context.Background()), tickDedupMinute)

    if 2 != job.runCount.Load() {
        t.Fatalf("expected the re-evaluated tick to run again on its winner, ran %d", job.runCount.Load())
    }

    twin := &countingCommand{commandName: "job:twin"}
    configuration := NewConfiguration().
        Schedule("job:twin", EntryConfig{}).
        Schedule("job:twin", EntryConfig{})

    twinLocker := &recordingTickLocker{inner: locker, acquireDelay: 50 * time.Millisecond}
    twins := NewRunnerCommand(configuration, RunnerConfig{Commands: []clicontract.Command{twin}, TickDedup: TickDedupConfig{Locker: twinLocker}})
    twinReport, _ := dispatchMinute(t, twins, newRunnerTestRuntime(context.Background()), tickDedupMinute)

    if 2 != len(twinReport.Ran) || 2 != twin.runCount.Load() || 1 != len(twinLocker.recordedNames()) {
        t.Fatalf("expected both twin entries to run on one claim, got %+v and %v", twinReport, twinLocker.recordedNames())
    }

    other := newTickDedupRunner(job, EntryConfig{}, RunnerConfig{TickDedup: TickDedupConfig{Locker: locker}})
    otherReport, _ := dispatchMinute(t, other, newRunnerTestRuntime(context.Background()), tickDedupMinute)

    if 1 != len(otherReport.Skipped) {
        t.Fatalf("expected another runner to skip the tick, got %+v", otherReport)
    }
}

func TestTickDedup_LedgerRowOutlivesAStepBackLongerThanTheHold(t *testing.T) {
    job := &countingCommand{commandName: "job:every-minute"}
    frozenClock := clock.NewFrozenClock(tickDedupMinute)
    locker := newSharedInMemoryLocker(frozenClock)

    runner := newTickDedupRunner(job, EntryConfig{}, RunnerConfig{TickDedup: TickDedupConfig{Locker: locker}})
    runtimeInstance := newRunnerTestRuntime(context.Background())

    _, _ = dispatchMinute(t, runner, runtimeInstance, tickDedupMinute)

    /* another instance takes the lapsed marker, so only the ledger can let the winner re-run */
    frozenClock.Advance(10 * time.Minute)
    other := newTickDedupRunner(job, EntryConfig{}, RunnerConfig{TickDedup: TickDedupConfig{Locker: locker}})
    _, _ = dispatchMinute(t, other, runtimeInstance, tickDedupMinute)

    runner.elapsedSince = func(time.Time) time.Duration {
        return 6 * time.Minute
    }

    runner.sweepWonTicks(runtimeInstance, tickDedupMinute.Add(-10*time.Minute))

    report, _ := dispatchMinute(t, runner, runtimeInstance, tickDedupMinute)
    if 1 != len(report.Ran) {
        t.Fatalf("expected the winner to re-run its tick after a step back, got %+v", report)
    }

    runner.sweepWonTicks(runtimeInstance, tickDedupMinute.Add(6*time.Minute))

    if 0 != runner.wonTicks.length() {
        t.Fatalf("expected the ledger to be purged, got %d rows", runner.wonTicks.length())
    }
}

func TestTickDedup_KeyNamesTheEntryAndTheMinute(t *testing.T) {
    keyOf := func(commandName string, entry EntryConfig, config RunnerConfig, at time.Time) string {
        runner := newTickDedupRunner(&countingCommand{commandName: commandName}, entry, config)
        prefix := config.TickDedup.KeyPrefix
        if "" == prefix {
            prefix = "melody:cron:tick:"
        }

        return tickDedupKey(prefix, runner.entries[0], at)
    }

    base := keyOf("app:job", EntryConfig{}, RunnerConfig{}, tickDedupMinute)
    if "melody:cron:tick:app:job:03f01f058c6ac28c:29735100" != base {
        t.Fatalf("unexpected default key %s", base)
    }

    distinct := []string{
        base,
        keyOf("app:job", EntryConfig{Schedule: &Schedule{Minute: "*/2"}}, RunnerConfig{}, tickDedupMinute),
        keyOf("app:job", EntryConfig{Arguments: []string{"a b"}}, RunnerConfig{}, tickDedupMinute),
        keyOf("app:job", EntryConfig{Arguments: []string{"a", "b"}}, RunnerConfig{}, tickDedupMinute),
        keyOf("app:job:other", EntryConfig{}, RunnerConfig{}, tickDedupMinute),
        keyOf("app:job", EntryConfig{}, RunnerConfig{}, tickDedupMinute.Add(time.Minute)),
    }

    seen := make(map[string]bool)
    for _, key := range distinct {
        if true == seen[key] {
            t.Fatalf("expected distinct identities to give distinct keys, %s repeats in %v", key, distinct)
        }

        seen[key] = true
    }

    if base != keyOf("app:job", EntryConfig{}, RunnerConfig{}, tickDedupMinute) {
        t.Fatal("expected one identity to give one key")
    }

    prefixed := keyOf("app:job", EntryConfig{}, RunnerConfig{TickDedup: TickDedupConfig{KeyPrefix: "shop:ticks:"}}, tickDedupMinute)
    if false == strings.HasPrefix(prefixed, "shop:ticks:app:job:") {
        t.Fatalf("expected the configured prefix, got %s", prefixed)
    }

    bucharest, loadErr := time.LoadLocation("Europe/Bucharest")
    if nil != loadErr {
        t.Skipf("no zone data: %v", loadErr)
    }

    firstInstant := time.Date(2026, time.October, 25, 0, 30, 0, 0, time.UTC).In(bucharest)
    secondInstant := firstInstant.Add(time.Hour)
    if firstInstant.Hour() != secondInstant.Hour() {
        t.Fatalf("expected the fall-back to repeat the wall minute, got %v and %v", firstInstant, secondInstant)
    }

    if keyOf("app:job", EntryConfig{}, RunnerConfig{}, firstInstant) == keyOf("app:job", EntryConfig{}, RunnerConfig{}, secondInstant) {
        t.Fatal("expected the two instants of a repeated wall minute to be two keys")
    }
}

func TestTickDedup_TheKeyPrefixDefaultCarriesTheNamespace(t *testing.T) {
    newNamespacedRuntime := func(namespace string) containercontract.Runtime {
        configuration := &stubConfigurationWithKernel{
            stubConfiguration: newStubConfiguration(nil),
            kernel:            &stubKernelConfiguration{namespace: namespace},
        }

        serviceContainer := container.NewContainer(container.ContainerConfig{})
        serviceContainer.MustRegister(
            melodyconfig.ServiceConfig,
            func(resolver containercontract.Resolver) (configcontract.Configuration, error) {
                return configuration, nil
            },
            container.RegisterOptions{},
        )

        return runtime.NewRun(context.Background(), serviceContainer.NewScope())
    }

    locker := newSharedInMemoryLocker(clock.NewFrozenClock(tickDedupMinute))
    runner := newTickDedupRunner(&countingCommand{commandName: "app:job"}, EntryConfig{}, RunnerConfig{TickDedup: TickDedupConfig{Locker: locker}})

    if prefix := runner.resolveTickDedupRun(newNamespacedRuntime("shop")).keyPrefix; "shop:cron:tick:" != prefix {
        t.Fatalf("expected the namespace to open the prefix, got %s", prefix)
    }

    if prefix := runner.resolveTickDedupRun(newRunnerTestRuntime(context.Background())).keyPrefix; "melody:cron:tick:" != prefix {
        t.Fatalf("expected the default namespace's prefix, got %s", prefix)
    }

    named := newTickDedupRunner(&countingCommand{commandName: "app:job"}, EntryConfig{}, RunnerConfig{TickDedup: TickDedupConfig{Locker: locker, KeyPrefix: "orders:ticks:"}})
    if prefix := named.resolveTickDedupRun(newNamespacedRuntime("shop")).keyPrefix; "orders:ticks:" != prefix {
        t.Fatalf("expected a configured prefix to be used verbatim, got %s", prefix)
    }
}

func TestTickDedup_SessionLockerHoldsOneLockPerWonTickUntilTheHold(t *testing.T) {
    job := &countingCommand{commandName: "job:every-minute"}
    locker := newSessionTickLocker()

    runner := newTickDedupRunner(job, EntryConfig{}, RunnerConfig{TickDedup: TickDedupConfig{Locker: locker}})
    runtimeInstance := newRunnerTestRuntime(context.Background())

    _, _ = dispatchMinute(t, runner, runtimeInstance, tickDedupMinute)
    if 1 != locker.heldCount() {
        t.Fatalf("expected one held lock after the run returned, got %d", locker.heldCount())
    }

    other := newTickDedupRunner(job, EntryConfig{}, RunnerConfig{TickDedup: TickDedupConfig{Locker: locker}})
    otherReport, _ := dispatchMinute(t, other, runtimeInstance, tickDedupMinute)
    if 1 != len(otherReport.Skipped) {
        t.Fatalf("expected a second runner to skip while the lock is held, got %+v", otherReport)
    }

    runner.elapsedSince = func(time.Time) time.Duration {
        return 5 * time.Minute
    }
    runner.sweepWonTicks(runtimeInstance, tickDedupMinute.Add(6*time.Minute))

    if 0 != locker.heldCount() {
        t.Fatalf("expected the sweep to release the lock at the hold, got %d", locker.heldCount())
    }
}

func TestTickDedup_TheDocumentCarriesTheSkippedEntries(t *testing.T) {
    job := &countingCommand{commandName: "job:every-minute"}
    locker := newSharedInMemoryLocker(clock.NewFrozenClock(tickDedupMinute))

    first := newTickDedupRunner(job, EntryConfig{}, RunnerConfig{TickDedup: TickDedupConfig{Locker: locker}})
    second := newTickDedupRunner(job, EntryConfig{}, RunnerConfig{TickDedup: TickDedupConfig{Locker: locker}})

    ranReport, _ := dispatchMinute(t, first, newRunnerTestRuntime(context.Background()), tickDedupMinute)
    skippedReport, _ := dispatchMinute(t, second, newRunnerTestRuntime(context.Background()), tickDedupMinute)

    encoded, encodeErr := json.Marshal(ranReport)
    if nil != encodeErr || false == strings.Contains(string(encoded), `"skipped":[]`) {
        t.Fatalf("expected an empty skipped list, got %s (%v)", encoded, encodeErr)
    }

    encoded, _ = json.Marshal(skippedReport)
    if false == strings.Contains(string(encoded), `"skipped":[{"command":"job:every-minute","schedule":"* * * * *","arguments":[],"reason":"the tick is held by another instance"}]`) {
        t.Fatalf("expected the skip row, got %s", encoded)
    }

    if false == isIdleMinute(skippedReport) || true == isIdleMinute(ranReport) {
        t.Fatal("expected a minute of skips only to be idle and a minute that ran not to be")
    }

    var text bytes.Buffer
    _ = second.renderRunReport(parsedContextValues{WriterValue: &text}.parse(), output.Option{}, tickDedupMinute, skippedReport, nil)
    if false == strings.Contains(text.String(), "dispatched 0 of 1 configured entries, skipped 1") {
        t.Fatalf("expected the text form to count the skips, got %q", text.String())
    }

    if "" != skippedSuffixOf(ranReport) {
        t.Fatalf("expected no skipped count for a minute that skipped nothing, got %q", skippedSuffixOf(ranReport))
    }
}

/* the audit's falsifier: two loops whose clocks wake 300ms apart over one locker, a command that returns at once, one run for the minute */
func TestTickDedup_TwoRunnersOneLockerClocksApart(t *testing.T) {
    job := &countingCommand{commandName: "job:every-minute", ran: make(chan struct{}, 4)}
    locker := newSharedInMemoryLocker(clock.NewFrozenClock(tickDedupMinute))
    boundary := tickDedupMinute.Add(time.Minute)

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    startLoop := func() chan error {
        runner := newTickDedupRunner(job, EntryConfig{}, RunnerConfig{TickDedup: TickDedupConfig{Locker: locker}})

        var nowCallCount atomic.Int32
        runner.now = func() time.Time {
            if 2 >= nowCallCount.Add(1) {
                return boundary.Add(-5 * time.Millisecond)
            }

            return boundary.Add(-2 * time.Minute)
        }

        finished := make(chan error, 1)
        go func() {
            finished <- runner.runLoop(newRunnerTestRuntime(ctx), runner.location, nil)
        }()

        return finished
    }

    firstFinished := startLoop()

    select {
    case <-job.ran:
    case <-time.After(2 * time.Second):
        t.Fatal("expected the first loop to run the minute")
    }

    time.Sleep(300 * time.Millisecond)
    secondFinished := startLoop()
    time.Sleep(300 * time.Millisecond)

    cancel()
    <-firstFinished
    <-secondFinished

    if 1 != job.runCount.Load() {
        t.Fatalf("expected one run for the minute across both loops, ran %d", job.runCount.Load())
    }
}
