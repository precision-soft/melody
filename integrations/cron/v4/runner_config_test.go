package cron

import (
    "context"
    "errors"
    "strings"
    "sync/atomic"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    clicontract "github.com/precision-soft/melody/v4/cli/contract"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    "github.com/precision-soft/melody/v4/runtime"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
)

func refusalOfRunnerConstruction(t *testing.T, configuration *Configuration, config RunnerConfig) *exception.Error {
    t.Helper()

    var refusal *exception.Error

    func() {
        defer func() {
            recovered := recover()
            if nil == recovered {
                return
            }

            recoveredErr, isError := recovered.(error)
            if false == isError || false == errors.As(recoveredErr, &refusal) {
                t.Fatalf("expected an exception error, got %T: %v", recovered, recovered)
            }
        }()

        NewRunnerCommand(configuration, config)
    }()

    if nil == refusal {
        t.Fatal("expected NewRunnerCommand to refuse the configuration")
    }

    return refusal
}

func TestResolveRunnerConfig_AnEmptyConfigTakesEveryDefault(t *testing.T) {
    resolved, resolveErr := resolveRunnerConfig(RunnerConfig{})
    if nil != resolveErr {
        t.Fatalf("unexpected error: %v", resolveErr)
    }

    if 64*1024 != resolved.outputCaptureLimit {
        t.Fatalf("expected a 64 KiB output capture limit, got %d", resolved.outputCaptureLimit)
    }

    if 3*time.Hour != resolved.clockJumpResetThreshold {
        t.Fatalf("expected a three-hour reset threshold, got %v", resolved.clockJumpResetThreshold)
    }

    if true == resolved.timeout.Unlimited || time.Hour != resolved.timeout.Value {
        t.Fatalf("expected a one-hour timeout, got %+v", resolved.timeout)
    }

    if 5*time.Minute != resolved.gracefulTimeout {
        t.Fatalf("expected a five-minute graceful timeout, got %v", resolved.gracefulTimeout)
    }
}

func TestResolveRunnerConfig_GivenValuesReachTheResolvedRunner(t *testing.T) {
    resolved, resolveErr := resolveRunnerConfig(RunnerConfig{
        Dialect:                 RunnerDialectKubernetes,
        OutputCaptureLimit:      bound.Of(16),
        ClockJumpResetThreshold: bound.Of(2 * time.Minute),
        Timeout:                 bound.UnlimitedDuration(),
        GracefulTimeout:         bound.Of(time.Millisecond),
    })
    if nil != resolveErr {
        t.Fatalf("unexpected error: %v", resolveErr)
    }

    if RunnerDialectKubernetes != resolved.dialect || 16 != resolved.outputCaptureLimit || 2*time.Minute != resolved.clockJumpResetThreshold {
        t.Fatalf("expected the given values, got %+v", resolved)
    }

    if false == resolved.timeout.Unlimited {
        t.Fatalf("expected an unlimited runner timeout to be admitted, got %+v", resolved.timeout)
    }

    if time.Millisecond != resolved.gracefulTimeout {
        t.Fatalf("expected the given graceful timeout, got %v", resolved.gracefulTimeout)
    }
}

