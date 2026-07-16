package cron

import (
    "context"
    "errors"
    "fmt"
    "strings"
    "sync/atomic"
    "testing"
    "time"

    clicontract "github.com/precision-soft/melody/cli/contract"
    "github.com/precision-soft/melody/container"
    containercontract "github.com/precision-soft/melody/container/contract"
    "github.com/precision-soft/melody/exception"
    "github.com/precision-soft/melody/runtime"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
    urfavecli "github.com/urfave/cli/v3"
)

type recordingCommand struct {
    commandName string
    runCount    int
    runErr      error
}

func newRecordingCommand(name string) *recordingCommand {
    return &recordingCommand{commandName: name}
}

func (instance *recordingCommand) Name() string {
    return instance.commandName
}

func (instance *recordingCommand) Description() string {
    return "recording command"
}

func (instance *recordingCommand) Flags() []clicontract.Flag {
    return nil
}

func (instance *recordingCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext *clicontract.CommandContext) error {
    instance.runCount++

    return instance.runErr
}

func newRunnerTestRuntime(ctx context.Context) runtimecontract.Runtime {
    serviceContainer := container.NewContainer()

    return runtime.New(ctx, serviceContainer.NewScope(), serviceContainer)
}

type panickingCommand struct {
    commandName string
}

func (instance *panickingCommand) Name() string {
    return instance.commandName
}

func (instance *panickingCommand) Description() string {
    return "panicking command"
}

func (instance *panickingCommand) Flags() []clicontract.Flag {
    return nil
}

func (instance *panickingCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext *clicontract.CommandContext) error {
    panic("scheduled command panicked on purpose")
}

const probeFlagNameBatchSize = "batch-size"

type flagDefaultProbeCommand struct {
    commandName       string
    observedBatchSize int
}

func (instance *flagDefaultProbeCommand) Name() string {
    return instance.commandName
}

func (instance *flagDefaultProbeCommand) Description() string {
    return "flag default probe command"
}

func (instance *flagDefaultProbeCommand) Flags() []clicontract.Flag {
    return []clicontract.Flag{
        &clicontract.IntFlag{
            Name:  probeFlagNameBatchSize,
            Value: 100,
        },
    }
}

func (instance *flagDefaultProbeCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext *clicontract.CommandContext) error {
    instance.observedBatchSize = commandContext.Int(probeFlagNameBatchSize)

    return nil
}

type writerProbeCommand struct {
    commandName string
    runCount    int
}

func (instance *writerProbeCommand) Name() string {
    return instance.commandName
}

func (instance *writerProbeCommand) Description() string {
    return "writer probe command"
}

func (instance *writerProbeCommand) Flags() []clicontract.Flag {
    return nil
}

func (instance *writerProbeCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext *clicontract.CommandContext) error {
    instance.runCount++

    _, writeErr := fmt.Fprintln(commandContext.Writer, "writer probe output")

    return writeErr
}

type argsProbeCommand struct {
    commandName        string
    runCount           int
    observedArgsLength int
}

func (instance *argsProbeCommand) Name() string {
    return instance.commandName
}

func (instance *argsProbeCommand) Description() string {
    return "args probe command"
}

func (instance *argsProbeCommand) Flags() []clicontract.Flag {
    return nil
}

func (instance *argsProbeCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext *clicontract.CommandContext) error {
    instance.runCount++
    instance.observedArgsLength = commandContext.Args().Len()

    return nil
}

type signalingCommand struct {
    commandName string
    ran         chan struct{}
}

func (instance *signalingCommand) Name() string {
    return instance.commandName
}

func (instance *signalingCommand) Description() string {
    return "signaling command"
}

func (instance *signalingCommand) Flags() []clicontract.Flag {
    return nil
}

func (instance *signalingCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext *clicontract.CommandContext) error {
    select {
    case instance.ran <- struct{}{}:
    default:
    }

    return nil
}

type rendezvousCommand struct {
    commandName string
    arrived     chan struct{}
    peerArrived chan struct{}
}

func (instance *rendezvousCommand) Name() string {
    return instance.commandName
}

func (instance *rendezvousCommand) Description() string {
    return "rendezvous command"
}

func (instance *rendezvousCommand) Flags() []clicontract.Flag {
    return nil
}

func (instance *rendezvousCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext *clicontract.CommandContext) error {
    close(instance.arrived)

    select {
    case <-instance.peerArrived:
        return nil
    case <-time.After(2 * time.Second):
        return errors.New("the peer command never ran concurrently")
    }
}

type blockingCommand struct {
    commandName    string
    started        chan struct{}
    completedCount atomic.Int32
}

func (instance *blockingCommand) Name() string {
    return instance.commandName
}

func (instance *blockingCommand) Description() string {
    return "blocking command"
}

func (instance *blockingCommand) Flags() []clicontract.Flag {
    return nil
}

func (instance *blockingCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext *clicontract.CommandContext) error {
    select {
    case instance.started <- struct{}{}:
    default:
    }

    <-runtimeInstance.Context().Done()

    instance.completedCount.Add(1)

    return nil
}

type failingCloseScope struct {
    containercontract.Scope
    closeErr error
}

func (instance failingCloseScope) Close() error {
    return instance.closeErr
}

type failingScopeContainer struct {
    containercontract.Container
    scopeCloseErr error
}

func (instance failingScopeContainer) NewScope() containercontract.Scope {
    return failingCloseScope{
        Scope:    instance.Container.NewScope(),
        closeErr: instance.scopeCloseErr,
    }
}

func TestRunnerCommand_RunDueInvokesOnlyMatchingCommands(t *testing.T) {
    topOfHour := newRecordingCommand("job:top")
    halfHour := newRecordingCommand("job:half")

    configuration := NewConfiguration().
        Schedule("job:top", &EntryConfig{Schedule: &Schedule{Minute: "0"}}).
        Schedule("job:half", &EntryConfig{Schedule: &Schedule{Minute: "30"}})

    runner := NewRunnerCommand(configuration, RunnerDialectCrontab, topOfHour, halfHour)

    at := time.Date(2026, time.July, 15, 9, 0, 0, 0, time.UTC)
    if runErr := runner.runDue(newRunnerTestRuntime(context.Background()), at); nil != runErr {
        t.Fatalf("unexpected error: %v", runErr)
    }

    if 1 != topOfHour.runCount {
        t.Fatalf("expected the top-of-hour command to run once, ran %d", topOfHour.runCount)
    }

    if 0 != halfHour.runCount {
        t.Fatalf("expected the half-hour command not to run at minute zero, ran %d", halfHour.runCount)
    }
}

