package cron

import (
    "bytes"
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "math"
    "strings"
    "sync"
    "sync/atomic"
    "testing"
    "time"
    "unicode/utf8"

    "github.com/precision-soft/melody/v3/application"
    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/cli/output"
    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
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

func (instance *recordingCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error {
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

func (instance *panickingCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error {
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

func (instance *flagDefaultProbeCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error {
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

func (instance *writerProbeCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error {
    instance.runCount++

    _, writeErr := fmt.Fprintln(commandContext.Writer(), "writer probe output")

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

func (instance *argsProbeCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error {
    instance.runCount++
    instance.observedArgsLength = len(commandContext.Arguments())

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

func (instance *signalingCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error {
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

func (instance *rendezvousCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error {
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
    /* completionDelay holds the job inside Run after its context is cancelled, so a loop that returns without waiting for its in-flight jobs is observed returning FIRST rather than winning a race it usually loses */
    completionDelay time.Duration
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

func (instance *blockingCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error {
    select {
    case instance.started <- struct{}{}:
    default:
    }

    <-runtimeInstance.Context().Done()

    if 0 < instance.completionDelay {
        time.Sleep(instance.completionDelay)
    }

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

    cliCommand := newRunnerDispatch(runner, nil, nil)

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

/* the fake clock returns an instant just before the targeted minute while the loop anchors the chain and arms the timer, then a stepped-back instant for every later read; only an evaluation pinned to the armed minute still fires the schedule, and the stepped-back reads may only influence the arming of later wakes. */
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

/* each command releases only when its peer has also started, so the tick passes solely when the due entries run concurrently, like crontab starting an independent process per entry. */
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

/* the job blocks until the runtime context is cancelled, so a second start signal while no run has completed proves the loop armed and fired the next minute without waiting on the running job — and that an entry may overlap itself. */
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

/* the job holds itself inside Run for completionDelay AFTER the cancellation reaches its child context, so the two events are ordered rather than raced: a loop that abandons its in-flight jobs returns while the job is still sleeping, and the assertion below sees completedCount==0 every time. Without that delay both goroutines wake on the same cancel() and the job wins by luck, so the test passed even with inFlight.Wait() removed. */
func TestRunnerCommand_LoopWaitsForInFlightJobsOnCancellation(t *testing.T) {
    started := make(chan struct{}, 1)
    job := &blockingCommand{commandName: "job:blocking", started: started, completionDelay: 250 * time.Millisecond}

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

    _, invokeErr := runner.invoke(runtimeInstance, runner.entries[0])
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

/* the schedule steps the day of month across the odd days and pins Monday; 2026-07-20 is an even-numbered Monday, so it is due only under the kubernetes dialect's or rule, proving the configured dialect reaches every entry's matcher on the runDue path the --once flag uses. The zero-value dialect is asserted against the named crontab constant through the same entries. */
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

/* drives reconcileWallClock through every absolute minute of one local span, the way the loop wakes, and counts how often each entry class fires; the fixed-time matcher pins 03:30 so daylight-saving days prove the once-and-only-once property. */
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

/* Europe/Bucharest 2026-03-29: 03:00 EET jumps to 04:00 EEST, so the 03:00-03:59 wall minutes never exist; the catch-up must still evaluate them once for fixed-time entries while wildcard entries fire once per absolute minute (a 23-hour day). */
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

/* Europe/Bucharest 2026-10-25: 04:00 EEST falls back to 03:00 EET, so the 03:00-03:59 wall minutes repeat; fixed-time entries must stay suppressed on the repeat while wildcard entries fire once per absolute minute (a 25-hour day, so 60 wall doubles). */
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

func (instance *exitCoderCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error {
    return exception.NewExitError(3, exception.NewError("the job failed with an exit code", nil, nil))
}

/* the returned error carries an exit code, which the cli library's default handler turns into os.Exit; the runner must return it as a plain failure instead — a red run here does not merely fail, it kills the whole test process. */
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

/* an entry naming a system user stays runnable in-process — the one Configuration keeps driving both the generated manifests and the runner — and the runner records the affected command for the warning Run logs. */
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

/* the fake clock returns an instant just before a minute boundary once, for both the chain anchor and the first arming; a second read after the boundary would manufacture a two-minute jump on which a wildcard entry pinned to the boundary minute never fires. */
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

func (instance *countingCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error {
    instance.runCount.Add(1)

    select {
    case instance.ran <- struct{}{}:
    default:
    }

    return nil
}

/* a backward wall step inside the armed window makes the loop arm a second time for the minute it just dispatched (the evaluation is pinned to the armed minute, so the step does not move it): the fake clock wakes for 10:00 from 09:59:59.900, steps back to 09:59:59.890, and the re-arm renders 10:00 again. The wildcard entry must run once for that minute, not twice seconds apart — the repeated wall minute of a fall-back is the other case, and a whole hour of other minutes runs in between there. */
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

/* drives the real runner loop across the Europe/Bucharest 2026-10-25 fall-back: the first wake fires the 03:30 fixed-time entry on the first pass of the repeated hour, the second wake lands on the repeat (04:00 EEST renders as 03:00 EET, a backward jump), and the third wake re-reaches the pinned 03:30 on the repeat — the dispatch class filter must keep the fixed-time entry suppressed there while the wildcard entry follows every wake. */
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
        finished <- runner.Run(newRunnerTestRuntime(ctx), &clicontract.StaticContext{})
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

/* contextWatchingCommand returns the moment its context is cancelled, the way a command written against the runtime context behaves when its deadline fires. */
type contextWatchingCommand struct {
    commandName string
    started     chan struct{}
}

func newContextWatchingCommand(name string) *contextWatchingCommand {
    return &contextWatchingCommand{commandName: name, started: make(chan struct{}, 1)}
}

func (instance *contextWatchingCommand) Name() string {
    return instance.commandName
}

func (instance *contextWatchingCommand) Description() string {
    return "context watching command"
}

func (instance *contextWatchingCommand) Flags() []clicontract.Flag {
    return nil
}

func (instance *contextWatchingCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error {
    /* a non-blocking mark rather than a close, so the command survives being dispatched more than once */
    select {
    case instance.started <- struct{}{}:
    default:
    }

    <-runtimeInstance.Context().Done()

    return runtimeInstance.Context().Err()
}

/* wedgedCommand never looks at its context, the way a command blocked on a deadline-less network read behaves; only the test lets it go. */
type wedgedCommand struct {
    commandName string
    started     chan struct{}
    release     chan struct{}
}

func newWedgedCommand(name string) *wedgedCommand {
    return &wedgedCommand{commandName: name, started: make(chan struct{}), release: make(chan struct{})}
}

func (instance *wedgedCommand) Name() string {
    return instance.commandName
}

func (instance *wedgedCommand) Description() string {
    return "wedged command"
}

func (instance *wedgedCommand) Flags() []clicontract.Flag {
    return nil
}

func (instance *wedgedCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error {
    close(instance.started)

    <-instance.release

    return nil
}

/* sleepingCommand runs for a fixed span without watching its context, so an entry that opted out of the deadline can be shown to still run to completion. */
type sleepingCommand struct {
    commandName string
    duration    time.Duration
    completed   atomic.Int64
}

func (instance *sleepingCommand) Name() string {
    return instance.commandName
}

func (instance *sleepingCommand) Description() string {
    return "sleeping command"
}

func (instance *sleepingCommand) Flags() []clicontract.Flag {
    return nil
}

func (instance *sleepingCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error {
    time.Sleep(instance.duration)

    instance.completed.Add(1)

    return nil
}

type closeCountingScope struct {
    containercontract.Scope
    closed *atomic.Int64
}

func (instance closeCountingScope) Close() error {
    instance.closed.Add(1)

    return instance.Scope.Close()
}

type closeCountingContainer struct {
    containercontract.Container
    closed *atomic.Int64
}

func (instance closeCountingContainer) NewScope() containercontract.Scope {
    return closeCountingScope{Scope: instance.Container.NewScope(), closed: instance.closed}
}

/* an entry inherits the runner default and overrides it with EntryConfig.Timeout; a negative value is carried through untouched as the opt-out. */
func TestRunnerCommand_EntryTimeoutDefaultsAndOverrides(t *testing.T) {
    configuration := NewConfiguration().
        Schedule("job:default", &EntryConfig{Schedule: &Schedule{Minute: "0"}}).
        Schedule("job:custom", &EntryConfig{Schedule: &Schedule{Minute: "0"}, Timeout: 90 * time.Second}).
        Schedule("job:unbounded", &EntryConfig{Schedule: &Schedule{Minute: "0"}, Timeout: -1})

    runner := NewRunnerCommand(
        configuration,
        RunnerDialectCrontab,
        newRecordingCommand("job:default"),
        newRecordingCommand("job:custom"),
        newRecordingCommand("job:unbounded"),
    )

    expected := []time.Duration{defaultCommandTimeout, 90 * time.Second, -1}
    for index, want := range expected {
        if want != runner.entries[index].timeout {
            t.Fatalf("entry %d: expected timeout %v, got %v", index, want, runner.entries[index].timeout)
        }
    }
}

/* a command that watches its context is cancelled by the deadline and unwinds on its own; the failure must reach the caller naming the timeout rather than being reported as a plain command error. */
func TestRunnerCommand_TimeoutCancelsACommandThatWatchesItsContext(t *testing.T) {
    job := newContextWatchingCommand("job:top")

    configuration := NewConfiguration().
        Schedule("job:top", &EntryConfig{Schedule: &Schedule{Minute: "0"}, Timeout: 50 * time.Millisecond})

    runner := NewRunnerCommand(configuration, RunnerDialectCrontab, job)
    runner.unwindGrace = 2 * time.Second

    at := time.Date(2026, time.July, 15, 9, 0, 0, 0, time.UTC)

    started := time.Now()
    _, invokeErr := runner.invoke(newRunnerTestRuntime(context.Background()), runner.entries[0])
    elapsed := time.Since(started)

    if nil == invokeErr {
        t.Fatal("expected the timed-out command to be reported")
    }

    if false == errors.Is(invokeErr, ErrCommandTimeout) {
        t.Fatalf("expected the failure to wrap ErrCommandTimeout, got %v", invokeErr)
    }

    if false == strings.Contains(invokeErr.Error(), "cancelled by its timeout") {
        t.Fatalf("expected the failure to name the timeout as the cause, got %v", invokeErr)
    }

    if elapsed > time.Second {
        t.Fatalf("expected the deadline to cut the command off promptly, took %v", elapsed)
    }

    select {
    case <-job.started:
    default:
        t.Fatal("expected the command to have run")
    }

    /* the dispatch path must surface it too, rather than dropping a command nothing waited for */
    if runDueErr := runner.runDue(newRunnerTestRuntime(context.Background()), at); nil == runDueErr {
        t.Fatal("expected the aggregated dispatch to report the timed-out command")
    }
}

/* the deadline must also hold against a command that never looks at its context — that is the shape which leaks — and the child scope must be released on that path rather than held for a goroutine that may never return. */
func TestRunnerCommand_WedgedCommandIsAbandonedAndItsScopeReleased(t *testing.T) {
    job := newWedgedCommand("job:top")
    defer close(job.release)

    configuration := NewConfiguration().
        Schedule("job:top", &EntryConfig{Schedule: &Schedule{Minute: "0"}, Timeout: 50 * time.Millisecond})

    runner := NewRunnerCommand(configuration, RunnerDialectCrontab, job)
    runner.unwindGrace = 50 * time.Millisecond

    var closedScopes atomic.Int64
    serviceContainer := container.NewContainer()
    runtimeInstance := runtime.New(
        context.Background(),
        serviceContainer.NewScope(),
        closeCountingContainer{Container: serviceContainer, closed: &closedScopes},
    )

    completed := make(chan error, 1)
    go func() {
        completed <- invokeDiscardingRunId(runner, runtimeInstance, runner.entries[0])
    }()

    var invokeErr error
    select {
    case invokeErr = <-completed:
    case <-time.After(5 * time.Second):
        t.Fatal("invoke never returned: a command that ignores its context is not bounded by the deadline")
    }

    if nil == invokeErr {
        t.Fatal("expected the abandoned command to be reported")
    }

    if false == errors.Is(invokeErr, ErrCommandTimeout) {
        t.Fatalf("expected the failure to wrap ErrCommandTimeout, got %v", invokeErr)
    }

    if false == strings.Contains(invokeErr.Error(), "abandoned") {
        t.Fatalf("expected the failure to say the command was abandoned, got %v", invokeErr)
    }

    if 1 != closedScopes.Load() {
        t.Fatalf("expected the child scope to be released on the abandon path, closed %d", closedScopes.Load())
    }

    <-job.started
}

/* an entry that opts out of the deadline keeps the pre-deadline behaviour: the runner waits for the command however long it takes. */
func TestRunnerCommand_NegativeTimeoutOptsOutOfTheDeadline(t *testing.T) {
    job := &sleepingCommand{commandName: "job:top", duration: 150 * time.Millisecond}

    configuration := NewConfiguration().
        Schedule("job:top", &EntryConfig{Schedule: &Schedule{Minute: "0"}, Timeout: -1})

    runner := NewRunnerCommand(configuration, RunnerDialectCrontab, job)
    runner.unwindGrace = time.Millisecond

    at := time.Date(2026, time.July, 15, 9, 0, 0, 0, time.UTC)
    if runErr := runner.runDue(newRunnerTestRuntime(context.Background()), at); nil != runErr {
        t.Fatalf("expected an opted-out entry to run to completion, got %v", runErr)
    }

    if 1 != job.completed.Load() {
        t.Fatalf("expected the command to complete once, completed %d", job.completed.Load())
    }
}

/* a command shorter than its deadline is unaffected: no timeout is reported and its own outcome stands. */
func TestRunnerCommand_CommandInsideItsTimeoutIsUntouched(t *testing.T) {
    job := &sleepingCommand{commandName: "job:top", duration: 10 * time.Millisecond}

    configuration := NewConfiguration().
        Schedule("job:top", &EntryConfig{Schedule: &Schedule{Minute: "0"}, Timeout: 5 * time.Second})

    runner := NewRunnerCommand(configuration, RunnerDialectCrontab, job)

    at := time.Date(2026, time.July, 15, 9, 0, 0, 0, time.UTC)
    if runErr := runner.runDue(newRunnerTestRuntime(context.Background()), at); nil != runErr {
        t.Fatalf("unexpected error: %v", runErr)
    }

    if 1 != job.completed.Load() {
        t.Fatalf("expected the command to complete once, completed %d", job.completed.Load())
    }
}

/* The deadline is opt-in. Every entry configured before it existed leaves Timeout at zero, and a default of one hour would have begun cutting a ninety-minute job short at sixty on the upgrade that introduced it — a run the entry never asked to be bounded and whose scope would then be torn down under it. An entry that wants the bound asks for it. */
func TestTimeoutOfEntry_ZeroLeavesTheRunUnbounded(t *testing.T) {
    if 0 != timeoutOfEntry(&ScheduledCommand{Config: &EntryConfig{}}) {
        t.Fatalf("expected an entry that sets no timeout to run unbounded, got %v", timeoutOfEntry(&ScheduledCommand{Config: &EntryConfig{}}))
    }

    if 0 != timeoutOfEntry(&ScheduledCommand{}) {
        t.Fatalf("expected an entry with no configuration at all to run unbounded, got %v", timeoutOfEntry(&ScheduledCommand{}))
    }

    /* a negative value is carried through rather than folded to zero, so the entry's own opt-out stays legible in the run's error context; downstream both read as unbounded, which commandContextOf is what decides */
    if 0 < timeoutOfEntry(&ScheduledCommand{Config: &EntryConfig{Timeout: -time.Second}}) {
        t.Fatalf("expected a negative timeout to leave the run unbounded, got %v", timeoutOfEntry(&ScheduledCommand{Config: &EntryConfig{Timeout: -time.Second}}))
    }

    if 90*time.Minute != timeoutOfEntry(&ScheduledCommand{Config: &EntryConfig{Timeout: 90 * time.Minute}}) {
        t.Fatalf("expected the entry's own timeout to be used, got %v", timeoutOfEntry(&ScheduledCommand{Config: &EntryConfig{Timeout: 90 * time.Minute}}))
    }
}

/* the unwind window is per entry, because how long an honest unwind takes is a property of the work rather than of the runner: a batch to flush is not a job that returns the moment its context is cancelled. An entry that names none falls to the runner's default, which is what a caller replacing that default means by replacing it. */
func TestGracefulTimeoutOf_TakesTheEntrysOwnWindowAndFallsBackToTheRunnerDefault(t *testing.T) {
    runner := &RunnerCommand{unwindGrace: commandUnwindGrace}

    entryWithoutWindow := &scheduledRunEntry{gracefulTimeout: gracefulTimeoutOfEntry(&ScheduledCommand{Config: &EntryConfig{}})}
    if commandUnwindGrace != runner.gracefulTimeoutOf(entryWithoutWindow) {
        t.Fatalf("expected an entry naming no window to take the runner default, got %v", runner.gracefulTimeoutOf(entryWithoutWindow))
    }

    entryWithWindow := &scheduledRunEntry{gracefulTimeout: gracefulTimeoutOfEntry(&ScheduledCommand{Config: &EntryConfig{GracefulTimeout: 20 * time.Minute}})}
    if 20*time.Minute != runner.gracefulTimeoutOf(entryWithWindow) {
        t.Fatalf("expected the entry's own window to be used, got %v", runner.gracefulTimeoutOf(entryWithWindow))
    }

    /* a negative window is not an opt-out: the window opens only once a cancellation has reached the command, and honouring it would tear the scope down the instant that cancellation landed */
    entryWithNegativeWindow := &scheduledRunEntry{gracefulTimeout: gracefulTimeoutOfEntry(&ScheduledCommand{Config: &EntryConfig{GracefulTimeout: -time.Second}})}
    if commandUnwindGrace != runner.gracefulTimeoutOf(entryWithNegativeWindow) {
        t.Fatalf("expected a negative window to read as unset, got %v", runner.gracefulTimeoutOf(entryWithNegativeWindow))
    }
}

/* the window an entry names governs the run end to end, not just the resolver: this drives the real invoke with a command that ignores its context and asserts the abandon lands after the entry's window rather than after the runner's much longer default. */
func TestRunnerCommand_AnEntrysGracefulWindowGovernsWhenItIsAbandoned(t *testing.T) {
    job := newWedgedCommand("job:top")
    defer close(job.release)

    configuration := NewConfiguration().
        Schedule("job:top", &EntryConfig{
            Schedule:        &Schedule{Minute: "0"},
            Timeout:         50 * time.Millisecond,
            GracefulTimeout: 50 * time.Millisecond,
        })

    runner := NewRunnerCommand(configuration, RunnerDialectCrontab, job)

    var closedScopes atomic.Int64
    serviceContainer := container.NewContainer()
    runtimeInstance := runtime.New(
        context.Background(),
        serviceContainer.NewScope(),
        closeCountingContainer{Container: serviceContainer, closed: &closedScopes},
    )

    completed := make(chan error, 1)
    go func() {
        completed <- invokeDiscardingRunId(runner, runtimeInstance, runner.entries[0])
    }()

    var invokeErr error
    select {
    case invokeErr = <-completed:
    case <-time.After(5 * time.Second):
        t.Fatal("invoke never returned: the entry's own graceful window did not govern the abandon, the runner default did")
    }

    if nil == invokeErr || false == errors.Is(invokeErr, ErrCommandTimeout) {
        t.Fatalf("expected the abandoned command to be reported as a timeout, got %v", invokeErr)
    }

    if 1 != closedScopes.Load() {
        t.Fatalf("expected the child scope to be released on the abandon path, closed %d", closedScopes.Load())
    }
}

func TestAbandonDelayOf_SaturatesInsteadOfWrappingNegative(t *testing.T) {
    saturated := abandonDelayOf(time.Hour, math.MaxInt64)
    if math.MaxInt64 != saturated {
        t.Fatalf("a graceful window written as the largest duration must saturate, got %d", saturated)
    }

    if 0 >= saturated {
        t.Fatalf("a non-positive delay arms a timer that fires at once, killing every run at t=0, got %d", saturated)
    }

    ordinary := abandonDelayOf(time.Minute, 30*time.Second)
    if 90*time.Second != ordinary {
        t.Fatalf("a sum that cannot overflow must be the plain sum, got %s", ordinary)
    }
}

func TestRunnerCommand_AnEntryWhoseWindowsCannotBeSummedStillRunsToCompletion(t *testing.T) {
    job := newRecordingCommand("job:top")

    configuration := NewConfiguration().
        Schedule("job:top", &EntryConfig{
            Schedule:        &Schedule{Minute: "0"},
            Timeout:         time.Hour,
            GracefulTimeout: math.MaxInt64,
        })

    runner := NewRunnerCommand(configuration, RunnerDialectCrontab, job)

    completed := make(chan error, 1)
    go func() {
        completed <- invokeDiscardingRunId(runner, newRunnerTestRuntime(context.Background()), runner.entries[0])
    }()

    select {
    case invokeErr := <-completed:
        if nil != invokeErr {
            t.Fatalf("the run was cut short by an abandon that should never have been armed: %v", invokeErr)
        }
    case <-time.After(5 * time.Second):
        t.Fatal("invoke never returned")
    }

    if 1 != job.runCount {
        t.Fatalf("expected the command to have run once, ran %d", job.runCount)
    }
}

func TestRunnerCommand_TheAbandonErrorNamesTheWindowTheRunWasActuallyGiven(t *testing.T) {
    job := newWedgedCommand("job:top")
    defer close(job.release)

    configuration := NewConfiguration().
        Schedule("job:top", &EntryConfig{
            Schedule:        &Schedule{Minute: "0"},
            Timeout:         20 * time.Millisecond,
            GracefulTimeout: 30 * time.Millisecond,
        })

    runner := NewRunnerCommand(configuration, RunnerDialectCrontab, job)

    completed := make(chan error, 1)
    go func() {
        completed <- invokeDiscardingRunId(runner, newRunnerTestRuntime(context.Background()), runner.entries[0])
    }()

    var invokeErr error
    select {
    case invokeErr = <-completed:
    case <-time.After(5 * time.Second):
        t.Fatal("invoke never returned")
    }

    reported, isReported := invokeErr.(*exception.Error)
    if false == isReported {
        t.Fatalf("expected an exception error, got %T", invokeErr)
    }

    unwindGrace := fmt.Sprintf("%v", reported.Context()["unwindGrace"])
    if (30 * time.Millisecond).String() != unwindGrace {
        t.Fatalf("the abandon must name the window this run was given, not the runner default; got %q", unwindGrace)
    }
}

/* the outer select cannot be made to see both cases ready from outside the runner — the window is one scheduling instant — so the branch is driven directly, with the completion already in the channel, which is the state that window produces. */
func TestResolveAbandonedRun_ACommandThatAlreadyAnsweredReportsItsOwnOutcome(t *testing.T) {
    runner := &RunnerCommand{unwindGrace: commandUnwindGrace}
    entry := &scheduledRunEntry{commandName: "job:top", timeout: time.Minute}

    commandErr := errors.New("the command's own failure")
    completed := make(chan error, 1)
    completed <- commandErr

    resolved := runner.resolveAbandonedRun(
        newRunnerTestRuntime(context.Background()),
        entry,
        "test-run-id",
        context.Background(),
        completed,
        abandonedAfterDeadline,
    )

    if false == errors.Is(resolved, commandErr) {
        t.Fatalf("expected the command's own failure, got %v", resolved)
    }

    if true == errors.Is(resolved, ErrCommandTimeout) {
        t.Fatalf("a command that already answered must not be reported abandoned, got %v", resolved)
    }
}

func TestResolveAbandonedRun_ACommandStillRunningIsReportedAbandoned(t *testing.T) {
    runner := &RunnerCommand{unwindGrace: commandUnwindGrace}
    entry := &scheduledRunEntry{commandName: "job:top", timeout: time.Minute}

    resolved := runner.resolveAbandonedRun(
        newRunnerTestRuntime(context.Background()),
        entry,
        "test-run-id",
        context.Background(),
        make(chan error, 1),
        abandonedAfterDeadline,
    )

    if false == errors.Is(resolved, ErrCommandTimeout) {
        t.Fatalf("expected the abandon to be reported, got %v", resolved)
    }

    if false == strings.Contains(resolved.Error(), "abandoned") {
        t.Fatalf("expected the failure to say the command was abandoned, got %v", resolved)
    }
}

/* a command that answered with its deadline already exceeded keeps its own error beside the timeout, the way the completion branch of the outer select reports it. */
func TestResolveAbandonedRun_ADeadlineExceededAnswerCarriesBothFailures(t *testing.T) {
    runner := &RunnerCommand{unwindGrace: commandUnwindGrace}
    entry := &scheduledRunEntry{commandName: "job:top", timeout: time.Minute}

    lapsedContext, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
    defer cancel()

    commandErr := errors.New("the command's own failure")
    completed := make(chan error, 1)
    completed <- commandErr

    resolved := runner.resolveAbandonedRun(newRunnerTestRuntime(context.Background()), entry, "test-run-id", lapsedContext, completed, abandonedAfterDeadline)

    if false == errors.Is(resolved, commandErr) || false == errors.Is(resolved, ErrCommandTimeout) {
        t.Fatalf("expected both the timeout and the command's own failure, got %v", resolved)
    }

    if true == strings.Contains(resolved.Error(), "abandoned") {
        t.Fatalf("a command that answered is cancelled, not abandoned, got %v", resolved)
    }
}

type typedNilErrorCommand struct {
    commandName string
}

func (instance *typedNilErrorCommand) Name() string {
    return instance.commandName
}

func (instance *typedNilErrorCommand) Description() string {
    return "typed nil error command"
}

func (instance *typedNilErrorCommand) Flags() []clicontract.Flag {
    return nil
}

func (instance *typedNilErrorCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error {
    var failure *exception.Error

    return failure
}

func TestRunnerCommand_InvokeNormalizesATypedNilCommandError(t *testing.T) {
    job := &typedNilErrorCommand{commandName: "job:top"}

    configuration := NewConfiguration().
        Schedule("job:top", &EntryConfig{Schedule: &Schedule{Minute: "0"}})

    runner := NewRunnerCommand(configuration, RunnerDialectCrontab, job)

    _, invokeErr := runner.invoke(newRunnerTestRuntime(context.Background()), runner.entries[0])
    if nil != invokeErr {
        t.Fatalf("expected the typed-nil command error to normalize to success, got %v", invokeErr)
    }
}

type capturingRunnerLogger struct {
    mutex   sync.Mutex
    entries []capturedRunnerRecord
}

type capturedRunnerRecord struct {
    message string
    context map[string]any
}

func (instance *capturingRunnerLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()
    instance.entries = append(instance.entries, capturedRunnerRecord{message: message, context: context})
}

func (instance *capturingRunnerLogger) Debug(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelDebug, message, context)
}

func (instance *capturingRunnerLogger) Info(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelInfo, message, context)
}

func (instance *capturingRunnerLogger) Warning(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelWarning, message, context)
}

func (instance *capturingRunnerLogger) Error(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelError, message, context)
}

func (instance *capturingRunnerLogger) Emergency(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelEmergency, message, context)
}

func (instance *capturingRunnerLogger) recordByMessageProbe(message string) (capturedRunnerRecord, bool) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    for _, entry := range instance.entries {
        if message == entry.message {
            return entry, true
        }
    }

    return capturedRunnerRecord{}, false
}

func (instance *capturingRunnerLogger) recordByContextValue(key string, value string) (capturedRunnerRecord, bool) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    for _, entry := range instance.entries {
        if "cron runner command failed" != entry.message {
            continue
        }

        if value == entry.context[key] {
            return entry, true
        }
    }

    return capturedRunnerRecord{}, false
}

func TestRunnerCommand_DispatchFailureRecordCarriesTheExceptionContext(t *testing.T) {
    panickingJob := &panickingCommand{commandName: "job:panicking"}
    /* a plain error carries no exception context of its own, so the record's commandName can come only from the dispatch site — the panicking sibling's failure already names the command inside its exception context and cannot prove that half */
    plainJob := &recordingCommand{commandName: "job:plain", runErr: errors.New("plain failure")}

    configuration := NewConfiguration().
        Schedule("job:panicking", &EntryConfig{Schedule: &Schedule{Minute: "0"}}).
        Schedule("job:plain", &EntryConfig{Schedule: &Schedule{Minute: "0"}})

    runner := NewRunnerCommand(configuration, RunnerDialectCrontab, panickingJob, plainJob)

    captured := &capturingRunnerLogger{}
    serviceContainer := container.NewContainer()
    serviceContainer.MustRegister(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return captured, nil
        },
    )
    runtimeInstance := runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)

    at := time.Date(2026, time.July, 15, 9, 0, 0, 0, time.UTC)
    if runErr := runner.runDue(runtimeInstance, at); nil == runErr {
        t.Fatal("expected the panicking job to fail the run")
    }

    panickingRecord, panickingFound := captured.recordByContextValue("commandName", "job:panicking")
    if false == panickingFound {
        t.Fatal("expected a failure record naming the panicking job")
    }

    if _, hasPanicValue := panickingRecord.context["panicValue"]; false == hasPanicValue {
        t.Fatalf("expected the record to carry the panic value from the failure's exception context, got keys %v", panickingRecord.context)
    }

    if _, plainFound := captured.recordByContextValue("commandName", "job:plain"); false == plainFound {
        t.Fatal("expected the plain failure's record to name its command through the dispatch site's own context")
    }
}

func TestRunnerCommand_OnceAggregateCarriesTheFailuresAsCause(t *testing.T) {
    sentinel := errors.New("job failure sentinel")
    job := &recordingCommand{commandName: "job:top", runErr: sentinel}

    configuration := NewConfiguration().
        Schedule("job:top", &EntryConfig{Schedule: &Schedule{Minute: "0"}})

    runner := NewRunnerCommand(configuration, RunnerDialectCrontab, job)

    at := time.Date(2026, time.July, 15, 9, 0, 0, 0, time.UTC)
    runErr := runner.runDue(newRunnerTestRuntime(context.Background()), at)
    if nil == runErr {
        t.Fatal("expected the failing job to fail the run")
    }

    if false == errors.Is(runErr, sentinel) {
        t.Fatalf("expected the aggregate to carry the command's own failure as its cause, got %v", runErr)
    }
}

func TestRunnerCommand_DestinationFileEntryPanicsAtConstruction(t *testing.T) {
    defer func() {
        recovered := recover()
        if nil == recovered {
            t.Fatal("expected a panic for an entry routed to another crontab file")
        }

        recoveredErr, isError := recovered.(error)
        if false == isError {
            t.Fatalf("expected the panic value to be an error, got %T", recovered)
        }

        if false == errors.Is(recoveredErr, ErrUnsupportedRunnerEntry) {
            t.Fatalf("expected ErrUnsupportedRunnerEntry, got %v", recoveredErr)
        }
    }()

    job := newRecordingCommand("job:routed")

    configuration := NewConfiguration().
        Schedule("job:routed", &EntryConfig{
            Schedule:        &Schedule{Minute: "0"},
            DestinationFile: "other.crontab",
        })

    NewRunnerCommand(configuration, RunnerDialectCrontab, job)
}

type identityProbeCommand struct {
    commandName     string
    observedRunId   string
    processResolved bool
}

func (instance *identityProbeCommand) Name() string {
    return instance.commandName
}

func (instance *identityProbeCommand) Description() string {
    return "identity probe command"
}

func (instance *identityProbeCommand) Flags() []clicontract.Flag {
    return nil
}

func (instance *identityProbeCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error {
    logging.LoggerFromRuntime(runtimeInstance).Info("job line", nil)

    processContext := application.ProcessContextFromResolver(runtimeInstance.Scope())
    if nil != processContext {
        instance.processResolved = true
        instance.observedRunId = processContext.ProcessId()
    }

    return nil
}

/* the child scope carries the console identity the cli entry point installs into its own: a fresh per-run ProcessContext and a logger that keeps the runner's processId while adding the run's cronRunId. */
func TestRunnerCommand_InvokeInstallsThePerRunIdentity(t *testing.T) {
    job := &identityProbeCommand{commandName: "job:identity"}

    configuration := NewConfiguration().
        Schedule("job:identity", &EntryConfig{Schedule: &Schedule{Minute: "0"}})

    runner := NewRunnerCommand(configuration, RunnerDialectCrontab, job)

    captured := &capturingRunnerLogger{}
    serviceContainer := container.NewContainer()
    serviceContainer.MustRegister(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return captured, nil
        },
    )

    parentScope := serviceContainer.NewScope()
    defer func() {
        _ = parentScope.Close()
    }()
    parentScope.MustOverrideProtectedInstance(
        logging.ServiceLogger,
        logging.NewProcessLogger(captured, "proc-1", "processId"),
    )
    parentScope.MustOverrideProtectedInstance(
        application.ServiceProcessContext,
        application.NewProcessContext("proc-1", time.Now()),
    )

    runtimeInstance := runtime.New(context.Background(), parentScope, serviceContainer)

    if _, invokeErr := runner.invoke(runtimeInstance, runner.entries[0]); nil != invokeErr {
        t.Fatalf("invoke: %v", invokeErr)
    }

    if false == job.processResolved {
        t.Fatal("expected the job to resolve a ProcessContext through the documented console door")
    }

    if "proc-1" == job.observedRunId || "" == job.observedRunId {
        t.Fatalf("expected a fresh per-run id, got %q", job.observedRunId)
    }

    record, found := captured.recordByMessageProbe("job line")
    if false == found {
        t.Fatal("expected the job's record to be captured")
    }

    if "proc-1" != record.context["processId"] {
        t.Fatalf("expected the job's record to keep the runner's processId, got %v", record.context["processId"])
    }

    if job.observedRunId != record.context["cronRunId"] {
        t.Fatalf("expected the job's record to carry the run's id, got %v", record.context["cronRunId"])
    }
}

/* the runner accepts the standard flags every melody command carries, so the framework's -v rewrite no longer kills it at parse, and it carries its own two beside them. Asserted by name rather than by count: a count passes just as well when a standard flag is swapped for another, which is not what the sentence above claims. */
func TestRunnerCommand_CarriesTheStandardFlags(t *testing.T) {
    runner := NewRunnerCommand(NewConfiguration(), RunnerDialectCrontab)

    declared := map[string]bool{}
    for _, flag := range runner.Flags() {
        declared[flag.Definition().Name] = true
    }

    for _, standardFlag := range output.StandardFlags() {
        standardFlagName := standardFlag.Definition().Name
        if false == declared[standardFlagName] {
            t.Fatalf("expected the standard flag %q to be declared", standardFlagName)
        }
    }

    for _, ownFlag := range []string{flagNameOnce, flagNameReportIdle} {
        if false == declared[ownFlag] {
            t.Fatalf("expected the runner's own flag %q to be declared", ownFlag)
        }
    }
}

/* invokeDiscardingRunId adapts invoke's two-value answer to the single error the completion channels of these tests carry; the runId's presence on the records has its own test. */
func invokeDiscardingRunId(runner *RunnerCommand, runtimeInstance runtimecontract.Runtime, entry *scheduledRunEntry) error {
    _, invokeErr := runner.invoke(runtimeInstance, entry)

    return invokeErr
}

/* the classification that keeps a clean shutdown out of the failure aggregate: only the parent's cancellation qualifies, and a deadline the entry asked for stays a failure whatever the shutdown is doing. */
func TestIsShutdownCancellation_OnlyTheParentsCancellationQualifies(t *testing.T) {
    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()
    cancelledRuntime := newRunnerTestRuntime(cancelledContext)
    liveRuntime := newRunnerTestRuntime(context.Background())

    if false == isShutdownCancellation(cancelledRuntime, context.Canceled) {
        t.Fatal("expected the parent's cancellation to classify as the shutdown reaching the run")
    }

    if true == isShutdownCancellation(liveRuntime, context.Canceled) {
        t.Fatal("expected a cancellation under a live runner to stay a failure")
    }

    if true == isShutdownCancellation(cancelledRuntime, context.DeadlineExceeded) {
        t.Fatal("expected the entry's own deadline to stay a failure under shutdown")
    }

    /* the portant input for the deadline clause: a run both cancelled and deadline-cut answers a join carrying both flavors, and the deadline half must keep it a failure — on the bare DeadlineExceeded the clause is shadowed by the Canceled test above it */
    if true == isShutdownCancellation(cancelledRuntime, errors.Join(context.DeadlineExceeded, context.Canceled)) {
        t.Fatal("expected a deadline-cut run to stay a failure even when the shutdown's cancellation rides the same join")
    }

    if true == isShutdownCancellation(cancelledRuntime, errors.New("job broke")) {
        t.Fatal("expected an ordinary failure to stay one under shutdown")
    }
}

/* the run's id is minted first and returned beside the outcome, so the runner's records about the run carry the cronRunId the run's own records carry. */
func TestInvoke_AnswersTheRunIdBesideTheOutcome(t *testing.T) {
    job := newRecordingCommand("job:runid")

    configuration := NewConfiguration().
        Schedule("job:runid", &EntryConfig{Schedule: &Schedule{Minute: "0"}})

    runner := NewRunnerCommand(configuration, RunnerDialectCrontab, job)

    runId, invokeErr := runner.invoke(newRunnerTestRuntime(context.Background()), runner.entries[0])
    if nil != invokeErr {
        t.Fatalf("expected the run to succeed, got %v", invokeErr)
    }

    if "" == runId {
        t.Fatal("expected a non-empty run id beside the outcome")
    }
}

/* the runner's recovery boundary keeps what wakes an operator usefully: the panic value travels as the cause so errors.Is still reaches the failure underneath, and the stack is captured on the goroutine that raised it. Stringified into the context alone, a nightly job that died on the framework's own idiom reached the record as a message and a command name, with no file and no line anywhere. */
func TestRunnerCommand_APanickingJobKeepsItsCauseAndItsStack(t *testing.T) {
    rootCause := errors.New("dial tcp 10.0.0.7:5432: connect: connection refused")

    job := &idiomaticPanickingCommand{
        commandName: "job:panicking",
        panicValue: exception.NewError(
            "config parameter is not defined",
            exceptioncontract.Context{"parameterName": "app.report.recipient"},
            rootCause,
        ),
    }

    configuration := NewConfiguration().
        Schedule("job:panicking", &EntryConfig{Schedule: &Schedule{Minute: "0"}})

    runner := NewRunnerCommand(configuration, RunnerDialectCrontab, job)

    captured := &capturingRunnerLogger{}
    serviceContainer := container.NewContainer()
    serviceContainer.MustRegister(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return captured, nil
        },
    )
    runtimeInstance := runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)

    at := time.Date(2026, time.July, 15, 9, 0, 0, 0, time.UTC)

    runErr := runner.runDue(runtimeInstance, at)
    if nil == runErr {
        t.Fatal("expected the panicking job to fail the run")
    }

    if false == errors.Is(runErr, rootCause) {
        t.Fatalf("expected the cause chain of the panicking error to survive, got %v", runErr)
    }

    record, found := captured.recordByContextValue("commandName", "job:panicking")
    if false == found {
        t.Fatal("expected a failure record naming the panicking job")
    }

    stack, hasStack := record.context["panicStack"]
    if false == hasStack {
        t.Fatalf("expected the record to carry the stack of the panicking job, got keys %v", record.context)
    }

    stackText, isString := stack.(string)
    if false == isString || false == strings.Contains(stackText, "runScheduledCommand") {
        t.Fatalf("expected the stack of the goroutine that raised the panic, got %v", stack)
    }
}

/* a typed-nil panic value must not reach the cause slot: its Error() dereferences a nil receiver, and the render of the very record the boundary exists to write would take the scheduler down with it. */
func TestRunnerCommand_ATypedNilPanicValueIsNotHandedOnAsACause(t *testing.T) {
    var typedNil *exception.Error

    job := &idiomaticPanickingCommand{commandName: "job:panicking", panicValue: typedNil}

    configuration := NewConfiguration().
        Schedule("job:panicking", &EntryConfig{Schedule: &Schedule{Minute: "0"}})

    runner := NewRunnerCommand(configuration, RunnerDialectCrontab, job)

    captured := &capturingRunnerLogger{}
    serviceContainer := container.NewContainer()
    serviceContainer.MustRegister(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return captured, nil
        },
    )
    runtimeInstance := runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)

    at := time.Date(2026, time.July, 15, 9, 0, 0, 0, time.UTC)

    runErr := runner.runDue(runtimeInstance, at)
    if nil == runErr {
        t.Fatal("expected the panicking job to fail the run")
    }

    /* the record renders, which is what a cause holding the typed nil would have taken away */
    if _, found := captured.recordByContextValue("commandName", "job:panicking"); false == found {
        t.Fatal("expected the failure record to be written rather than lost to a second panic")
    }
}

type idiomaticPanickingCommand struct {
    commandName string
    panicValue  any
}

func (instance *idiomaticPanickingCommand) Name() string {
    return instance.commandName
}

func (instance *idiomaticPanickingCommand) Description() string {
    return "idiomatic panicking command"
}

func (instance *idiomaticPanickingCommand) Flags() []clicontract.Flag {
    return nil
}

func (instance *idiomaticPanickingCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error {
    panic(instance.panicValue)
}

/* outputRecordFor reads the one record carrying a job's own output, which the failure lookup beside it cannot answer: that one filters on the dispatch failure message, and a successful run files no failure at all */
func (instance *capturingRunnerLogger) outputRecordFor(commandName string) (capturedRunnerRecord, bool) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    for _, entry := range instance.entries {
        if "cron: scheduled command output" != entry.message {
            continue
        }

        if commandName == entry.context["commandName"] {
            return entry, true
        }
    }

    return capturedRunnerRecord{}, false
}

type outputWritingProbeCommand struct {
    commandName    string
    observedFormat string
    output         string
}

func (instance *outputWritingProbeCommand) Name() string {
    return instance.commandName
}

func (instance *outputWritingProbeCommand) Description() string {
    return "a job that writes its own report and reads its own posture"
}

func (instance *outputWritingProbeCommand) Flags() []clicontract.Flag {
    return output.StandardFlags()
}

func (instance *outputWritingProbeCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error {
    instance.observedFormat = string(output.NormalizeOption(output.ParseOptionFromCommand(commandContext)).Format)

    _, writeErr := io.WriteString(commandContext.Writer(), instance.output)

    return writeErr
}

/* a job's own output belongs in the journal. Due entries run concurrently and every one of them held the same os.Stdout, so several reports arrived interleaved, in colour, and none of them reached the log file the operator reads — the runner's documented promise that it writes nothing to the command output itself was true only of the runner's own lines. */
func TestRunnerCommand_ScheduledJobOutputIsCapturedIntoTheJournal(t *testing.T) {
    job := &outputWritingProbeCommand{commandName: "job:reporting", output: "SYNCED 42 ROWS\n"}

    configuration := NewConfiguration().
        Schedule("job:reporting", &EntryConfig{Schedule: &Schedule{Minute: "0"}})

    runner := NewRunnerCommand(configuration, RunnerDialectCrontab, job)

    captured := &capturingRunnerLogger{}
    serviceContainer := container.NewContainer()
    serviceContainer.MustRegister(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return captured, nil
        },
    )
    runtimeInstance := runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)

    at := time.Date(2026, time.July, 15, 9, 0, 0, 0, time.UTC)
    if runErr := runner.runDue(runtimeInstance, at); nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    record, found := captured.outputRecordFor("job:reporting")
    if false == found {
        t.Fatal("expected a record naming the job that wrote")
    }

    if "SYNCED 42 ROWS\n" != record.context["commandOutput"] {
        t.Fatalf("expected the job's own output in the record, got %#v", record.context["commandOutput"])
    }

    /* the run id ties the output to the run that produced it, which is the whole point under overlapping runs of one entry */
    if runId, hasRunId := record.context["cronRunId"].(string); false == hasRunId || "" == runId {
        t.Fatalf("expected the record to name the run, got %#v", record.context["cronRunId"])
    }

    /* a job that printed nothing files nothing: an empty record per entry per minute would bury the runner's own lines */
    silent := &outputWritingProbeCommand{commandName: "job:silent", output: ""}
    silentConfiguration := NewConfiguration().
        Schedule("job:silent", &EntryConfig{Schedule: &Schedule{Minute: "0"}})

    silentCaptured := &capturingRunnerLogger{}
    silentContainer := container.NewContainer()
    silentContainer.MustRegister(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return silentCaptured, nil
        },
    )

    if runErr := NewRunnerCommand(silentConfiguration, RunnerDialectCrontab, silent).runDue(
        runtime.New(context.Background(), silentContainer.NewScope(), silentContainer),
        at,
    ); nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    if _, silentFound := silentCaptured.outputRecordFor("job:silent"); true == silentFound {
        t.Fatal("expected a job that printed nothing to file no output record")
    }
}

/* what one run may contribute to the journal is bounded: a job printing a row per record would write its whole result set into one log line, and the failure of the log would be caused by the reporting meant to make the run visible. What was cut is counted, so a truncated report never reads as a complete one — and the job must not discover the budget as a short write on its own report. */
func TestRunnerCommand_ScheduledJobOutputIsBoundedAndSaysWhatItCut(t *testing.T) {
    oversized := strings.Repeat("x", scheduledOutputCaptureLimit+512)
    job := &outputWritingProbeCommand{commandName: "job:verbose", output: oversized}

    configuration := NewConfiguration().
        Schedule("job:verbose", &EntryConfig{Schedule: &Schedule{Minute: "0"}})

    captured := &capturingRunnerLogger{}
    serviceContainer := container.NewContainer()
    serviceContainer.MustRegister(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return captured, nil
        },
    )

    at := time.Date(2026, time.July, 15, 9, 0, 0, 0, time.UTC)

    /* the run has to succeed: a short write answered to the job would come back as the job's own failure */
    if runErr := NewRunnerCommand(configuration, RunnerDialectCrontab, job).runDue(
        runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer),
        at,
    ); nil != runErr {
        t.Fatalf("expected the write to be answered in full, got %v", runErr)
    }

    record, found := captured.outputRecordFor("job:verbose")
    if false == found {
        t.Fatal("expected a record naming the job that wrote")
    }

    recorded, isText := record.context["commandOutput"].(string)
    if false == isText || scheduledOutputCaptureLimit != len(recorded) {
        t.Fatalf("expected the record to carry exactly the budget, got %d bytes", len(recorded))
    }

    if 512 != record.context["commandOutputDroppedBytes"] {
        t.Fatalf("expected the dropped byte count, got %#v", record.context["commandOutputDroppedBytes"])
    }
}

/* the writer itself is where two of its rules live, and neither is observable through a job that writes once: the budget is measured against what is already held, so a second write has to see the first, and the full length is always reported because a short write is an io.Writer contract violation the standard library answers with an error the job would then return as its own failure. */
func TestScheduledOutputCapture_ReportsTheFullLengthAndBoundsWhatItKeeps(t *testing.T) {
    capture := newScheduledOutputCapture()

    firstChunk := strings.Repeat("a", scheduledOutputCaptureLimit-100)
    firstWritten, firstErr := capture.Write([]byte(firstChunk))
    if nil != firstErr || len(firstChunk) != firstWritten {
        t.Fatalf("expected the first write to be answered in full, got %d, %v", firstWritten, firstErr)
    }

    /* the second write overruns the budget by 512: only 100 bytes of it may be kept */
    secondChunk := strings.Repeat("b", 612)
    secondWritten, secondErr := capture.Write([]byte(secondChunk))
    if nil != secondErr || len(secondChunk) != secondWritten {
        t.Fatalf("expected the overrunning write to be answered in full, got %d, %v", secondWritten, secondErr)
    }

    kept, dropped := capture.captured()
    if scheduledOutputCaptureLimit != len(kept) {
        t.Fatalf("expected the capture to stop at the budget, kept %d bytes", len(kept))
    }

    if 512 != dropped {
        t.Fatalf("expected the overrun to be counted, got %d", dropped)
    }

    /* a write arriving after the budget is exhausted is still answered in full and still counted */
    thirdWritten, thirdErr := capture.Write([]byte("ccc"))
    if nil != thirdErr || 3 != thirdWritten {
        t.Fatalf("expected the write past the budget to be answered in full, got %d, %v", thirdWritten, thirdErr)
    }

    _, droppedAfterThird := capture.captured()
    if 515 != droppedAfterThird {
        t.Fatalf("expected the write past the budget to be counted, got %d", droppedAfterThird)
    }
}

/* an entry declares its own posture, because the runner has none to lend it: the dispatch used to hand the child nothing but its command name, so every job ran on the declared defaults of its flags whatever the entry configured, and the manifest generated from the same Configuration ran a different command line. */
func TestRunnerCommand_EntryArgumentsReachTheScheduledCommand(t *testing.T) {
    job := &outputWritingProbeCommand{commandName: "job:posture", output: "done\n"}

    entryArguments := []string{"--format=json"}

    configuration := NewConfiguration().
        Schedule("job:posture", &EntryConfig{
            Schedule:  &Schedule{Minute: "0"},
            Arguments: entryArguments,
        })

    /* the registrant keeps writing to its own slice: what was registered is what stays in force */
    entryArguments[0] = "--format=table"

    runner := NewRunnerCommand(configuration, RunnerDialectCrontab, job)

    at := time.Date(2026, time.July, 15, 9, 0, 0, 0, time.UTC)
    if runErr := runner.runDue(newRunnerTestRuntime(context.Background()), at); nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    if string(output.FormatJson) != job.observedFormat {
        t.Fatalf("expected the entry's own posture to reach the child, got %q", job.observedFormat)
    }
}

/* newCapturingRunnerRuntime is a runtime whose logger records every line, which is how the runner's own declarations and failure records are read back. */
func newCapturingRunnerRuntime(ctx context.Context) (runtimecontract.Runtime, *capturingRunnerLogger) {
    captured := &capturingRunnerLogger{}
    serviceContainer := container.NewContainer()
    serviceContainer.MustRegister(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return captured, nil
        },
    )

    return runtime.New(ctx, serviceContainer.NewScope(), serviceContainer), captured
}

/* TestRunnerCommand_OnceRendersTheMachineDocument pins the document a deploy step reads. The command declared and validated --format=json through StandardFlags and then answered zero bytes on every path: `melody:cron:run --once --format=json | jq` received an empty stream, which is indistinguishable, to the step consuming it, from a missing binary. The only observable effect the flag had was that the cli banner disappeared. */
func TestRunnerCommand_OnceRendersTheMachineDocument(t *testing.T) {
    job := newRecordingCommand("job:top")

    configuration := NewConfiguration().
        Schedule("job:top", &EntryConfig{Schedule: &Schedule{Minute: "0"}})

    runner := NewRunnerCommand(configuration, RunnerDialectCrontab, job)
    runner.now = func() time.Time {
        return time.Date(2026, time.July, 15, 9, 0, 0, 0, time.UTC)
    }

    buffer := &bytes.Buffer{}

    cliCommand := newRunnerDispatch(runner, nil, buffer)

    if runErr := cliCommand.Run(context.Background(), []string{runner.Name(), "--once", "--format=json"}); nil != runErr {
        t.Fatalf("unexpected error running --once: %v", runErr)
    }

    if 0 == buffer.Len() {
        t.Fatal("expected --format=json to answer a document, got an empty stream")
    }

    document := struct {
        Meta struct {
            Command string `json:"command"`
        } `json:"meta"`
        Data struct {
            Configured int `json:"configured"`
            Ran        []struct {
                Command  string `json:"command"`
                Schedule string `json:"schedule"`
                RunId    string `json:"runId"`
                Failed   bool   `json:"failed"`
            } `json:"ran"`
        } `json:"data"`
    }{}
    if decodeErr := json.Unmarshal(buffer.Bytes(), &document); nil != decodeErr {
        t.Fatalf("expected one json document, got %q: %v", buffer.String(), decodeErr)
    }

    if runner.Name() != document.Meta.Command {
        t.Fatalf("expected the runner to name itself, got %q", document.Meta.Command)
    }

    if 1 != document.Data.Configured {
        t.Fatalf("expected the configured count, got %d", document.Data.Configured)
    }

    if 1 != len(document.Data.Ran) {
        t.Fatalf("expected the dispatched run in the document, got %v", document.Data.Ran)
    }

    if "job:top" != document.Data.Ran[0].Command {
        t.Fatalf("expected the dispatched command, got %q", document.Data.Ran[0].Command)
    }

    if "0 * * * *" != document.Data.Ran[0].Schedule {
        t.Fatalf("expected the schedule the entry runs under, got %q", document.Data.Ran[0].Schedule)
    }

    if "" == document.Data.Ran[0].RunId {
        t.Fatalf("expected the run id its own records carry, got %q", document.Data.Ran[0].RunId)
    }

    if true == document.Data.Ran[0].Failed {
        t.Fatalf("expected the healthy run to be reported as such, got %v", document.Data.Ran[0])
    }
}

/* a failed run puts the failure inside the envelope's error, naming which command failed, so the document reports the same verdict as the exit code */
func TestRunnerCommand_OnceRendersTheFailureInsideTheDocument(t *testing.T) {
    failing := newRecordingCommand("job:failing")
    failing.runErr = errors.New("boom")

    configuration := NewConfiguration().
        Schedule("job:failing", &EntryConfig{Schedule: &Schedule{Minute: "0"}})

    runner := NewRunnerCommand(configuration, RunnerDialectCrontab, failing)
    runner.now = func() time.Time {
        return time.Date(2026, time.July, 15, 9, 0, 0, 0, time.UTC)
    }

    buffer := &bytes.Buffer{}
    runtimeInstance, _ := newCapturingRunnerRuntime(context.Background())

    cliCommand := newRunnerDispatch(runner, runtimeInstance, buffer)

    if runErr := cliCommand.Run(context.Background(), []string{runner.Name(), "--once", "--format=json"}); nil == runErr {
        t.Fatal("expected the failed job to fail the run")
    }

    document := struct {
        Data struct {
            Ran []struct {
                Command string `json:"command"`
                Failed  bool   `json:"failed"`
                Error   string `json:"error"`
            } `json:"ran"`
        } `json:"data"`
        Error *struct {
            Code    string              `json:"code"`
            Details map[string][]string `json:"details"`
        } `json:"error"`
    }{}
    if decodeErr := json.Unmarshal(buffer.Bytes(), &document); nil != decodeErr {
        t.Fatalf("expected one json document, got %q: %v", buffer.String(), decodeErr)
    }

    if nil == document.Error {
        t.Fatalf("expected the failure inside the envelope, got %q", buffer.String())
    }

    if "cron.runFailed" != document.Error.Code {
        t.Fatalf("expected the run-failed code, got %q", document.Error.Code)
    }

    if 1 != len(document.Error.Details["failed"]) || "job:failing" != document.Error.Details["failed"][0] {
        t.Fatalf("expected the failed command named in the details, got %v", document.Error.Details)
    }

    if 1 != len(document.Data.Ran) || false == document.Data.Ran[0].Failed {
        t.Fatalf("expected the run itself to be reported as failed, got %v", document.Data.Ran)
    }

    if false == strings.Contains(document.Data.Ran[0].Error, "boom") {
        t.Fatalf("expected the run to carry its own failure, got %q", document.Data.Ran[0].Error)
    }
}

/* TestRunnerCommand_DeclaresWhatItDrives pins the other half of the same silence. A scheduler built over a configuration emptied by a refactor, or whose entries an environment gate filtered away, ran forever, exited successfully and wrote not one byte on any channel: nothing distinguished it from a healthy one, and the absence was noticed days later, when the nightly sweep turned out not to have run. */
func TestRunnerCommand_DeclaresWhatItDrives(t *testing.T) {
    job := newRecordingCommand("job:top")

    configuration := NewConfiguration().
        Schedule("job:top", &EntryConfig{Schedule: &Schedule{Minute: "0"}})

    runner := NewRunnerCommand(configuration, RunnerDialectCrontab, job)

    runtimeInstance, captured := newCapturingRunnerRuntime(context.Background())

    runner.declareSchedule(runtimeInstance)

    record, found := captured.recordByMessageProbe("cron runner drives the scheduled entries")
    if false == found {
        t.Fatalf("expected the runner to declare what it drives, got %v", captured.entries)
    }

    if 1 != record.context["entries"] {
        t.Fatalf("expected the entry count, got %v", record.context["entries"])
    }

    scheduled, isList := record.context["scheduled"].([]map[string]any)
    if false == isList || 1 != len(scheduled) {
        t.Fatalf("expected the scheduled entries in the record, got %v", record.context["scheduled"])
    }

    if "job:top" != scheduled[0]["command"] {
        t.Fatalf("expected the command name, got %v", scheduled[0]["command"])
    }

    if "0 * * * *" != scheduled[0]["schedule"] {
        t.Fatalf("expected the schedule the entry runs under, got %v", scheduled[0]["schedule"])
    }
}

/* a runner driving nothing says so at warning, mirroring the generator's own nothingToWrite: a configuration emptied by a refactor is exactly the case where silence reads as health */
func TestRunnerCommand_DeclaresAnEmptyScheduleAsAWarning(t *testing.T) {
    runner := NewRunnerCommand(NewConfiguration(), RunnerDialectCrontab)

    runtimeInstance, captured := newCapturingRunnerRuntime(context.Background())

    runner.declareSchedule(runtimeInstance)

    record, found := captured.recordByMessageProbe("cron runner drives no scheduled entries: nothing will ever be dispatched")
    if false == found {
        t.Fatalf("expected the empty schedule to be declared, got %v", captured.entries)
    }

    if 0 != record.context["entries"] {
        t.Fatalf("expected the zero count, got %v", record.context["entries"])
    }
}

/* TestRunScheduledCommand_APanicKeepsItsCauseChain pins the record a panicking job produces. The recovery boundary joined its rich error onto the (nil) run error, and errors.Join answers an Unwrap of []error while exception.LogContext anchors cause and causeChain on errors.Unwrap — the single-value form — so the record carried the top message and nothing else: not the context naming the parameter, not the chain naming what refused. The same failure RETURNED rather than raised filed a complete record, which is the difference this closes. */
func TestRunScheduledCommand_APanicKeepsItsCauseChain(t *testing.T) {
    dialErr := errors.New("dial tcp 10.0.0.9:5432: connect: connection refused")

    job := &panicValueCommand{
        commandName: "job:panicking",
        panicValue: exception.NewError(
            "the ledger client refused the handshake",
            exceptioncontract.Context{"ledger": "acme-prod", "endpoint": "10.0.0.9:5432"},
            dialErr,
        ),
    }

    configuration := NewConfiguration().
        Schedule("job:panicking", &EntryConfig{Schedule: &Schedule{Minute: "0"}})

    runner := NewRunnerCommand(configuration, RunnerDialectCrontab, job)

    runtimeInstance, captured := newCapturingRunnerRuntime(context.Background())

    at := time.Date(2026, time.July, 15, 9, 0, 0, 0, time.UTC)
    if runErr := runner.runDue(runtimeInstance, at); nil == runErr {
        t.Fatal("expected the panicking job to fail the run")
    }

    record, found := captured.recordByMessageProbe("cron runner command failed")
    if false == found {
        t.Fatalf("expected the failure record, got %v", captured.entries)
    }

    if nil == record.context["cause"] {
        t.Fatalf("expected the panic's cause in the record, got %v", record.context)
    }

    if false == strings.Contains(fmt.Sprintf("%v", record.context["cause"]), "refused the handshake") {
        t.Fatalf("expected the cause to be the panic's own error, got %v", record.context["cause"])
    }

    causeChain, isChain := record.context["causeChain"].([]string)
    if false == isChain || 2 > len(causeChain) {
        t.Fatalf("expected the whole cause chain in the record, got %v", record.context["causeChain"])
    }

    if false == strings.Contains(strings.Join(causeChain, " | "), "connection refused") {
        t.Fatalf("expected the chain to reach what refused, got %v", causeChain)
    }

    /* the context of the panic error itself travels under causeContextChain, which is where LogContext files the context of every link below the top one: the ledger and the endpoint are exactly what tells an outage from a credential change from a wiring fault, and none of the three reached any record while the boundary joined */
    causeContextChain, isContextChain := record.context["causeContextChain"].([]map[string]any)
    if false == isContextChain || 0 == len(causeContextChain) {
        t.Fatalf("expected the cause context chain in the record, got %v", record.context["causeContextChain"])
    }

    if "acme-prod" != causeContextChain[0]["ledger"] {
        t.Fatalf("expected the panic error's own context in the record, got %v", causeContextChain)
    }

    if "10.0.0.9:5432" != causeContextChain[0]["endpoint"] {
        t.Fatalf("expected the panic error's own context in the record, got %v", causeContextChain)
    }
}

/* panicValueCommand panics with a value the test chooses, so the boundary's handling of a rich error-shaped panic is observable */
type panicValueCommand struct {
    commandName string
    panicValue  any
}

func (instance *panicValueCommand) Name() string {
    return instance.commandName
}

func (instance *panicValueCommand) Description() string {
    return "panicking command with a chosen value"
}

func (instance *panicValueCommand) Flags() []clicontract.Flag {
    return nil
}

func (instance *panicValueCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error {
    panic(instance.panicValue)
}

/* a timeout carries the command's own failure down one chain: both stay reachable by identity, and the record built from that chain reaches the command's own context instead of stopping at the join */
func TestTimeoutError_CarriesTheCommandFailureDownOneChain(t *testing.T) {
    runner := &RunnerCommand{unwindGrace: commandUnwindGrace}
    entry := &scheduledRunEntry{commandName: "job:top", timeout: time.Minute}

    commandErr := exception.NewError(
        "the nightly sweep could not reach its ledger",
        exceptioncontract.Context{"ledger": "acme-prod"},
        errors.New("connection refused"),
    )

    timeoutErr := runner.timeoutError(entry, false, commandErr)

    if false == errors.Is(timeoutErr, ErrCommandTimeout) {
        t.Fatalf("expected the classification to stay reachable, got %v", timeoutErr)
    }

    if false == errors.Is(timeoutErr, commandErr) {
        t.Fatalf("expected the command's own failure to stay reachable, got %v", timeoutErr)
    }

    logContext := exception.LogContext(timeoutErr)

    causeChain, isChain := logContext["causeChain"].([]string)
    if false == isChain || 3 > len(causeChain) {
        t.Fatalf("expected the whole chain in the record, got %v", logContext["causeChain"])
    }

    if false == strings.Contains(strings.Join(causeChain, " | "), "connection refused") {
        t.Fatalf("expected the chain to reach what refused, got %v", causeChain)
    }
}

/* a timeout with no command failure keeps the sentinel alone as its cause, so the shape a caller reads does not change with the presence of a second error */
func TestTimeoutError_WithoutACommandFailureWrapsTheSentinelAlone(t *testing.T) {
    runner := &RunnerCommand{unwindGrace: commandUnwindGrace}
    entry := &scheduledRunEntry{commandName: "job:top", timeout: time.Minute}

    timeoutErr := runner.timeoutError(entry, true, nil)

    if ErrCommandTimeout != errors.Unwrap(timeoutErr) {
        t.Fatalf("expected the sentinel as the sole cause, got %v", errors.Unwrap(timeoutErr))
    }
}

/* TestInvoke_ACommandFailureAndAScopeCloseFailureBothSurvive pins the shape that replaced the join at the scope-close boundary. The command's own failure stays the wrapped one, so its context and its whole cause chain still reach the record; the close failure travels beside it in the context, where a join would have emptied both — errors.Join answers an Unwrap of []error and exception.LogContext anchors cause and causeChain on the single-value form. */
func TestInvoke_ACommandFailureAndAScopeCloseFailureBothSurvive(t *testing.T) {
    job := newRecordingCommand("job:top")
    job.runErr = exception.NewError(
        "the nightly sweep could not reach its ledger",
        exceptioncontract.Context{"ledger": "acme-prod"},
        errors.New("connection refused"),
    )

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

    _, invokeErr := runner.invoke(runtimeInstance, runner.entries[0])
    if nil == invokeErr {
        t.Fatal("expected the run to fail")
    }

    if false == errors.Is(invokeErr, job.runErr) {
        t.Fatalf("expected the command's own failure to stay reachable, got %v", invokeErr)
    }

    logContext := exception.LogContext(invokeErr)

    causeChain, isChain := logContext["causeChain"].([]string)
    if false == isChain || 2 > len(causeChain) {
        t.Fatalf("expected the command's whole chain in the record, got %v", logContext["causeChain"])
    }

    if false == strings.Contains(strings.Join(causeChain, " | "), "connection refused") {
        t.Fatalf("expected the chain to reach what refused, got %v", causeChain)
    }

    if false == strings.Contains(fmt.Sprintf("%v", logContext["scopeCloseError"]), "child scope close failed") {
        t.Fatalf("expected the close failure beside it in the context, got %v", logContext["scopeCloseError"])
    }

    closeChain, isCloseChain := logContext["scopeCloseErrorCauseChain"].([]string)
    if false == isCloseChain || 0 == len(closeChain) {
        t.Fatalf("expected the close failure's own chain, got %v", logContext["scopeCloseErrorCauseChain"])
    }

    if false == strings.Contains(strings.Join(closeChain, " | "), "scope close failed") {
        t.Fatalf("expected the close failure's chain to reach its cause, got %v", closeChain)
    }
}

/* TestRunLoop_IsSilentOnAMinuteThatDispatchedNothing pins the default posture of the scheduler loop's document. A per-minute document for an entry that runs once a night would be 1439 lines a day saying nothing, so a minute that dispatched nothing is silent unless --report-idle asks for it. */
func TestRunLoop_IsSilentOnAMinuteThatDispatchedNothing(t *testing.T) {
    buffer, finished, cancel := driveOneIdleLoopMinute(t, false)
    defer cancel()

    <-finished

    if 0 != buffer.Len() {
        t.Fatalf("expected an idle minute to stay silent by default, got %q", buffer.String())
    }
}

/* TestRunLoop_ReportsAnIdleMinuteWhenAsked pins the flag: without it a consumer cannot tell a live scheduler with nothing due from a dead one, since both write nothing at all. */
func TestRunLoop_ReportsAnIdleMinuteWhenAsked(t *testing.T) {
    buffer, finished, cancel := driveOneIdleLoopMinute(t, true)
    defer cancel()

    <-finished

    if 0 == buffer.Len() {
        t.Fatalf("expected --report-idle to report the idle minute, got an empty stream")
    }

    document := struct {
        Data struct {
            Configured int              `json:"configured"`
            Ran        []map[string]any `json:"ran"`
        } `json:"data"`
    }{}
    if decodeErr := json.Unmarshal([]byte(strings.TrimSpace(buffer.String())), &document); nil != decodeErr {
        t.Fatalf("expected one json document per idle minute, got %q: %v", buffer.String(), decodeErr)
    }

    if 1 != document.Data.Configured {
        t.Fatalf("expected the configured count on an idle minute, got %d", document.Data.Configured)
    }

    if 0 != len(document.Data.Ran) {
        t.Fatalf("expected an idle minute to dispatch nothing, got %v", document.Data.Ran)
    }
}

/* driveOneIdleLoopMinute runs the scheduler loop across exactly one minute boundary on which no entry is due, and answers the loop's output buffer together with its completion. The clock trick is the one the loop's other tests use: the first reads sit just before the boundary, so the armed timer fires in milliseconds. */
func driveOneIdleLoopMinute(t *testing.T, reportIdle bool) (*bytes.Buffer, chan error, context.CancelFunc) {
    t.Helper()

    /* the entry is due at minute 30 and the loop crosses minute 10, so the minute dispatches nothing while the runner still drives one configured entry */
    configuration := NewConfiguration().
        Schedule("job:never", &EntryConfig{Schedule: &Schedule{Minute: "30"}})

    runner := NewRunnerCommand(configuration, RunnerDialectCrontab, newRecordingCommand("job:never"))

    idleMinute := time.Date(2026, time.July, 15, 9, 10, 0, 0, time.UTC)

    var nowCallCount atomic.Int32
    runner.now = func() time.Time {
        if 2 >= nowCallCount.Add(1) {
            return idleMinute.Add(-5 * time.Millisecond)
        }

        return idleMinute
    }

    buffer := &bytes.Buffer{}
    runner.reporting = &runReporting{
        commandContext: &clicontract.StaticContext{WriterValue: buffer},
        option:         output.NormalizeOption(output.Option{Format: output.FormatJson}),
        reportIdle:     reportIdle,
    }

    ctx, cancel := context.WithCancel(context.Background())

    finished := make(chan error, 1)
    go func() {
        finished <- runner.runLoop(newRunnerTestRuntime(ctx))
    }()

    /* the boundary is five milliseconds away; the wait is generous because what is asserted is what the loop wrote, not how fast it woke */
    time.Sleep(250 * time.Millisecond)
    cancel()

    return buffer, finished, cancel
}

/* the arguments field is a list on every row, empty rather than null for the entry that declares none — which is most of them. A field whose json type changes with the outcome cannot be consumed at all: `jq '.data.ran[].arguments | length'` died on the first argument-less job, in the one document of this family that was left unguarded while its siblings — error, ran, failed, pruned — each got a normalizer of their own. */
func TestRunnerCommand_TheArgumentsFieldIsAListOnEveryRow(t *testing.T) {
    /* the entry that declares an argument is given a command that DECLARES the flag it names: a scheduled child is parsed against its own flag set alone, so an entry naming a flag its command does not declare fails at parse and the row it produces is a failure rather than the ordinary row this asserts the shape of */
    withArguments := &outputWritingProbeCommand{commandName: "job:with"}
    withoutArguments := newRecordingCommand("job:without")

    configuration := NewConfiguration().
        Schedule("job:with", &EntryConfig{Schedule: &Schedule{Minute: "0"}, Arguments: []string{"--format=json"}}).
        Schedule("job:without", &EntryConfig{Schedule: &Schedule{Minute: "0"}})

    runner := NewRunnerCommand(configuration, RunnerDialectCrontab, withArguments, withoutArguments)
    runner.now = func() time.Time {
        return time.Date(2026, time.July, 15, 9, 0, 0, 0, time.UTC)
    }

    buffer := &bytes.Buffer{}

    cliCommand := newRunnerDispatch(runner, nil, buffer)

    if runErr := cliCommand.Run(context.Background(), []string{runner.Name(), "--once", "--format=json"}); nil != runErr {
        t.Fatalf("unexpected error running --once: %v", runErr)
    }

    document := struct {
        Data struct {
            Ran []struct {
                Command   string    `json:"command"`
                Arguments *[]string `json:"arguments"`
            } `json:"ran"`
        } `json:"data"`
    }{}
    if decodeErr := json.Unmarshal(buffer.Bytes(), &document); nil != decodeErr {
        t.Fatalf("expected one json document, got %q: %v", buffer.String(), decodeErr)
    }

    if 2 != len(document.Data.Ran) {
        t.Fatalf("expected both dispatched runs in the document, got %v", document.Data.Ran)
    }

    for _, row := range document.Data.Ran {
        if nil == row.Arguments {
            t.Fatalf("%s: expected an array, got a json null in %q", row.Command, buffer.String())
        }
    }

    for _, row := range document.Data.Ran {
        if "job:with" == row.Command && 1 != len(*row.Arguments) {
            t.Fatalf("expected the declared argument to travel, got %v", *row.Arguments)
        }

        if "job:without" == row.Command && 0 != len(*row.Arguments) {
            t.Fatalf("expected an empty list for the entry declaring none, got %v", *row.Arguments)
        }
    }
}

/* the classification the row is built from is the classification the record is filed under, because reportRunOutcome takes it instead of computing a second one. It is driven here with a LIVE runtime context and a cancellation error: recomputing would answer "not a shutdown" for both calls, so the run reported cancelled would be filed as a failure and counted as one — which is exactly what a SIGTERM landing between the two reads used to produce, a document saying failed over a process exiting 0. */
func TestRunnerCommand_ReportRunOutcomeTakesTheClassificationItIsGiven(t *testing.T) {
    runner := NewRunnerCommand(NewConfiguration(), RunnerDialectCrontab)
    entry := &scheduledRunEntry{commandName: "job:probe"}

    for _, testCase := range []struct {
        name            string
        cancelled       bool
        countsAsFailure bool
        message         string
    }{
        {
            name:            "a run classified as cancelled is a clean stop",
            cancelled:       true,
            countsAsFailure: false,
            message:         "cron: scheduled command cancelled by shutdown",
        },
        {
            name:            "a run classified as failed is counted",
            cancelled:       false,
            countsAsFailure: true,
            message:         "cron runner command failed",
        },
    } {
        t.Run(testCase.name, func(t *testing.T) {
            runtimeInstance, captured := newCapturingRunnerRuntime(context.Background())

            if nil != runtimeInstance.Context().Err() {
                t.Fatal("the runtime context must be live, or the two classifications cannot disagree")
            }

            countsAsFailure := runner.reportRunOutcome(runtimeInstance, entry, "run-probe", context.Canceled, testCase.cancelled)

            if testCase.countsAsFailure != countsAsFailure {
                t.Fatalf("counts towards the failure aggregate = %v, want %v", countsAsFailure, testCase.countsAsFailure)
            }

            if 1 != len(captured.entries) {
                t.Fatalf("expected exactly one record, got %d: %v", len(captured.entries), captured.entries)
            }

            if testCase.message != captured.entries[0].message {
                t.Fatalf("record = %q, want %q", captured.entries[0].message, testCase.message)
            }

            if "run-probe" != captured.entries[0].context["cronRunId"] {
                t.Fatalf("the record does not carry the run id: %v", captured.entries[0].context)
            }
        })
    }
}

/* a run that did not fail leaves no record and counts as nothing */
func TestRunnerCommand_ReportRunOutcomeIsSilentForASuccessfulRun(t *testing.T) {
    runner := NewRunnerCommand(NewConfiguration(), RunnerDialectCrontab)
    runtimeInstance, captured := newCapturingRunnerRuntime(context.Background())

    if true == runner.reportRunOutcome(runtimeInstance, &scheduledRunEntry{commandName: "job:probe"}, "run-probe", nil, false) {
        t.Fatal("a successful run must not count towards the failure aggregate")
    }

    if 0 != len(captured.entries) {
        t.Fatalf("a successful run must leave no record, got %v", captured.entries)
    }
}

/* the evaluated minute doubles as the document's timestamp, so the ordinary advance must carry the real local instant — offset and all — while only the minutes a forward jump skipped, which have no local representation, stay utc-materialized. The pseudo-utc rendering used to reach the document as a real instant, off by the whole zone offset and disagreeing with the --once mode. */
func TestReconcileWallClockReportsTheCurrentMinuteAsRealLocalTime(t *testing.T) {
    zone := time.FixedZone("probe", 3*3600)
    previousTarget := time.Date(2026, time.August, 15, 3, 29, 0, 0, zone)
    current := time.Date(2026, time.August, 15, 3, 30, 7, 0, zone)

    evaluations, nextTarget, note := reconcileWallClock(previousTarget, current)
    if "" != note {
        t.Fatalf("unexpected note: %s", note)
    }

    if 1 != len(evaluations) {
        t.Fatalf("expected one evaluation, got %d", len(evaluations))
    }

    expected := "2026-08-15T03:30:00+03:00"
    if rendered := evaluations[0].at.Format(time.RFC3339); expected != rendered {
        t.Fatalf("expected the real local minute %q, got %q", expected, rendered)
    }

    if rendered := nextTarget.Format(time.RFC3339); expected != rendered {
        t.Fatalf("expected the chain anchor to stay real local time, got %q", rendered)
    }
}

/* the fixed zone has no daylight-saving gap, so every minute a three-minute jump skips exists on its calendar and is reported as the real local instant; the pin that expected them utc-materialized was written on this very input, under a rationale — "no local representation to print" — that is true of a spring-forward gap only. The half of it that guards a real rule stays: a skipped minute never runs the wildcard class. */
func TestReconcileWallClock_CatchUpMinutesTheZoneHasAreRealLocalTime(t *testing.T) {
    zone := time.FixedZone("probe", 3*3600)
    previousTarget := time.Date(2026, time.August, 15, 3, 0, 0, 0, zone)
    current := time.Date(2026, time.August, 15, 3, 3, 2, 0, zone)

    evaluations, _, note := reconcileWallClock(previousTarget, current)
    if "" != note {
        t.Fatalf("unexpected note: %s", note)
    }

    expectedCurrent := "2026-08-15T03:03:00+03:00"
    expectedSkipped := []string{"2026-08-15T03:01:00+03:00", "2026-08-15T03:02:00+03:00"}

    skipped := []string{}
    fixedTimeAtCurrent := 0
    for _, evaluation := range evaluations {
        rendered := evaluation.at.Format(time.RFC3339)

        if rendered == expectedCurrent {
            if true == evaluation.runFixedTime {
                fixedTimeAtCurrent++
            }

            continue
        }

        if true == evaluation.runWildcard {
            t.Fatalf("a skipped minute must not run wildcard entries, got %q", rendered)
        }

        skipped = append(skipped, rendered)
    }

    if fmt.Sprintf("%v", expectedSkipped) != fmt.Sprintf("%v", skipped) {
        t.Fatalf("expected the skipped minutes as real local instants %v, got %v", expectedSkipped, skipped)
    }

    if 1 != fixedTimeAtCurrent {
        t.Fatalf("expected the current minute to run the fixed-time class exactly once as real local time, got %d", fixedTimeAtCurrent)
    }
}

func TestReconcileWallClockReportsTheRepeatedMinuteAsRealLocalTime(t *testing.T) {
    zone := time.FixedZone("probe", 3*3600)
    previousTarget := time.Date(2026, time.August, 15, 3, 30, 0, 0, zone)
    current := time.Date(2026, time.August, 15, 3, 30, 3, 0, zone)

    evaluations, nextTarget, note := reconcileWallClock(previousTarget, current)
    if "" != note {
        t.Fatalf("unexpected note: %s", note)
    }

    if 1 != len(evaluations) {
        t.Fatalf("expected one evaluation, got %d", len(evaluations))
    }

    if rendered := evaluations[0].at.Format(time.RFC3339); "2026-08-15T03:30:00+03:00" != rendered {
        t.Fatalf("expected the repeated minute as real local time, got %q", rendered)
    }

    if false == nextTarget.Equal(previousTarget) {
        t.Fatal("a repeated minute must leave the chain anchored")
    }
}

/* two entries share the minute and run concurrently, so completion order is scheduling luck; the document orders its rows by command name, which is the property that lets two identical minutes be diffed. The pin is deterministic — it asserts the ORDER of both rows, not the effect of a sort a lucky iteration could reproduce — and neither frozen major carries it. */
func TestDispatchDue_TheDocumentOrdersRunsByCommandName(t *testing.T) {
    first := newRecordingCommand("job:alpha")
    second := newRecordingCommand("job:beta")

    configuration := NewConfiguration().
        Schedule("job:beta", &EntryConfig{Schedule: &Schedule{Minute: "0"}}).
        Schedule("job:alpha", &EntryConfig{Schedule: &Schedule{Minute: "0"}})

    runner := NewRunnerCommand(configuration, RunnerDialectCrontab, first, second)

    at := time.Date(2026, time.July, 15, 9, 0, 0, 0, time.UTC)
    report, runErr := runner.dispatchDue(newRunnerTestRuntime(context.Background()), at, true, true)()
    if nil != runErr {
        t.Fatalf("unexpected error: %v", runErr)
    }

    if 2 != len(report.Ran) {
        t.Fatalf("expected both runs in the document, got %d", len(report.Ran))
    }

    if "job:alpha" != report.Ran[0].Command || "job:beta" != report.Ran[1].Command {
        t.Fatalf(
            "expected the rows ordered by command name whatever the completion order, got %q, %q",
            report.Ran[0].Command,
            report.Ran[1].Command,
        )
    }
}

/* TestRunnerCommand_TheConfiguredZoneDecidesWhatIsDue is the guard the whole zone declaration exists for, and it is written so that the two zones DISAGREE about the same instant. 09:00 UTC is 18:00 in Tokyo, and the entry is scheduled for eighteen hundred: under the configured zone it is due, under the process zone it is not. A probe whose zones agreed at the chosen instant would pass with the zone never applied at all. */
func TestRunnerCommand_TheConfiguredZoneDecidesWhatIsDue(t *testing.T) {
    job := newRecordingCommand("job:evening")

    configuration := NewConfiguration().
        InTimezone("Asia/Tokyo").
        Schedule("job:evening", &EntryConfig{Schedule: &Schedule{Minute: "0", Hour: "18"}})

    runner := NewRunnerCommand(configuration, RunnerDialectCrontab, job)
    runner.now = func() time.Time {
        return time.Date(2026, time.July, 15, 9, 0, 0, 0, time.UTC)
    }

    cliCommand := newRunnerDispatch(runner, nil, nil)

    if runErr := cliCommand.Run(context.Background(), []string{runner.Name(), "--once"}); nil != runErr {
        t.Fatalf("unexpected error running --once: %v", runErr)
    }

    if 1 != job.runCount {
        t.Fatalf("expected the eighteen-hundred entry to be due at 09:00 UTC under Asia/Tokyo, ran %d", job.runCount)
    }
}

/* the negative half of the pair: the SAME instant and the SAME schedule, with no zone declared, must NOT be due. Without it the test above would still pass on a runner that ran everything. */
func TestRunnerCommand_WithoutAZoneTheSameInstantIsNotDue(t *testing.T) {
    job := newRecordingCommand("job:evening")

    configuration := NewConfiguration().
        Schedule("job:evening", &EntryConfig{Schedule: &Schedule{Minute: "0", Hour: "18"}})

    runner := NewRunnerCommand(configuration, RunnerDialectCrontab, job)
    runner.now = func() time.Time {
        return time.Date(2026, time.July, 15, 9, 0, 0, 0, time.UTC)
    }

    cliCommand := newRunnerDispatch(runner, nil, nil)

    if runErr := cliCommand.Run(context.Background(), []string{runner.Name(), "--once"}); nil != runErr {
        t.Fatalf("unexpected error running --once: %v", runErr)
    }

    if 0 != job.runCount {
        t.Fatalf("expected the eighteen-hundred entry not to be due at 09:00 UTC, ran %d", job.runCount)
    }
}

/* the flag wins over the configuration: an operator running one invocation against another region's calendar says so at the invocation. The configuration names a zone under which the entry is NOT due, so only the flag can make it run. */
func TestRunnerCommand_TheTimezoneFlagWinsOverTheConfiguration(t *testing.T) {
    job := newRecordingCommand("job:evening")

    configuration := NewConfiguration().
        InTimezone("UTC").
        Schedule("job:evening", &EntryConfig{Schedule: &Schedule{Minute: "0", Hour: "18"}})

    runner := NewRunnerCommand(configuration, RunnerDialectCrontab, job)
    runner.now = func() time.Time {
        return time.Date(2026, time.July, 15, 9, 0, 0, 0, time.UTC)
    }

    cliCommand := newRunnerDispatch(runner, nil, nil)

    if runErr := cliCommand.Run(context.Background(), []string{runner.Name(), "--once", "--timezone=Asia/Tokyo"}); nil != runErr {
        t.Fatalf("unexpected error running --once: %v", runErr)
    }

    if 1 != job.runCount {
        t.Fatalf("expected the flag to override the configured zone, ran %d", job.runCount)
    }
}

/* a zone the standard library cannot load is a wiring mistake and fails at CONSTRUCTION, beside the malformed schedules and the unknown command names: a scheduler that fell back to the process zone would run every job at the right clock time in the wrong place, which nobody notices until the reports are wrong. */
func TestNewRunnerCommand_PanicsForAZoneItCannotLoad(t *testing.T) {
    configuration := NewConfiguration().
        InTimezone("Mars/Olympus_Mons").
        Schedule("job:top", &EntryConfig{Schedule: &Schedule{Minute: "0"}})

    defer func() {
        recovered := recover()
        if nil == recovered {
            t.Fatal("expected a panic for a zone that cannot be loaded")
        }

        recoveredErr, isError := recovered.(error)
        if false == isError {
            t.Fatalf("expected the panic to carry an error, got %T", recovered)
        }

        if false == errors.Is(recoveredErr, ErrUnknownTimezone) {
            t.Fatalf("expected ErrUnknownTimezone, got %v", recoveredErr)
        }
    }()

    _ = NewRunnerCommand(configuration, RunnerDialectCrontab, newRecordingCommand("job:top"))
}

/* a mistyped FLAG is what the caller typed, so it is refused by name and the command carries the refusal out — a stack trace would be the wrong answer to a typing mistake. */
func TestRunnerCommand_RefusesATimezoneFlagItCannotLoad(t *testing.T) {
    job := newRecordingCommand("job:top")

    configuration := NewConfiguration().
        Schedule("job:top", &EntryConfig{Schedule: &Schedule{Minute: "0"}})

    runner := NewRunnerCommand(configuration, RunnerDialectCrontab, job)
    runner.now = func() time.Time {
        return time.Date(2026, time.July, 15, 9, 0, 0, 0, time.UTC)
    }

    cliCommand := newRunnerDispatch(runner, nil, nil)

    runErr := cliCommand.Run(context.Background(), []string{runner.Name(), "--once", "--timezone=Mars/Olympus_Mons"})
    if false == errors.Is(runErr, ErrUnknownTimezone) {
        t.Fatalf("expected ErrUnknownTimezone, got %v", runErr)
    }

    if 0 != job.runCount {
        t.Fatalf("expected nothing to run under a zone that was refused, ran %d", job.runCount)
    }
}

/* ignoringCommand blocks until released and never looks at its context: the shape of a job wedged on a deadline-less read. */
type ignoringCommand struct {
    commandName string
    started     chan struct{}
    release     chan struct{}
    completed   atomic.Int64
}

func newIgnoringCommand(name string) *ignoringCommand {
    return &ignoringCommand{commandName: name, started: make(chan struct{}, 1), release: make(chan struct{})}
}

func (instance *ignoringCommand) Name() string {
    return instance.commandName
}

func (instance *ignoringCommand) Description() string {
    return "ignoring command"
}

func (instance *ignoringCommand) Flags() []clicontract.Flag {
    return nil
}

func (instance *ignoringCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error {
    select {
    case instance.started <- struct{}{}:
    default:
    }

    <-instance.release

    instance.completed.Add(1)

    return nil
}

func (instance *ignoringCommand) awaitStart(t *testing.T) {
    t.Helper()

    select {
    case <-instance.started:
    case <-time.After(3 * time.Second):
        t.Fatal("expected the job to start")
    }
}

/* awaitWithin runs the given call on a goroutine and fails the test by name when it does not return inside the budget, so a bound the runner lost fails on the timer instead of hanging the suite. */
func awaitWithin(t *testing.T, budget time.Duration, label string, run func()) {
    t.Helper()

    done := make(chan struct{})
    go func() {
        run()
        close(done)
    }()

    select {
    case <-done:
    case <-time.After(budget):
        t.Fatalf("%s did not return within %s", label, budget)
    }
}

func newLoggingRunnerTestRuntime(ctx context.Context, logger loggingcontract.Logger) runtimecontract.Runtime {
    serviceContainer := container.NewContainer()
    serviceContainer.MustRegister(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return logger, nil
        },
    )

    return runtime.New(ctx, serviceContainer.NewScope(), serviceContainer)
}

func TestWallMinuteTime_AMinuteInsideTheSpringForwardGapStaysUtcMaterialized(t *testing.T) {
    bucharest, locationErr := time.LoadLocation("Europe/Bucharest")
    if nil != locationErr {
        t.Fatalf("could not load the location: %v", locationErr)
    }

    /* 2026-03-29 03:30 does not exist in Bucharest: the clocks go from 03:00 to 04:00 */
    gapIndex := wallMinuteIndex(time.Date(2026, time.March, 29, 3, 30, 0, 0, time.UTC))

    materialized := wallMinuteTime(gapIndex, bucharest)
    if rendered := materialized.Format(time.RFC3339); "2026-03-29T03:30:00Z" != rendered {
        t.Fatalf("expected the gap minute utc-materialized with its own fields, got %q", rendered)
    }

    matcher, parseErr := newScheduleMatcher(&Schedule{Minute: "30", Hour: "3"}, RunnerDialectCrontab)
    if nil != parseErr {
        t.Fatalf("unexpected error: %v", parseErr)
    }

    if false == matcher.Matches(materialized) {
        t.Fatal("a fixed-time entry pinned inside the gap must still match the materialized minute")
    }
}

func TestWallMinuteTime_AMinuteTheZoneHasIsItsLocalInstant(t *testing.T) {
    bucharest, locationErr := time.LoadLocation("Europe/Bucharest")
    if nil != locationErr {
        t.Fatalf("could not load the location: %v", locationErr)
    }

    index := wallMinuteIndex(time.Date(2026, time.March, 29, 4, 1, 0, 0, bucharest))

    if rendered := wallMinuteTime(index, bucharest).Format(time.RFC3339); "2026-03-29T04:01:00+03:00" != rendered {
        t.Fatalf("expected the real local instant, got %q", rendered)
    }
}

/* a suspend that spans the spring-forward walks through both kinds of minute in one catch-up: the ones inside the gap have no local instant and stay utc-materialized, the ones after it are real local time. */
func TestReconcileWallClock_ACatchUpAcrossTheSpringForwardKeepsOnlyTheGapUtc(t *testing.T) {
    bucharest, locationErr := time.LoadLocation("Europe/Bucharest")
    if nil != locationErr {
        t.Fatalf("could not load the location: %v", locationErr)
    }

    previousTarget := time.Date(2026, time.March, 29, 2, 59, 0, 0, bucharest)
    current := time.Date(2026, time.March, 29, 4, 2, 5, 0, bucharest)

    evaluations, _, note := reconcileWallClock(previousTarget, current)
    if "" != note {
        t.Fatalf("unexpected note: %s", note)
    }

    rendered := map[string]bool{}
    for _, evaluation := range evaluations {
        rendered[evaluation.at.Format(time.RFC3339)] = true
    }

    for _, expected := range []string{"2026-03-29T03:00:00Z", "2026-03-29T03:59:00Z", "2026-03-29T04:00:00+03:00", "2026-03-29T04:01:00+03:00", "2026-03-29T04:02:00+03:00"} {
        if false == rendered[expected] {
            t.Fatalf("expected %q among the evaluated minutes, got %v", expected, rendered)
        }
    }

    for _, unexpected := range []string{"2026-03-29T04:00:00Z", "2026-03-29T04:30:00+03:00"} {
        if true == rendered[unexpected] {
            t.Fatalf("did not expect %q among the evaluated minutes", unexpected)
        }
    }
}

func TestRunnerCommand_OnceReportsTheWallMinute(t *testing.T) {
    job := newRecordingCommand("job:top")

    configuration := NewConfiguration().
        Schedule("job:top", &EntryConfig{Schedule: &Schedule{Minute: "0"}})

    runner := NewRunnerCommand(configuration, RunnerDialectCrontab, job)
    runner.now = func() time.Time {
        return time.Date(2026, time.July, 15, 9, 0, 37, 0, time.FixedZone("probe", 3*3600))
    }

    buffer := &bytes.Buffer{}
    cliCommand := newRunnerDispatch(runner, nil, buffer)

    if runErr := cliCommand.Run(context.Background(), []string{runner.Name(), "--once", "--format=json"}); nil != runErr {
        t.Fatalf("unexpected error running --once: %v", runErr)
    }

    document := struct {
        Data struct {
            At string `json:"at"`
        } `json:"data"`
    }{}
    if decodeErr := json.Unmarshal(buffer.Bytes(), &document); nil != decodeErr {
        t.Fatalf("expected one json document, got %q: %v", buffer.String(), decodeErr)
    }

    if "2026-07-15T09:00:00+03:00" != document.Data.At {
        t.Fatalf("expected --once to report the wall minute the loop reports, got %q", document.Data.At)
    }
}

func TestRunnerCommand_ShutdownAbandonsAJobThatIgnoresItsCancellation(t *testing.T) {
    job := newIgnoringCommand("job:ignoring")
    defer close(job.release)

    configuration := NewConfiguration().
        Schedule("job:ignoring", &EntryConfig{})

    runner := NewRunnerCommand(configuration, RunnerDialectCrontab, job)
    runner.unwindGrace = 50 * time.Millisecond

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

    captured := &capturingRunnerLogger{}
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    finished := make(chan error, 1)
    go func() {
        finished <- runner.runLoop(newLoggingRunnerTestRuntime(ctx, captured))
    }()

    job.awaitStart(t)
    cancel()

    awaitWithin(t, 2*time.Second, "the loop over a job ignoring the shutdown", func() { <-finished })

    if 0 != job.completed.Load() {
        t.Fatal("the job must still be running when the loop returns: it was abandoned, not finished")
    }

    record, found := captured.recordByMessageProbe("cron: scheduled command ignored the shutdown cancellation for the whole graceful window and is being abandoned; its container scope is closed under it while it may still be running")
    if false == found {
        t.Fatalf("expected the shutdown abandon to be announced, got %v", captured.entries)
    }

    if (50 * time.Millisecond).String() != fmt.Sprintf("%v", record.context["gracefulTimeout"]) {
        t.Fatalf("expected the warning to name the window, got %v", record.context)
    }
}

func TestRunnerCommand_ShutdownAbandonsAJobWhoseDeadlineIsOutOfReach(t *testing.T) {
    job := newIgnoringCommand("job:ignoring")
    defer close(job.release)

    configuration := NewConfiguration().
        Schedule("job:ignoring", &EntryConfig{Schedule: &Schedule{Minute: "0"}, Timeout: time.Hour})

    runner := NewRunnerCommand(configuration, RunnerDialectCrontab, job)
    runner.unwindGrace = 50 * time.Millisecond

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    completed := make(chan error, 1)
    go func() {
        completed <- invokeDiscardingRunId(runner, newRunnerTestRuntime(ctx), runner.entries[0])
    }()

    job.awaitStart(t)
    cancel()

    var invokeErr error
    awaitWithin(t, 2*time.Second, "invoke over a job ignoring the shutdown", func() { invokeErr = <-completed })

    if false == errors.Is(invokeErr, ErrCommandTimeout) {
        t.Fatalf("expected the abandon to be classified with ErrCommandTimeout, got %v", invokeErr)
    }

    if true == errors.Is(invokeErr, context.Canceled) {
        t.Fatalf("an abandoned run must not read as a cancellation the command honoured, got %v", invokeErr)
    }

    if false == strings.Contains(invokeErr.Error(), "shutdown cancellation") {
        t.Fatalf("expected the failure to name the shutdown, got %v", invokeErr)
    }
}

func TestDispatchDue_AShutdownAbandonedRunIsFailedNotCancelled(t *testing.T) {
    job := newIgnoringCommand("job:ignoring")
    defer close(job.release)

    configuration := NewConfiguration().
        Schedule("job:ignoring", &EntryConfig{Schedule: &Schedule{Minute: "0"}})

    runner := NewRunnerCommand(configuration, RunnerDialectCrontab, job)
    runner.unwindGrace = 50 * time.Millisecond

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    at := time.Date(2026, time.July, 15, 9, 0, 0, 0, time.UTC)
    wait := runner.dispatchDue(newRunnerTestRuntime(ctx), at, true, true)

    job.awaitStart(t)
    cancel()

    var report dueReport
    var runErr error
    awaitWithin(t, 2*time.Second, "the --once wait over a job ignoring the shutdown", func() { report, runErr = wait() })

    if nil == runErr || false == errors.Is(runErr, ErrCommandTimeout) {
        t.Fatalf("expected the minute to aggregate the abandon as a failure, got %v", runErr)
    }

    if 1 != len(report.Ran) {
        t.Fatalf("expected one run in the document, got %v", report.Ran)
    }

    if false == report.Ran[0].Failed || true == report.Ran[0].Cancelled {
        t.Fatalf("an abandoned run is failed, not cancelled: got failed=%v cancelled=%v", report.Ran[0].Failed, report.Ran[0].Cancelled)
    }
}

/* the graceful window is a wait, not a kill: a job that unwinds inside it after the shutdown reports its own outcome. */
func TestRunnerCommand_ShutdownWaitsForAJobThatUnwindsInsideTheGracefulWindow(t *testing.T) {
    job := newIgnoringCommand("job:ignoring")

    configuration := NewConfiguration().
        Schedule("job:ignoring", &EntryConfig{Schedule: &Schedule{Minute: "0"}})

    runner := NewRunnerCommand(configuration, RunnerDialectCrontab, job)
    runner.unwindGrace = time.Second

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    completed := make(chan error, 1)
    go func() {
        completed <- invokeDiscardingRunId(runner, newRunnerTestRuntime(ctx), runner.entries[0])
    }()

    job.awaitStart(t)
    cancel()

    time.Sleep(100 * time.Millisecond)
    close(job.release)

    var invokeErr error
    awaitWithin(t, 3*time.Second, "invoke over a job unwinding inside the window", func() { invokeErr = <-completed })

    if nil != invokeErr {
        t.Fatalf("expected the job's own outcome, got %v", invokeErr)
    }

    if 1 != job.completed.Load() {
        t.Fatalf("expected the job to have completed once, got %d", job.completed.Load())
    }
}

func TestResolveAbandonedRun_AShutdownAbandonNamesTheShutdownAndIsNotACancellation(t *testing.T) {
    configuration := NewConfiguration().
        Schedule("job:top", &EntryConfig{Schedule: &Schedule{Minute: "0"}})

    runner := NewRunnerCommand(configuration, RunnerDialectCrontab, newRecordingCommand("job:top"))
    entry := runner.entries[0]

    ctx, cancel := context.WithCancel(context.Background())
    cancel()

    runtimeInstance := newRunnerTestRuntime(ctx)

    resolved := runner.resolveAbandonedRun(runtimeInstance, entry, "test-run-id", ctx, make(chan error, 1), abandonedAfterShutdown)
    if nil == resolved {
        t.Fatal("expected the abandoned run to be reported")
    }

    if false == strings.Contains(resolved.Error(), "shutdown cancellation") {
        t.Fatalf("expected the failure to name the shutdown, got %v", resolved)
    }

    if false == errors.Is(resolved, ErrCommandTimeout) {
        t.Fatalf("expected the failure to carry the timeout classification, got %v", resolved)
    }

    if true == isShutdownCancellation(runtimeInstance, resolved) {
        t.Fatal("an abandoned run must not be classified as a shutdown cancellation the command honoured")
    }
}

/* panickingScopeContainer refuses the FIRST scope asked of it and answers the rest, so the minute-mate of the refused run still gets one. */
type panickingScopeContainer struct {
    containercontract.Container
    refused *atomic.Bool
}

func (instance panickingScopeContainer) NewScope() containercontract.Scope {
    if true == instance.refused.CompareAndSwap(false, true) {
        panic("the application's container refused a scope")
    }

    return instance.Container.NewScope()
}

type panickingCloseScope struct {
    containercontract.Scope
}

func (instance panickingCloseScope) Close() error {
    panic("the application's scope panicked on close")
}

type panickingCloseContainer struct {
    containercontract.Container
}

func (instance panickingCloseContainer) NewScope() containercontract.Scope {
    return panickingCloseScope{Scope: instance.Container.NewScope()}
}

/* panickingLogger panics on every record: the shape of an application logger whose sink is gone. */
type panickingLogger struct{}

func (instance *panickingLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
    panic("the application's logger panicked on " + message)
}

func (instance *panickingLogger) Debug(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelDebug, message, context)
}

func (instance *panickingLogger) Info(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelInfo, message, context)
}

func (instance *panickingLogger) Warning(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelWarning, message, context)
}

func (instance *panickingLogger) Error(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelError, message, context)
}

func (instance *panickingLogger) Emergency(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelEmergency, message, context)
}

/* errorWithPanickingText is a failure whose Error() panics, the shape the container's own teardown guards against. */
type errorWithPanickingText struct{}

func (instance errorWithPanickingText) Error() string {
    panic("the application's error panicked rendering itself")
}

func dispatchOnePanicProbe(t *testing.T, runtimeInstance runtimecontract.Runtime, job clicontract.Command) (dueReport, error, *recordingCommand) {
    t.Helper()

    healthy := newRecordingCommand("job:healthy")

    configuration := NewConfiguration().
        Schedule(job.Name(), &EntryConfig{Schedule: &Schedule{Minute: "0"}}).
        Schedule("job:healthy", &EntryConfig{Schedule: &Schedule{Minute: "0"}})

    runner := NewRunnerCommand(configuration, RunnerDialectCrontab, job, healthy)

    at := time.Date(2026, time.July, 15, 9, 0, 0, 0, time.UTC)
    wait := runner.dispatchDue(runtimeInstance, at, true, true)

    var report dueReport
    var runErr error
    awaitWithin(t, 2*time.Second, "the minute's wait", func() { report, runErr = wait() })

    return report, runErr, healthy
}

func assertRunnerPanicFiled(t *testing.T, report dueReport, runErr error, healthy *recordingCommand, commandName string, panicText string) {
    t.Helper()

    if nil == runErr {
        t.Fatal("expected the minute to aggregate the contained panic as a failure")
    }

    aggregate, isExceptionError := runErr.(*exception.Error)
    if false == isExceptionError {
        t.Fatalf("expected an exception error, got %T", runErr)
    }

    if false == strings.Contains(fmt.Sprintf("%v", aggregate.Context()["commands"]), commandName) {
        t.Fatalf("expected %q among the failed commands, got %v", commandName, aggregate.Context()["commands"])
    }

    var filed *exception.Error
    found := false
    for _, run := range report.Ran {
        if run.Command != commandName {
            continue
        }

        found = true

        if false == run.Failed || true == run.Cancelled {
            t.Fatalf("expected the run filed as failed, got failed=%v cancelled=%v", run.Failed, run.Cancelled)
        }

        if "" == run.RunId {
            t.Fatal("expected the filed row to carry the run id")
        }

        if false == strings.Contains(run.Error, "runner panicked") {
            t.Fatalf("expected the row to name the contained panic, got %q", run.Error)
        }
    }

    if false == found {
        t.Fatalf("expected %q in the document, got %v", commandName, report.Ran)
    }

    /* the aggregate wraps the join of the minute's failures; the filed one is the first exception error beneath it */
    if false == errors.As(errors.Unwrap(runErr), &filed) {
        t.Fatalf("expected the contained panic beneath the aggregate, got %v", runErr)
    }

    if false == strings.Contains(fmt.Sprintf("%v", filed.Context()["panicValue"]), panicText) {
        t.Fatalf("expected the panic value in the failure's context, got %v", filed.Context())
    }

    if 1 != healthy.runCount {
        t.Fatalf("expected the healthy command to still run, ran %d", healthy.runCount)
    }
}

/* the two runs of the minute race for the one refused scope, so the assertion is on the shape of the minute, not on which entry drew it: one run filed as the runner's own panic, the other completed. */
func TestDispatchDue_APanicPreparingARunIsFiledAsThatRunsFailure(t *testing.T) {
    serviceContainer := container.NewContainer()
    runtimeInstance := runtime.New(context.Background(), serviceContainer.NewScope(), panickingScopeContainer{Container: serviceContainer, refused: &atomic.Bool{}})

    first := newRecordingCommand("job:one")
    report, runErr, healthy := dispatchOnePanicProbe(t, runtimeInstance, first)

    if nil == runErr || false == strings.Contains(runErr.Error(), "scheduled commands failed") {
        t.Fatalf("expected the minute to aggregate the contained panic, got %v", runErr)
    }

    if 2 != len(report.Ran) {
        t.Fatalf("expected both runs in the document, got %v", report.Ran)
    }

    failed := 0
    for _, run := range report.Ran {
        if false == run.Failed {
            continue
        }

        failed++

        if "" == run.RunId || false == strings.Contains(run.Error, "runner panicked") {
            t.Fatalf("expected the refused run filed with its id and the contained panic, got %+v", run)
        }
    }

    if 1 != failed {
        t.Fatalf("expected exactly one run filed as the runner's panic, got %d", failed)
    }

    if 1 != first.runCount+healthy.runCount {
        t.Fatalf("expected the run that got a scope to complete, ran %d and %d", first.runCount, healthy.runCount)
    }

    var filed *exception.Error
    if false == errors.As(errors.Unwrap(runErr), &filed) || false == strings.Contains(fmt.Sprintf("%v", filed.Context()["panicValue"]), "refused a scope") {
        t.Fatalf("expected the panic value beneath the aggregate, got %v", runErr)
    }
}

func TestDispatchDue_APanicClosingTheChildScopeIsFiledAsThatRunsFailure(t *testing.T) {
    serviceContainer := container.NewContainer()
    runtimeInstance := runtime.New(context.Background(), serviceContainer.NewScope(), panickingCloseContainer{Container: serviceContainer})

    report, runErr, healthy := dispatchOnePanicProbe(t, runtimeInstance, newRecordingCommand("job:one"))

    assertRunnerPanicFiled(t, report, runErr, healthy, "job:one", "panicked on close")
}

func TestDispatchDue_APanicInTheLoggerIsFiledAsThatRunsFailure(t *testing.T) {
    failing := newRecordingCommand("job:one")
    failing.runErr = errors.New("job failed")

    report, runErr, healthy := dispatchOnePanicProbe(t, newLoggingRunnerTestRuntime(context.Background(), &panickingLogger{}), failing)

    assertRunnerPanicFiled(t, report, runErr, healthy, "job:one", "logger panicked")
}

func TestDispatchDue_AFailureWhoseTextPanicsDoesNotHoldTheMinute(t *testing.T) {
    failing := newRecordingCommand("job:one")
    failing.runErr = errorWithPanickingText{}

    report, runErr, healthy := dispatchOnePanicProbe(t, newRunnerTestRuntime(context.Background()), failing)

    assertRunnerPanicFiled(t, report, runErr, healthy, "job:one", "rendering itself")
}

/* an application that registers its logger under the concrete type of its provider refuses the runner's per-run logger override — the cli entry point refuses the same way — and every run used to take the process down with it. */
func TestDispatchDue_ALoggerRegisteredUnderItsConcreteTypeFailsTheRunNotTheProcess(t *testing.T) {
    serviceContainer := container.NewContainer()
    serviceContainer.MustRegister(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (*capturingRunnerLogger, error) {
            return &capturingRunnerLogger{}, nil
        },
    )
    runtimeInstance := runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)

    report, runErr, healthy := dispatchOnePanicProbe(t, runtimeInstance, newRecordingCommand("job:one"))

    if nil == runErr {
        t.Fatal("expected the refused override to be filed as a failure")
    }

    for _, run := range report.Ran {
        if false == run.Failed {
            t.Fatalf("expected every run to be filed as failed under the refused override, got %v", report.Ran)
        }
    }

    if 0 != healthy.runCount {
        t.Fatalf("the healthy command cannot run either: its own override is refused the same way, ran %d", healthy.runCount)
    }
}

/* the budget lands in the middle of a two-byte rune: what is kept must still be valid text, so the cut backs off to the rune's start and the whole rune counts as dropped. */
func TestScheduledOutputCapture_CutsOnARuneBoundary(t *testing.T) {
    capture := newScheduledOutputCapture()

    filler := strings.Repeat("a", scheduledOutputCaptureLimit-1)
    if _, writeErr := capture.Write([]byte(filler)); nil != writeErr {
        t.Fatalf("unexpected error: %v", writeErr)
    }

    written, writeErr := capture.Write([]byte("é"))
    if nil != writeErr || 2 != written {
        t.Fatalf("expected the full length reported, got %d, %v", written, writeErr)
    }

    kept, dropped := capture.captured()
    if false == utf8.ValidString(kept) {
        t.Fatal("expected the kept output to be valid utf-8")
    }

    if scheduledOutputCaptureLimit-1 != len(kept) {
        t.Fatalf("expected the cut backed off to the rune start, kept %d bytes", len(kept))
    }

    if 2 != dropped {
        t.Fatalf("expected the whole split rune counted as dropped, got %d", dropped)
    }
}

/* the contained panic is also recorded, when the logger is not the collaborator that panicked: the row and the aggregate carry it regardless, the record is the operator's line. */
func TestDispatchDue_AContainedPanicIsRecordedWhenTheLoggerCanWrite(t *testing.T) {
    captured := &capturingRunnerLogger{}
    serviceContainer := container.NewContainer()
    serviceContainer.MustRegister(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return captured, nil
        },
    )
    runtimeInstance := runtime.New(context.Background(), serviceContainer.NewScope(), panickingCloseContainer{Container: serviceContainer})

    _, runErr, _ := dispatchOnePanicProbe(t, runtimeInstance, newRecordingCommand("job:one"))
    if nil == runErr {
        t.Fatal("expected the contained panic aggregated as a failure")
    }

    record, found := captured.recordByMessageProbe("cron runner command failed")
    if false == found {
        t.Fatalf("expected the contained panic recorded, got %v", captured.entries)
    }

    if false == strings.Contains(fmt.Sprintf("%v", record.context), "panicked on close") {
        t.Fatalf("expected the record to carry the panic value, got %v", record.context)
    }
}