func TestResolveRunnerConfig_RefusesAValueOutsideItsRuleNamingTheField(t *testing.T) {
    testCases := []struct {
        name   string
        config RunnerConfig
        field  string
    }{
        {"zero output capture limit", RunnerConfig{OutputCaptureLimit: bound.Of(0)}, "RunnerConfig.OutputCaptureLimit"},
        {"unlimited output capture limit", RunnerConfig{OutputCaptureLimit: bound.UnlimitedCount()}, "RunnerConfig.OutputCaptureLimit"},
        {"one-minute reset threshold", RunnerConfig{ClockJumpResetThreshold: bound.Of(time.Minute)}, "RunnerConfig.ClockJumpResetThreshold"},
        {"reset threshold not in whole minutes", RunnerConfig{ClockJumpResetThreshold: bound.Of(150 * time.Second)}, "RunnerConfig.ClockJumpResetThreshold"},
        {"unlimited reset threshold", RunnerConfig{ClockJumpResetThreshold: bound.UnlimitedDuration()}, "RunnerConfig.ClockJumpResetThreshold"},
        {"zero timeout", RunnerConfig{Timeout: bound.Of(time.Duration(0))}, "RunnerConfig.Timeout"},
        {"zero graceful timeout", RunnerConfig{GracefulTimeout: bound.Of(time.Duration(0))}, "RunnerConfig.GracefulTimeout"},
        {"unlimited graceful timeout", RunnerConfig{GracefulTimeout: bound.UnlimitedDuration()}, "RunnerConfig.GracefulTimeout"},
    }

    for _, testCase := range testCases {
        t.Run(testCase.name, func(t *testing.T) {
            _, resolveErr := resolveRunnerConfig(testCase.config)

            var refusal *exception.Error
            if false == errors.As(resolveErr, &refusal) {
                t.Fatalf("expected an exception error, got %v", resolveErr)
            }

            if testCase.field != refusal.Context()["field"] {
                t.Fatalf("expected the refusal to name %s, got %v", testCase.field, refusal.Context())
            }

            constructionRefusal := refusalOfRunnerConstruction(t, nil, testCase.config)
            if testCase.field != constructionRefusal.Context()["field"] {
                t.Fatalf("expected NewRunnerCommand to refuse naming %s, got %v", testCase.field, constructionRefusal.Context())
            }
        })
    }
}

func TestResolveRunnerConfig_RefusesAnUnknownDialect(t *testing.T) {
    _, resolveErr := resolveRunnerConfig(RunnerConfig{Dialect: RunnerDialect("solaris")})
    if false == errors.Is(resolveErr, ErrUnknownRunnerDialect) {
        t.Fatalf("expected ErrUnknownRunnerDialect, got %v", resolveErr)
    }
}

func TestNewRunnerCommand_RefusesAnEntryLimitOutsideItsRuleNamingTheFieldAndTheCommand(t *testing.T) {
    testCases := []struct {
        name  string
        entry EntryConfig
        field string
    }{
        {"zero timeout", EntryConfig{Schedule: &Schedule{Minute: "0"}, Timeout: bound.Of(time.Duration(0))}, "EntryConfig.Timeout"},
        {"negative timeout", EntryConfig{Schedule: &Schedule{Minute: "0"}, Timeout: bound.Of(-time.Second)}, "EntryConfig.Timeout"},
        {"zero graceful timeout", EntryConfig{Schedule: &Schedule{Minute: "0"}, GracefulTimeout: bound.Of(time.Duration(0))}, "EntryConfig.GracefulTimeout"},
        {"unlimited graceful timeout", EntryConfig{Schedule: &Schedule{Minute: "0"}, GracefulTimeout: bound.UnlimitedDuration()}, "EntryConfig.GracefulTimeout"},
        {"zero instances", EntryConfig{Schedule: &Schedule{Minute: "0"}, Instances: bound.Of(0)}, "EntryConfig.Instances"},
        {"unlimited instances", EntryConfig{Schedule: &Schedule{Minute: "0"}, Instances: bound.UnlimitedCount()}, "EntryConfig.Instances"},
    }

    for _, testCase := range testCases {
        t.Run(testCase.name, func(t *testing.T) {
            configuration := NewConfiguration().Schedule("job:top", testCase.entry)

            refusal := refusalOfRunnerConstruction(t, configuration, RunnerConfig{Commands: []clicontract.Command{newRecordingCommand("job:top")}})

            if testCase.field != refusal.Context()["field"] || "job:top" != refusal.Context()["commandName"] {
                t.Fatalf("expected the refusal to name %s and the command, got %v", testCase.field, refusal.Context())
            }
        })
    }
}

func TestNewRunnerCommand_TheRunnersTimeoutAndWindowReachAnEntryThatNamesNone(t *testing.T) {
    configuration := NewConfiguration().
        Schedule("job:inherits", EntryConfig{Schedule: &Schedule{Minute: "0"}}).
        Schedule("job:own", EntryConfig{Schedule: &Schedule{Minute: "0"}, Timeout: bound.Of(time.Minute), GracefulTimeout: bound.Of(time.Second)})

    runner := NewRunnerCommand(configuration, RunnerConfig{
        Commands:        []clicontract.Command{newRecordingCommand("job:inherits"), newRecordingCommand("job:own")},
        Timeout:         bound.Of(10 * time.Minute),
        GracefulTimeout: bound.Of(7 * time.Minute),
    })

    if 10*time.Minute != runner.entries[0].timeout || 7*time.Minute != runner.gracefulTimeoutOf(runner.entries[0]) {
        t.Fatalf("expected the runner's timeout and window, got %v and %v", runner.entries[0].timeout, runner.gracefulTimeoutOf(runner.entries[0]))
    }

    if time.Minute != runner.entries[1].timeout || time.Second != runner.gracefulTimeoutOf(runner.entries[1]) {
        t.Fatalf("expected the entry's own timeout and window, got %v and %v", runner.entries[1].timeout, runner.gracefulTimeoutOf(runner.entries[1]))
    }
}