func TestRunnerCommand_OnceFlagRunsDueCommandsAndExits(t *testing.T) {
    job := newRecordingCommand("job:top")

    configuration := NewConfiguration().
        Schedule("job:top", &EntryConfig{Schedule: &Schedule{Minute: "0"}})

    runner := NewRunnerCommand(configuration, RunnerDialectCrontab, job)
    runner.now = func() time.Time {
        return time.Date(2026, time.July, 15, 9, 0, 0, 0, time.UTC)
    }

    cliCommand := &urfavecli.Command{
        Name:  runner.Name(),
        Flags: runner.Flags(),
        Action: func(ctx context.Context, commandContext *urfavecli.Command) error {
            return runner.Run(newRunnerTestRuntime(ctx), commandContext)
        },
    }

    if runErr := cliCommand.Run(context.Background(), []string{runner.Name(), "--once"}); nil != runErr {
        t.Fatalf("unexpected error running --once: %v", runErr)
    }

    if 1 != job.runCount {
        t.Fatalf("expected --once to run the due command exactly once, ran %d", job.runCount)
    }
}

func TestRunnerCommand_AggregatesFailuresWithoutStoppingOthers(t *testing.T) {
    failing := newRecordingCommand("job:failing")
    failing.runErr = errors.New("boom")
    healthy := newRecordingCommand("job:healthy")

    configuration := NewConfiguration().
        Schedule("job:failing", &EntryConfig{Schedule: &Schedule{Minute: "0"}}).
        Schedule("job:healthy", &EntryConfig{Schedule: &Schedule{Minute: "0"}})

    runner := NewRunnerCommand(configuration, RunnerDialectCrontab, failing, healthy)

    at := time.Date(2026, time.July, 15, 9, 0, 0, 0, time.UTC)
    runErr := runner.runDue(newRunnerTestRuntime(context.Background()), at)
    if nil == runErr {
        t.Fatal("expected an aggregate error when a scheduled command fails")
    }

    if 1 != healthy.runCount {
        t.Fatalf("expected the healthy command to still run, ran %d", healthy.runCount)
    }
}

func TestRunnerCommand_UnknownScheduledCommandPanicsAtConstruction(t *testing.T) {
    defer func() {
        if recovered := recover(); nil == recovered {
            t.Fatal("expected a panic when a scheduled command has no matching registered command")
        }
    }()

    configuration := NewConfiguration().
        Schedule("job:missing", &EntryConfig{Schedule: &Schedule{Minute: "0"}})

    NewRunnerCommand(configuration, RunnerDialectCrontab)
}

func TestRunnerCommand_InvalidSchedulePanicsAtConstruction(t *testing.T) {
    defer func() {
        if recovered := recover(); nil == recovered {
            t.Fatal("expected a panic when a scheduled command carries a malformed schedule")
        }
    }()

    job := newRecordingCommand("job:top")

    configuration := NewConfiguration().
        Schedule("job:top", &EntryConfig{Schedule: &Schedule{Minute: "99"}})

    NewRunnerCommand(configuration, RunnerDialectCrontab, job)
}

func TestRunnerCommand_PanicInCommandIsContainedAndOthersStillRun(t *testing.T) {
    panicking := &panickingCommand{commandName: "job:panicking"}
    healthy := newRecordingCommand("job:healthy")

    configuration := NewConfiguration().
        Schedule("job:panicking", &EntryConfig{Schedule: &Schedule{Minute: "0"}}).
        Schedule("job:healthy", &EntryConfig{Schedule: &Schedule{Minute: "0"}})

    runner := NewRunnerCommand(configuration, RunnerDialectCrontab, panicking, healthy)

    at := time.Date(2026, time.July, 15, 9, 0, 0, 0, time.UTC)
    runErr := runner.runDue(newRunnerTestRuntime(context.Background()), at)
    if nil == runErr {
        t.Fatal("expected an aggregate error when a scheduled command panics")
    }

    if 1 != healthy.runCount {
        t.Fatalf("expected the healthy command to still run after another command panicked, ran %d", healthy.runCount)
    }

    aggregate, isExceptionError := runErr.(*exception.Error)
    if false == isExceptionError {
        t.Fatalf("expected an exception error from runDue, got %T", runErr)
    }

    if false == strings.Contains(fmt.Sprintf("%v", aggregate.Context()["commands"]), "job:panicking") {
        t.Fatalf("expected the panicking command to be aggregated as failed, got %v", aggregate.Context()["commands"])
    }
}

func TestRunnerCommand_CommandContextReadsDeclaredFlagDefaults(t *testing.T) {
    probe := &flagDefaultProbeCommand{commandName: "job:probe"}

    configuration := NewConfiguration().
        Schedule("job:probe", &EntryConfig{Schedule: &Schedule{Minute: "0"}})

    runner := NewRunnerCommand(configuration, RunnerDialectCrontab, probe)

    at := time.Date(2026, time.July, 15, 9, 0, 0, 0, time.UTC)
    if runErr := runner.runDue(newRunnerTestRuntime(context.Background()), at); nil != runErr {
        t.Fatalf("unexpected error: %v", runErr)
    }

    if 100 != probe.observedBatchSize {
        t.Fatalf("expected the declared flag default 100, got %d", probe.observedBatchSize)
    }
}

func TestRunnerCommand_CommandContextWriterIsUsable(t *testing.T) {
    probe := &writerProbeCommand{commandName: "job:writer"}

    configuration := NewConfiguration().
        Schedule("job:writer", &EntryConfig{Schedule: &Schedule{Minute: "0"}})

    runner := NewRunnerCommand(configuration, RunnerDialectCrontab, probe)

    at := time.Date(2026, time.July, 15, 9, 0, 0, 0, time.UTC)
    if runErr := runner.runDue(newRunnerTestRuntime(context.Background()), at); nil != runErr {
        t.Fatalf("unexpected error writing to the command context writer: %v", runErr)
    }

    if 1 != probe.runCount {
        t.Fatalf("expected the writer probe to run once, ran %d", probe.runCount)
    }
}

func TestRunnerCommand_CommandContextArgsIsUsable(t *testing.T) {
    probe := &argsProbeCommand{commandName: "job:args"}

    configuration := NewConfiguration().
        Schedule("job:args", &EntryConfig{Schedule: &Schedule{Minute: "0"}})

    runner := NewRunnerCommand(configuration, RunnerDialectCrontab, probe)

    at := time.Date(2026, time.July, 15, 9, 0, 0, 0, time.UTC)
    if runErr := runner.runDue(newRunnerTestRuntime(context.Background()), at); nil != runErr {
        t.Fatalf("unexpected error reading the command context args: %v", runErr)
    }

    if 1 != probe.runCount {
        t.Fatalf("expected the args probe to run once, ran %d", probe.runCount)
    }

    if 0 != probe.observedArgsLength {
        t.Fatalf("expected no positional arguments, got %d", probe.observedArgsLength)
    }
}

/** @info the fake clock returns an instant just before the targeted minute while the loop anchors the chain and arms the timer, then a stepped-back instant for every later read; only an evaluation pinned to the armed minute still fires the schedule, and the stepped-back reads may only influence the arming of later wakes. */
func TestRunnerCommand_LoopEvaluatesTheTimerTargetedMinute(t *testing.T) {
    ran := make(chan struct{}, 1)
    job := &signalingCommand{commandName: "job:targeted", ran: ran}

    configuration := NewConfiguration().
        Schedule("job:targeted", &EntryConfig{Schedule: &Schedule{Minute: "30"}})

    runner := NewRunnerCommand(configuration, RunnerDialectCrontab, job)

    targetedMinute := time.Date(2026, time.July, 15, 9, 30, 0, 0, time.UTC)

    var nowCallCount atomic.Int32
    runner.now = func() time.Time {
        if 2 >= nowCallCount.Add(1) {
            return targetedMinute.Add(-5 * time.Millisecond)
        }

        return targetedMinute.Add(-2 * time.Minute)
    }

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    finished := make(chan error, 1)
    go func() {
        finished <- runner.runLoop(newRunnerTestRuntime(ctx))
    }()

    select {
    case <-ran:
    case <-time.After(2 * time.Second):
        t.Fatal("expected the loop to evaluate the timer-targeted minute instead of the stepped-back wall clock")
    }

    cancel()

    select {
    case <-finished:
    case <-time.After(3 * time.Second):
        t.Fatal("runner loop ignored the cancelled context")
    }
}

/** @info each command releases only when its peer has also started, so the tick passes solely when the due entries run concurrently, like crontab starting an independent process per entry. */
func TestRunnerCommand_RunDueRunsDueEntriesConcurrently(t *testing.T) {
    firstArrived := make(chan struct{})
    secondArrived := make(chan struct{})

    first := &rendezvousCommand{commandName: "job:first", arrived: firstArrived, peerArrived: secondArrived}
    second := &rendezvousCommand{commandName: "job:second", arrived: secondArrived, peerArrived: firstArrived}

    configuration := NewConfiguration().
        Schedule("job:first", &EntryConfig{Schedule: &Schedule{Minute: "0"}}).
        Schedule("job:second", &EntryConfig{Schedule: &Schedule{Minute: "0"}})

    runner := NewRunnerCommand(configuration, RunnerDialectCrontab, first, second)

    at := time.Date(2026, time.July, 15, 9, 0, 0, 0, time.UTC)
    if runErr := runner.runDue(newRunnerTestRuntime(context.Background()), at); nil != runErr {
        t.Fatalf("expected both due commands to run concurrently and succeed, got %v", runErr)
    }
}

/** @info the job blocks until the runtime context is cancelled, so a second start signal while no run has completed proves the loop armed and fired the next minute without waiting on the running job — and that an entry may overlap itself. */
func TestRunnerCommand_LoopTicksWhileAJobIsStillRunning(t *testing.T) {
    started := make(chan struct{}, 16)
    job := &blockingCommand{commandName: "job:blocking", started: started}

    configuration := NewConfiguration().
        Schedule("job:blocking", &EntryConfig{})

    runner := NewRunnerCommand(configuration, RunnerDialectCrontab, job)

    baseMinute := time.Date(2026, time.July, 15, 9, 29, 0, 0, time.UTC)

    var nowCallCount atomic.Int32
    runner.now = func() time.Time {
        callIndex := nowCallCount.Add(1)
        if 1 == callIndex {
            return baseMinute.Add(59*time.Second + 990*time.Millisecond)
        }

        return baseMinute.Add(time.Duration(callIndex-1) * time.Minute).Add(-5 * time.Millisecond)
    }

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    finished := make(chan error, 1)
    go func() {
        finished <- runner.runLoop(newRunnerTestRuntime(ctx))
    }()

    select {
    case <-started:
    case <-time.After(3 * time.Second):
        t.Fatal("expected the first job instance to start")
    }

    select {
    case <-started:
    case <-time.After(3 * time.Second):
        t.Fatal("expected the loop to tick and start a second job instance while the first was still running")
    }

    if 0 != job.completedCount.Load() {
        t.Fatalf("expected both job instances to still be running, %d completed", job.completedCount.Load())
    }

    cancel()

    select {
    case runErr := <-finished:
        if nil != runErr {
            t.Fatalf("expected a clean nil on cancellation, got %v", runErr)
        }
    case <-time.After(5 * time.Second):
        t.Fatal("runner loop ignored the cancelled context")
    }

    if 2 > job.completedCount.Load() {
        t.Fatalf("expected the loop to wait for the in-flight jobs, %d completed", job.completedCount.Load())
    }
}

/** @info the job finishes only after the cancellation reaches its child context, so a completion observed once the loop returned proves the loop waited for the in-flight job instead of abandoning it. */
func TestRunnerCommand_LoopWaitsForInFlightJobsOnCancellation(t *testing.T) {
    started := make(chan struct{}, 1)
    job := &blockingCommand{commandName: "job:blocking", started: started}

    configuration := NewConfiguration().
        Schedule("job:blocking", &EntryConfig{})

    runner := NewRunnerCommand(configuration, RunnerDialectCrontab, job)

    baseMinute := time.Date(2026, time.July, 15, 9, 29, 0, 0, time.UTC)

    var nowCallCount atomic.Int32
    runner.now = func() time.Time {
        callIndex := nowCallCount.Add(1)
        if 1 == callIndex {
            return baseMinute.Add(59*time.Second + 990*time.Millisecond)
        }

        if 2 == callIndex {
            return baseMinute.Add(59*time.Second + 995*time.Millisecond)
        }

        return baseMinute.Add(time.Minute + 100*time.Millisecond)
    }

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    finished := make(chan error, 1)
    go func() {
        finished <- runner.runLoop(newRunnerTestRuntime(ctx))
    }()

    select {
    case <-started:
    case <-time.After(3 * time.Second):
        t.Fatal("expected the job to start")
    }

    cancel()

    select {
    case runErr := <-finished:
        if nil != runErr {
            t.Fatalf("expected a clean nil on cancellation, got %v", runErr)
        }
    case <-time.After(5 * time.Second):
        t.Fatal("runner loop ignored the cancelled context")
    }

    if 1 != job.completedCount.Load() {
        t.Fatalf("expected the loop to return only after the in-flight job completed, %d completed", job.completedCount.Load())
    }
}