/* a command that watches its context under an entry naming no timeout is cancelled by the runner's, here shortened so the test does not wait the hour the default gives */
func TestRunnerCommand_TheRunnersTimeoutCancelsAnEntryThatNamesNone(t *testing.T) {
    job := newContextWatchingCommand("job:top")

    configuration := NewConfiguration().
        Schedule("job:top", EntryConfig{Schedule: &Schedule{Minute: "0"}})

    runner := NewRunnerCommand(configuration, RunnerConfig{
        Commands:        []clicontract.Command{job},
        Timeout:         bound.Of(50 * time.Millisecond),
        GracefulTimeout: bound.Of(2 * time.Second),
    })

    invokeErr := runner.invokeRun(newRunnerTestRuntime(context.Background()), runner.entries[0], logging.GenerateProcessId())
    if false == errors.Is(invokeErr, ErrCommandTimeout) {
        t.Fatalf("expected the runner's timeout to cancel the run, got %v", invokeErr)
    }
}

func TestRunnerCommand_TheOutputCaptureLimitReachesEveryRun(t *testing.T) {
    job := &outputWritingProbeCommand{commandName: "job:verbose", output: strings.Repeat("x", 17)}

    configuration := NewConfiguration().
        Schedule("job:verbose", EntryConfig{Schedule: &Schedule{Minute: "0"}})

    captured := &capturingRunnerLogger{}
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    serviceContainer.MustRegister(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return captured, nil
        },
        container.RegisterOptions{},
    )

    runner := NewRunnerCommand(configuration, RunnerConfig{Commands: []clicontract.Command{job}, OutputCaptureLimit: bound.Of(16)})

    at := time.Date(2026, time.July, 15, 9, 0, 0, 0, time.UTC)
    if runErr := runner.runDue(runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer), at); nil != runErr {
        t.Fatalf("unexpected error: %v", runErr)
    }

    record, found := captured.outputRecordFor("job:verbose")
    if false == found {
        t.Fatal("expected a record naming the job that wrote")
    }

    if strings.Repeat("x", 16) != record.context["commandOutput"] || 1 != record.context["commandOutputDroppedBytes"] {
        t.Fatalf("expected 16 bytes kept and 1 dropped, got %#v and %#v", record.context["commandOutput"], record.context["commandOutputDroppedBytes"])
    }
}