func TestRunnerCommand_InvokeReportsChildScopeCloseError(t *testing.T) {
    job := newRecordingCommand("job:top")

    configuration := NewConfiguration().
        Schedule("job:top", &EntryConfig{Schedule: &Schedule{Minute: "0"}})

    runner := NewRunnerCommand(configuration, RunnerDialectCrontab, job)

    scopeCloseErr := errors.New("scope close failed")
    serviceContainer := container.NewContainer()
    runtimeInstance := runtime.New(
        context.Background(),
        serviceContainer.NewScope(),
        failingScopeContainer{Container: serviceContainer, scopeCloseErr: scopeCloseErr},
    )

    invokeErr := runner.invoke(runtimeInstance, runner.entries[0])
    if nil == invokeErr {
        t.Fatal("expected the child scope close error to surface from invoke")
    }

    if false == errors.Is(invokeErr, scopeCloseErr) {
        t.Fatalf("expected the invoke error to wrap the scope close error, got %v", invokeErr)
    }

    if 1 != job.runCount {
        t.Fatalf("expected the command to run before the scope close error surfaced, ran %d", job.runCount)
    }
}

func TestRunnerCommand_CustomArgvEntryPanicsAtConstruction(t *testing.T) {
    defer func() {
        recovered := recover()
        if nil == recovered {
            t.Fatal("expected a panic for an entry with a custom argv")
        }

        recoveredErr, isError := recovered.(error)
        if false == isError {
            t.Fatalf("expected the panic value to be an error, got %T", recovered)
        }

        if false == errors.Is(recoveredErr, ErrUnsupportedRunnerEntry) {
            t.Fatalf("expected ErrUnsupportedRunnerEntry, got %v", recoveredErr)
        }
    }()

    job := newRecordingCommand("job:custom")

    configuration := NewConfiguration().
        Schedule("job:custom", &EntryConfig{
            Schedule: &Schedule{Minute: "0"},
            Command:  []string{"/usr/local/bin/backup", "--fast"},
        })

    NewRunnerCommand(configuration, RunnerDialectCrontab, job)
}

func TestRunnerCommand_MultiInstanceEntryPanicsAtConstruction(t *testing.T) {
    defer func() {
        recovered := recover()
        if nil == recovered {
            t.Fatal("expected a panic for an entry with more than one instance")
        }

        recoveredErr, isError := recovered.(error)
        if false == isError {
            t.Fatalf("expected the panic value to be an error, got %T", recovered)
        }

        if false == errors.Is(recoveredErr, ErrUnsupportedRunnerEntry) {
            t.Fatalf("expected ErrUnsupportedRunnerEntry, got %v", recoveredErr)
        }
    }()

    job := newRecordingCommand("job:top")

    configuration := NewConfiguration().
        Schedule("job:top", &EntryConfig{
            Schedule:  &Schedule{Minute: "0"},
            Instances: 3,
        })

    NewRunnerCommand(configuration, RunnerDialectCrontab, job)
}

func TestRunnerCommand_UnknownDialectPanicsAtConstruction(t *testing.T) {
    defer func() {
        recovered := recover()
        if nil == recovered {
            t.Fatal("expected a panic for an unknown runner dialect")
        }

        recoveredErr, isError := recovered.(error)
        if false == isError {
            t.Fatalf("expected the panic value to be an error, got %T", recovered)
        }

        if false == errors.Is(recoveredErr, ErrUnknownRunnerDialect) {
            t.Fatalf("expected ErrUnknownRunnerDialect, got %v", recoveredErr)
        }
    }()

    job := newRecordingCommand("job:top")

    configuration := NewConfiguration().
        Schedule("job:top", &EntryConfig{Schedule: &Schedule{Minute: "0"}})

    NewRunnerCommand(configuration, RunnerDialect("solaris"), job)
}

/** @info the schedule steps the day of month across the odd days and pins Monday; 2026-07-20 is an even-numbered Monday, so it is due only under the kubernetes dialect's or rule, proving the configured dialect reaches every entry's matcher on the runDue path the --once flag uses. The zero-value dialect is asserted against the named crontab constant through the same entries. */
func TestRunnerCommand_DialectReachesTheEntryMatchers(t *testing.T) {
    at := time.Date(2026, time.July, 20, 0, 0, 0, 0, time.UTC)
    if time.Monday != at.Weekday() {
        t.Fatalf("expected 2026-07-20 to be a Monday, got a %s", at.Weekday())
    }

    cases := []struct {
        name             string
        dialect          RunnerDialect
        expectedRunCount int
    }{
        {name: "zero-value default keeps the crontab and rule", dialect: "", expectedRunCount: 0},
        {name: "named crontab dialect keeps the and rule", dialect: RunnerDialectCrontab, expectedRunCount: 0},
        {name: "kubernetes dialect applies the or rule", dialect: RunnerDialectKubernetes, expectedRunCount: 1},
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            job := newRecordingCommand("job:odd-mondays")

            configuration := NewConfiguration().
                Schedule("job:odd-mondays", &EntryConfig{
                    Schedule: &Schedule{Minute: "0", Hour: "0", DayOfMonth: "*/2", DayOfWeek: "1"},
                })

            runner := NewRunnerCommand(configuration, testCase.dialect, job)

            if runErr := runner.runDue(newRunnerTestRuntime(context.Background()), at); nil != runErr {
                t.Fatalf("unexpected error: %v", runErr)
            }

            if testCase.expectedRunCount != job.runCount {
                t.Fatalf("expected the command to run %d times under the %q dialect, ran %d", testCase.expectedRunCount, testCase.dialect, job.runCount)
            }
        })
    }
}

/** @info drives reconcileWallClock through every absolute minute of one local span, the way the loop wakes, and counts how often each entry class fires; the fixed-time matcher pins 03:30 so daylight-saving days prove the once-and-only-once property. */
func driveReconciledSpan(
    t *testing.T,
    spanStart time.Time,
    spanEnd time.Time,
    fixedTimeMatcher *scheduleMatcher,
    wildcardMatcher *scheduleMatcher,
) (int, int) {
    t.Helper()

    previousTarget := spanStart
    fixedTimeRunCount := 0
    wildcardRunCount := 0

    for current := spanStart.Add(time.Minute); false == current.After(spanEnd); current = current.Add(time.Minute) {
        evaluations, nextTarget, note := reconcileWallClock(previousTarget, current)
        if "" != note {
            t.Fatalf("unexpected reconciliation note at %v: %s", current, note)
        }

        previousTarget = nextTarget

        for _, evaluation := range evaluations {
            if true == evaluation.runFixedTime && true == fixedTimeMatcher.Matches(evaluation.at) {
                fixedTimeRunCount++
            }

            if true == evaluation.runWildcard && true == wildcardMatcher.Matches(evaluation.at) {
                wildcardRunCount++
            }
        }
    }

    return fixedTimeRunCount, wildcardRunCount
}

func newReconcileTestMatchers(t *testing.T) (*scheduleMatcher, *scheduleMatcher) {
    t.Helper()

    fixedTimeMatcher, fixedTimeErr := newScheduleMatcher(&Schedule{Minute: "30", Hour: "3"}, RunnerDialectCrontab)
    if nil != fixedTimeErr {
        t.Fatalf("unexpected fixed-time matcher error: %v", fixedTimeErr)
    }

    wildcardMatcher, wildcardErr := newScheduleMatcher(nil, RunnerDialectCrontab)
    if nil != wildcardErr {
        t.Fatalf("unexpected wildcard matcher error: %v", wildcardErr)
    }

    return fixedTimeMatcher, wildcardMatcher
}

func TestReconcileWallClock_OneMinuteAdvanceEvaluatesBothClasses(t *testing.T) {
    previousTarget := time.Date(2026, time.July, 15, 9, 29, 0, 0, time.UTC)
    current := time.Date(2026, time.July, 15, 9, 30, 0, 500_000_000, time.UTC)

    evaluations, nextTarget, note := reconcileWallClock(previousTarget, current)

    if "" != note {
        t.Fatalf("unexpected note: %s", note)
    }

    if 1 != len(evaluations) {
        t.Fatalf("expected one evaluation, got %d", len(evaluations))
    }

    if false == evaluations[0].runFixedTime || false == evaluations[0].runWildcard {
        t.Fatalf("expected both classes to run, got %+v", evaluations[0])
    }

    if 30 != evaluations[0].at.Minute() || 9 != evaluations[0].at.Hour() {
        t.Fatalf("expected the evaluation to render 09:30, got %v", evaluations[0].at)
    }

    if wallMinuteIndex(nextTarget) != wallMinuteIndex(current.Truncate(time.Minute)) {
        t.Fatalf("expected the chain to advance to the current minute, got %v", nextTarget)
    }
}

func TestReconcileWallClock_SmallForwardJumpCatchesUpFixedTimeMinutesOnce(t *testing.T) {
    previousTarget := time.Date(2026, time.July, 15, 9, 0, 0, 0, time.UTC)
    current := time.Date(2026, time.July, 15, 9, 11, 0, 0, time.UTC)

    evaluations, nextTarget, note := reconcileWallClock(previousTarget, current)

    if "" != note {
        t.Fatalf("unexpected note: %s", note)
    }

    wildcardMinutes := make([]int, 0)
    fixedTimeMinutes := make([]int, 0)

    for _, evaluation := range evaluations {
        if true == evaluation.runWildcard {
            wildcardMinutes = append(wildcardMinutes, evaluation.at.Minute())
        }

        if true == evaluation.runFixedTime {
            fixedTimeMinutes = append(fixedTimeMinutes, evaluation.at.Minute())
        }
    }

    if 1 != len(wildcardMinutes) || 11 != wildcardMinutes[0] {
        t.Fatalf("expected wildcard entries to resume at the current minute only, got %v", wildcardMinutes)
    }

    if 11 != len(fixedTimeMinutes) {
        t.Fatalf("expected fixed-time entries to catch up on each skipped minute plus the current one, got %v", fixedTimeMinutes)
    }

    for offset, minuteValue := range fixedTimeMinutes {
        if offset+1 != minuteValue {
            t.Fatalf("expected the fixed-time catch-up to walk minutes 1..11 in order, got %v", fixedTimeMinutes)
        }
    }

    if wallMinuteIndex(nextTarget) != wallMinuteIndex(current) {
        t.Fatalf("expected the chain to re-anchor at the current minute, got %v", nextTarget)
    }
}

func TestReconcileWallClock_BackwardJumpRunsWildcardOnlyAndKeepsTheAnchor(t *testing.T) {
    previousTarget := time.Date(2026, time.July, 15, 9, 30, 0, 0, time.UTC)

    for _, current := range []time.Time{
        time.Date(2026, time.July, 15, 9, 15, 0, 0, time.UTC),
        time.Date(2026, time.July, 15, 9, 30, 30, 0, time.UTC),
    } {
        evaluations, nextTarget, note := reconcileWallClock(previousTarget, current)

        if "" != note {
            t.Fatalf("unexpected note: %s", note)
        }

        if 1 != len(evaluations) {
            t.Fatalf("expected one evaluation, got %d", len(evaluations))
        }

        if true == evaluations[0].runFixedTime {
            t.Fatalf("expected fixed-time entries to stay suppressed on a backward jump, got %+v", evaluations[0])
        }

        if false == evaluations[0].runWildcard {
            t.Fatalf("expected wildcard entries to keep running on a backward jump, got %+v", evaluations[0])
        }

        if evaluations[0].at.Minute() != current.Minute() {
            t.Fatalf("expected the wildcard evaluation at the current minute, got %v", evaluations[0].at)
        }

        if false == nextTarget.Equal(previousTarget) {
            t.Fatalf("expected the anchor to stay in place, got %v", nextTarget)
        }
    }
}

func TestReconcileWallClock_LargeJumpReanchorsWithoutCatchUp(t *testing.T) {
    previousTarget := time.Date(2026, time.July, 15, 9, 0, 0, 0, time.UTC)

    for _, current := range []time.Time{
        time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC),
        time.Date(2026, time.July, 15, 14, 30, 0, 0, time.UTC),
        time.Date(2026, time.July, 15, 6, 0, 0, 0, time.UTC),
        time.Date(2026, time.July, 15, 3, 30, 0, 0, time.UTC),
    } {
        evaluations, nextTarget, note := reconcileWallClock(previousTarget, current)

        if "" == note {
            t.Fatalf("expected a log note for the clock reset at %v", current)
        }

        if 1 != len(evaluations) {
            t.Fatalf("expected the reset to evaluate only the current minute, got %d evaluations", len(evaluations))
        }

        if false == evaluations[0].runFixedTime || false == evaluations[0].runWildcard {
            t.Fatalf("expected both classes to resume at the current minute, got %+v", evaluations[0])
        }

        if wallMinuteIndex(nextTarget) != wallMinuteIndex(current) {
            t.Fatalf("expected the chain to re-anchor at the current minute, got %v", nextTarget)
        }
    }
}