/* 10:00, then a ten-minute jump: the default threshold catches 10:05 up, a two-minute one re-anchors without it */
func TestRunnerCommand_TheClockJumpResetThresholdReachesTheLoop(t *testing.T) {
    fixedRunsUnder := func(t *testing.T, threshold bound.Duration) int32 {
        t.Helper()

        fixedJob := &countingCommand{commandName: "job:fixed", ran: make(chan struct{}, 4)}
        wildcardRan := make(chan struct{}, 4)
        wildcardJob := &countingCommand{commandName: "job:wildcard", ran: wildcardRan}

        configuration := NewConfiguration().
            Schedule("job:fixed", EntryConfig{Schedule: &Schedule{Minute: "5", Hour: "10"}}).
            Schedule("job:wildcard", EntryConfig{})

        runner := NewRunnerCommand(configuration, RunnerConfig{
            Commands:                []clicontract.Command{fixedJob, wildcardJob},
            ClockJumpResetThreshold: threshold,
        })

        boundary := time.Date(2026, time.July, 15, 10, 0, 0, 0, time.UTC)
        wakes := []time.Time{
            boundary.Add(-10 * time.Millisecond),
            boundary.Add(10*time.Minute - 10*time.Millisecond),
        }

        var nowCallCount atomic.Int32
        runner.now = func() time.Time {
            callIndex := int(nowCallCount.Add(1))
            if callIndex <= len(wakes) {
                return wakes[callIndex-1]
            }

            return boundary.Add(10*time.Minute + 100*time.Millisecond)
        }

        ctx, cancel := context.WithCancel(context.Background())
        defer cancel()

        finished := make(chan error, 1)
        go func() {
            finished <- runner.runLoop(newRunnerTestRuntime(ctx), runner.location, nil)
        }()

        for wake := 0; wake < 2; wake++ {
            select {
            case <-wildcardRan:
            case <-time.After(3 * time.Second):
                t.Fatalf("expected the wildcard entry to follow wake %d", wake+1)
            }
        }

        cancel()

        select {
        case <-finished:
        case <-time.After(3 * time.Second):
            t.Fatal("runner loop ignored the cancelled context")
        }

        return fixedJob.runCount.Load()
    }

    if caughtUp := fixedRunsUnder(t, bound.Duration{}); 1 != caughtUp {
        t.Fatalf("expected the default threshold to catch the skipped 10:05 up once, ran %d", caughtUp)
    }

    if reanchored := fixedRunsUnder(t, bound.Of(2*time.Minute)); 0 != reanchored {
        t.Fatalf("expected a two-minute threshold to re-anchor without catch-up, ran %d", reanchored)
    }
}

func TestReconcileWallClock_ReanchorsAtTheGivenThreshold(t *testing.T) {
    previousTarget := time.Date(2026, time.July, 15, 10, 0, 0, 0, time.UTC)

    if _, _, note := reconcileWallClock(previousTarget, previousTarget.Add(2*time.Minute), 2*time.Minute); "" == note {
        t.Fatal("expected a jump of exactly the threshold to re-anchor")
    }

    if _, _, note := reconcileWallClock(previousTarget, previousTarget.Add(time.Minute), 2*time.Minute); "" != note {
        t.Fatalf("expected an ordinary tick under a two-minute threshold not to re-anchor, got %q", note)
    }
}

type deadlineRecordingCommand struct {
    commandName string
    hasDeadline atomic.Bool
    deadline    atomic.Int64
}

func (instance *deadlineRecordingCommand) Name() string {
    return instance.commandName
}

func (instance *deadlineRecordingCommand) Description() string {
    return "deadline recording command"
}

func (instance *deadlineRecordingCommand) Flags() []clicontract.Flag {
    return nil
}

func (instance *deadlineRecordingCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext *clicontract.CommandContext) error {
    deadline, hasDeadline := runtimeInstance.Context().Deadline()
    instance.hasDeadline.Store(hasDeadline)
    instance.deadline.Store(deadline.UnixNano())

    return nil
}

func TestRunnerCommand_AnUnlimitedTimeoutHandsTheCommandAContextWithNoDeadline(t *testing.T) {
    unlimited := &deadlineRecordingCommand{commandName: "job:unlimited"}
    bounded := &deadlineRecordingCommand{commandName: "job:bounded"}

    configuration := NewConfiguration().
        Schedule("job:unlimited", EntryConfig{Schedule: &Schedule{Minute: "0"}, Timeout: bound.UnlimitedDuration()}).
        Schedule("job:bounded", EntryConfig{Schedule: &Schedule{Minute: "0"}})

    runner := NewRunnerCommand(configuration, RunnerConfig{Commands: []clicontract.Command{unlimited, bounded}})

    started := time.Now()
    at := time.Date(2026, time.July, 15, 9, 0, 0, 0, time.UTC)
    if runErr := runner.runDue(newRunnerTestRuntime(context.Background()), at); nil != runErr {
        t.Fatalf("unexpected error: %v", runErr)
    }

    if true == unlimited.hasDeadline.Load() {
        t.Fatal("expected an unlimited timeout to hand the command a context with no deadline")
    }

    if false == bounded.hasDeadline.Load() {
        t.Fatal("expected an entry naming no timeout to run under the runner's one-hour deadline")
    }

    if remaining := time.Unix(0, bounded.deadline.Load()).Sub(started); remaining < 59*time.Minute || remaining > 61*time.Minute {
        t.Fatalf("expected a deadline about one hour ahead, got %v", remaining)
    }
}