/** @info Europe/Bucharest 2026-03-29: 03:00 EET jumps to 04:00 EEST, so the 03:00-03:59 wall minutes never exist; the catch-up must still evaluate them once for fixed-time entries while wildcard entries fire once per absolute minute (a 23-hour day). */
func TestReconcileWallClock_SpringForwardRunsAFixedTimeEntryExactlyOnce(t *testing.T) {
    bucharest, locationErr := time.LoadLocation("Europe/Bucharest")
    if nil != locationErr {
        t.Fatalf("unexpected location error: %v", locationErr)
    }

    fixedTimeMatcher, wildcardMatcher := newReconcileTestMatchers(t)

    spanStart := time.Date(2026, time.March, 29, 0, 0, 0, 0, bucharest)
    spanEnd := time.Date(2026, time.March, 30, 0, 0, 0, 0, bucharest)

    absoluteMinutes := int(spanEnd.Sub(spanStart) / time.Minute)
    if 1380 != absoluteMinutes {
        t.Fatalf("expected the spring-forward day to span 1380 absolute minutes, got %d", absoluteMinutes)
    }

    fixedTimeRunCount, wildcardRunCount := driveReconciledSpan(t, spanStart, spanEnd, fixedTimeMatcher, wildcardMatcher)

    if 1 != fixedTimeRunCount {
        t.Fatalf("expected the 30 3 * * * entry to run exactly once across the spring-forward day, ran %d", fixedTimeRunCount)
    }

    if absoluteMinutes != wildcardRunCount {
        t.Fatalf("expected the wildcard entry to run once per absolute minute (%d), ran %d", absoluteMinutes, wildcardRunCount)
    }
}

/** @info Europe/Bucharest 2026-10-25: 04:00 EEST falls back to 03:00 EET, so the 03:00-03:59 wall minutes repeat; fixed-time entries must stay suppressed on the repeat while wildcard entries fire once per absolute minute (a 25-hour day, so 60 wall doubles). */
func TestReconcileWallClock_FallBackRunsAFixedTimeEntryExactlyOnce(t *testing.T) {
    bucharest, locationErr := time.LoadLocation("Europe/Bucharest")
    if nil != locationErr {
        t.Fatalf("unexpected location error: %v", locationErr)
    }

    fixedTimeMatcher, wildcardMatcher := newReconcileTestMatchers(t)

    spanStart := time.Date(2026, time.October, 25, 0, 0, 0, 0, bucharest)
    spanEnd := time.Date(2026, time.October, 26, 0, 0, 0, 0, bucharest)

    absoluteMinutes := int(spanEnd.Sub(spanStart) / time.Minute)
    if 1500 != absoluteMinutes {
        t.Fatalf("expected the fall-back day to span 1500 absolute minutes, got %d", absoluteMinutes)
    }

    fixedTimeRunCount, wildcardRunCount := driveReconciledSpan(t, spanStart, spanEnd, fixedTimeMatcher, wildcardMatcher)

    if 1 != fixedTimeRunCount {
        t.Fatalf("expected the 30 3 * * * entry to run exactly once across the fall-back day, ran %d", fixedTimeRunCount)
    }

    if absoluteMinutes != wildcardRunCount {
        t.Fatalf("expected the wildcard entry to run once per absolute minute (%d), ran %d", absoluteMinutes, wildcardRunCount)
    }
}

/** @info a plain day is the control: every class fires its natural count, proving the chain neither skips nor doubles when the clock behaves. */
func TestReconcileWallClock_PlainDayRunsEveryMinuteExactlyOnce(t *testing.T) {
    bucharest, locationErr := time.LoadLocation("Europe/Bucharest")
    if nil != locationErr {
        t.Fatalf("unexpected location error: %v", locationErr)
    }

    fixedTimeMatcher, wildcardMatcher := newReconcileTestMatchers(t)

    spanStart := time.Date(2026, time.July, 15, 0, 0, 0, 0, bucharest)
    spanEnd := time.Date(2026, time.July, 16, 0, 0, 0, 0, bucharest)

    fixedTimeRunCount, wildcardRunCount := driveReconciledSpan(t, spanStart, spanEnd, fixedTimeMatcher, wildcardMatcher)

    if 1 != fixedTimeRunCount {
        t.Fatalf("expected the 30 3 * * * entry to run exactly once on a plain day, ran %d", fixedTimeRunCount)
    }

    if 1440 != wildcardRunCount {
        t.Fatalf("expected the wildcard entry to run 1440 times on a plain day, ran %d", wildcardRunCount)
    }
}

type exitCoderCommand struct {
    commandName string
}

func (instance *exitCoderCommand) Name() string {
    return instance.commandName
}

func (instance *exitCoderCommand) Description() string {
    return "exit coder command"
}

func (instance *exitCoderCommand) Flags() []clicontract.Flag {
    return nil
}

func (instance *exitCoderCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext *clicontract.CommandContext) error {
    return exception.NewExitError(3, exception.NewError("the job failed with an exit code", nil, nil))
}

/** @info the returned error carries an exit code, which the cli library's default handler turns into os.Exit; the runner must return it as a plain failure instead — a red run here does not merely fail, it kills the whole test process. */
func TestRunnerCommand_ExitCoderErrorIsReturnedInsteadOfExitingTheScheduler(t *testing.T) {
    exiting := &exitCoderCommand{commandName: "job:exiting"}
    healthy := newRecordingCommand("job:healthy")

    configuration := NewConfiguration().
        Schedule("job:exiting", &EntryConfig{Schedule: &Schedule{Minute: "0"}}).
        Schedule("job:healthy", &EntryConfig{Schedule: &Schedule{Minute: "0"}})

    runner := NewRunnerCommand(configuration, RunnerDialectCrontab, exiting, healthy)

    at := time.Date(2026, time.July, 15, 9, 0, 0, 0, time.UTC)
    runErr := runner.runDue(newRunnerTestRuntime(context.Background()), at)
    if nil == runErr {
        t.Fatal("expected the exit-coded failure to surface as an aggregate error")
    }

    aggregate, isExceptionError := runErr.(*exception.Error)
    if false == isExceptionError {
        t.Fatalf("expected an exception error from runDue, got %T", runErr)
    }

    if false == strings.Contains(fmt.Sprintf("%v", aggregate.Context()["commands"]), "job:exiting") {
        t.Fatalf("expected the exit-coded command to be aggregated as failed, got %v", aggregate.Context()["commands"])
    }

    if 1 != healthy.runCount {
        t.Fatalf("expected the healthy command to still run, ran %d", healthy.runCount)
    }
}

func TestRunnerCommand_DuplicateCommandNamePanicsAtConstruction(t *testing.T) {
    defer func() {
        recovered := recover()
        if nil == recovered {
            t.Fatal("expected a panic for two runner commands sharing one name")
        }

        recoveredErr, isError := recovered.(error)
        if false == isError {
            t.Fatalf("expected the panic value to be an error, got %T", recovered)
        }

        if false == errors.Is(recoveredErr, ErrDuplicateRunnerCommand) {
            t.Fatalf("expected ErrDuplicateRunnerCommand, got %v", recoveredErr)
        }
    }()

    configuration := NewConfiguration().
        Schedule("job:top", &EntryConfig{Schedule: &Schedule{Minute: "0"}})

    NewRunnerCommand(configuration, RunnerDialectCrontab, newRecordingCommand("job:top"), newRecordingCommand("job:top"))
}

type memoizedFlagsCommand struct {
    commandName string
    flags       []clicontract.Flag
}

func newMemoizedFlagsCommand(name string) *memoizedFlagsCommand {
    return &memoizedFlagsCommand{
        commandName: name,
        flags: []clicontract.Flag{
            &clicontract.IntFlag{Name: probeFlagNameBatchSize, Value: 100},
        },
    }
}

func (instance *memoizedFlagsCommand) Name() string {
    return instance.commandName
}

func (instance *memoizedFlagsCommand) Description() string {
    return "memoized flags command"
}

func (instance *memoizedFlagsCommand) Flags() []clicontract.Flag {
    return instance.flags
}

func (instance *memoizedFlagsCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext *clicontract.CommandContext) error {
    return nil
}

/** @info the cli library writes parse state into the flag instances, so a command handing the runner the same instances on every Flags() call would make overlapping invocations race on them; the wiring error surfaces at construction. */
func TestRunnerCommand_SharedFlagInstancesPanicAtConstruction(t *testing.T) {
    defer func() {
        recovered := recover()
        if nil == recovered {
            t.Fatal("expected a panic for a command returning shared flag instances")
        }

        recoveredErr, isError := recovered.(error)
        if false == isError {
            t.Fatalf("expected the panic value to be an error, got %T", recovered)
        }

        if false == errors.Is(recoveredErr, ErrSharedRunnerCommandFlags) {
            t.Fatalf("expected ErrSharedRunnerCommandFlags, got %v", recoveredErr)
        }
    }()

    configuration := NewConfiguration().
        Schedule("job:memoized", &EntryConfig{Schedule: &Schedule{Minute: "0"}})

    NewRunnerCommand(configuration, RunnerDialectCrontab, newMemoizedFlagsCommand("job:memoized"))
}

/** @info an entry naming a system user stays runnable in-process — the one Configuration keeps driving both the generated manifests and the runner — and the runner records the affected command for the warning Run logs. */
func TestRunnerCommand_UserEntryIsAcceptedAndRecordedForTheWarning(t *testing.T) {
    job := newRecordingCommand("job:user")

    configuration := NewConfiguration().
        Schedule("job:user", &EntryConfig{Schedule: &Schedule{Minute: "0"}, User: "app"})

    runner := NewRunnerCommand(configuration, RunnerDialectCrontab, job)

    if 1 != len(runner.userIgnoredCommands) || "job:user" != runner.userIgnoredCommands[0] {
        t.Fatalf("expected the user-carrying entry to be recorded for the warning, got %v", runner.userIgnoredCommands)
    }

    at := time.Date(2026, time.July, 15, 9, 0, 0, 0, time.UTC)
    if runErr := runner.runDue(newRunnerTestRuntime(context.Background()), at); nil != runErr {
        t.Fatalf("unexpected error: %v", runErr)
    }

    if 1 != job.runCount {
        t.Fatalf("expected the user-carrying entry to run as the process user, ran %d", job.runCount)
    }
}

/** @info the fake clock returns an instant just before a minute boundary once, for both the chain anchor and the first arming; a second read after the boundary would manufacture a two-minute jump on which a wildcard entry pinned to the boundary minute never fires. */
func TestRunnerCommand_LoopAnchorsAndArmsFromOneClockRead(t *testing.T) {
    ran := make(chan struct{}, 1)
    job := &signalingCommand{commandName: "job:boundary", ran: ran}

    configuration := NewConfiguration().
        Schedule("job:boundary", &EntryConfig{Schedule: &Schedule{Minute: "30"}})

    runner := NewRunnerCommand(configuration, RunnerDialectCrontab, job)

    boundaryMinute := time.Date(2026, time.July, 15, 9, 30, 0, 0, time.UTC)

    var nowCallCount atomic.Int32
    runner.now = func() time.Time {
        if 1 == nowCallCount.Add(1) {
            return boundaryMinute.Add(-10 * time.Millisecond)
        }

        return boundaryMinute.Add(100 * time.Millisecond)
    }

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    finished := make(chan error, 1)
    go func() {
        finished <- runner.runLoop(newRunnerTestRuntime(ctx))
    }()

    select {
    case <-ran:
    case <-time.After(2 * time.Second):
        t.Fatal("expected the first wake to evaluate the boundary minute the anchor was derived from")
    }

    cancel()

    select {
    case <-finished:
    case <-time.After(3 * time.Second):
        t.Fatal("runner loop ignored the cancelled context")
    }
}

type countingCommand struct {
    commandName string
    runCount    atomic.Int32
    ran         chan struct{}
}

func (instance *countingCommand) Name() string {
    return instance.commandName
}

func (instance *countingCommand) Description() string {
    return "counting command"
}

func (instance *countingCommand) Flags() []clicontract.Flag {
    return nil
}

func (instance *countingCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext *clicontract.CommandContext) error {
    instance.runCount.Add(1)

    select {
    case instance.ran <- struct{}{}:
    default:
    }

    return nil
}

/** @info a backward wall step inside the armed window makes the loop arm a second time for the minute it just dispatched (the evaluation is pinned to the armed minute, so the step does not move it): the fake clock wakes for 10:00 from 09:59:59.900, steps back to 09:59:59.890, and the re-arm renders 10:00 again. The wildcard entry must run once for that minute, not twice seconds apart — the repeated wall minute of a fall-back is the other case, and a whole hour of other minutes runs in between there. */
func TestRunnerCommand_LoopDoesNotRedispatchTheMinuteItJustDispatched(t *testing.T) {
    ran := make(chan struct{}, 8)
    job := &countingCommand{commandName: "job:every-minute", ran: ran}

    configuration := NewConfiguration().
        Schedule("job:every-minute", &EntryConfig{})

    runner := NewRunnerCommand(configuration, RunnerDialectCrontab, job)

    boundary := time.Date(2026, time.July, 15, 10, 0, 0, 0, time.UTC)

    var nowCallCount atomic.Int32
    runner.now = func() time.Time {
        switch nowCallCount.Add(1) {
        case 1:
            return boundary.Add(-100 * time.Millisecond)
        case 2:
            /* the wall clock stepped back inside the armed window, so the re-arm targets the same minute again */
            return boundary.Add(-110 * time.Millisecond)
        }

        return boundary.Add(50 * time.Millisecond)
    }

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    finished := make(chan error, 1)
    go func() {
        finished <- runner.runLoop(newRunnerTestRuntime(ctx))
    }()

    select {
    case <-ran:
    case <-time.After(3 * time.Second):
        t.Fatal("expected the wildcard entry to run for the armed minute")
    }

    select {
    case <-ran:
        t.Fatal("expected the re-armed wake at the already-dispatched minute to be skipped, not to run the entry a second time")
    case <-time.After(500 * time.Millisecond):
    }

    cancel()

    select {
    case <-finished:
    case <-time.After(3 * time.Second):
        t.Fatal("runner loop ignored the cancelled context")
    }

    if 1 != job.runCount.Load() {
        t.Fatalf("expected the wildcard entry to run exactly once for the repeated wall minute, ran %d", job.runCount.Load())
    }
}

/** @info drives the real runner loop across the Europe/Bucharest 2026-10-25 fall-back: the first wake fires the 03:30 fixed-time entry on the first pass of the repeated hour, the second wake lands on the repeat (04:00 EEST renders as 03:00 EET, a backward jump), and the third wake re-reaches the pinned 03:30 on the repeat — the dispatch class filter must keep the fixed-time entry suppressed there while the wildcard entry follows every wake. */
func TestRunnerCommand_LoopSuppressesAFixedTimeEntryAcrossTheFallBackRepeat(t *testing.T) {
    bucharest, locationErr := time.LoadLocation("Europe/Bucharest")
    if nil != locationErr {
        t.Fatalf("unexpected location error: %v", locationErr)
    }

    fixedRan := make(chan struct{}, 16)
    fixedJob := &countingCommand{commandName: "job:fixed", ran: fixedRan}

    wildcardRan := make(chan struct{}, 16)
    wildcardJob := &countingCommand{commandName: "job:wildcard", ran: wildcardRan}

    configuration := NewConfiguration().
        Schedule("job:fixed", &EntryConfig{Schedule: &Schedule{Minute: "30", Hour: "3"}}).
        Schedule("job:wildcard", &EntryConfig{})

    runner := NewRunnerCommand(configuration, RunnerDialectCrontab, fixedJob, wildcardJob)

    /* 03:30 EEST, then 30 absolute minutes later 04:00 EEST (which renders as 03:00 EET), then 30 more to the repeated 03:30 EET — each approached 10ms before the boundary; after the third wake the clock rests just past 03:30 EET so the fourth arming waits a full minute and cancellation wins. The instant is built from the unambiguous 02:30 EEST, since 03:30 renders twice on this day. */
    firstPass := time.Date(2026, time.October, 25, 2, 30, 0, 0, bucharest).Add(time.Hour)
    if 3 != firstPass.Hour() || 30 != firstPass.Minute() {
        t.Fatalf("expected the first pass to render 03:30 EEST, got %v", firstPass)
    }
    wakes := []time.Time{
        firstPass.Add(-10 * time.Millisecond),
        firstPass.Add(30*time.Minute - 10*time.Millisecond),
        firstPass.Add(60*time.Minute - 10*time.Millisecond),
    }

    var nowCallCount atomic.Int32
    runner.now = func() time.Time {
        callIndex := int(nowCallCount.Add(1))
        if callIndex <= len(wakes) {
            return wakes[callIndex-1]
        }

        return firstPass.Add(60*time.Minute + 100*time.Millisecond)
    }

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    finished := make(chan error, 1)
    go func() {
        finished <- runner.runLoop(newRunnerTestRuntime(ctx))
    }()

    for wildcardWake := 0; wildcardWake < 3; wildcardWake++ {
        select {
        case <-wildcardRan:
        case <-time.After(3 * time.Second):
            t.Fatalf("expected the wildcard entry to follow wake %d", wildcardWake+1)
        }
    }

    cancel()

    select {
    case <-finished:
    case <-time.After(3 * time.Second):
        t.Fatal("runner loop ignored the cancelled context")
    }

    if 1 != fixedJob.runCount.Load() {
        t.Fatalf("expected the 03:30 fixed-time entry to run exactly once across the fall-back repeat, ran %d", fixedJob.runCount.Load())
    }

    if 3 != wildcardJob.runCount.Load() {
        t.Fatalf("expected the wildcard entry to run on every wake, ran %d", wildcardJob.runCount.Load())
    }
}

func TestRunnerCommand_LoopStopsOnContextCancellation(t *testing.T) {
    job := newRecordingCommand("job:top")

    configuration := NewConfiguration().
        Schedule("job:top", &EntryConfig{Schedule: &Schedule{Minute: "0"}})

    runner := NewRunnerCommand(configuration, RunnerDialectCrontab, job)

    ctx, cancel := context.WithCancel(context.Background())
    cancel()

    finished := make(chan error, 1)
    go func() {
        finished <- runner.Run(newRunnerTestRuntime(ctx), &urfavecli.Command{Name: runner.Name()})
    }()

    select {
    case runErr := <-finished:
        if nil != runErr {
            t.Fatalf("expected a clean nil on cancellation, got %v", runErr)
        }
    case <-time.After(3 * time.Second):
        t.Fatal("runner loop ignored the cancelled context")
    }
}
