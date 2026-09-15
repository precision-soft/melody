package cron

import (
    "context"
    "errors"
    "fmt"
    "io"
    "math"
    "reflect"
    "runtime/debug"
    "sort"
    "strings"
    "sync"
    "time"
    "unicode/utf8"

    "github.com/precision-soft/melody/v3/application"
    melodycli "github.com/precision-soft/melody/v3/cli"
    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/cli/output"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/precision-soft/melody/v3/logging"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

const (
    flagNameOnce       = "once"
    flagNameReportIdle = "report-idle"
    flagNameTimezone   = "timezone"
)

type scheduledRunEntry struct {
    commandName string
    command     clicontract.Command
    arguments   []string

    schedule        string
    matcher         *scheduleMatcher
    fixedTime       bool
    timeout         time.Duration
    gracefulTimeout time.Duration
}

const defaultCommandTimeout = 0

const commandUnwindGrace = 300 * time.Second

/* RunnerCommand evaluates the cron Configuration in-process and dispatches registered commands when due. RunnerDialect must match the generated manifests: crontab uses Vixie day-field rules, while kubernetes uses robfig rules. Both combine two restricted day fields with OR. Vixie treats star-based day fields as unrestricted; robfig does so only for a plain or unit-stepped wildcard, including one in a list.

   Due runs are concurrent and may overlap earlier runs of the same entry. EntryConfig.Timeout optionally bounds a run; expiry or runner shutdown cancels its context. After EntryConfig.GracefulTimeout, an unfinished run is reported, its scope is closed, and shutdown stops waiting for it. The goroutine itself is not terminated.

   Wall-clock jumps follow the reconciliation policy in reconcileWallClock. Per-entry serialization and multi-instance coordination are composition decisions: wrap commands with lock.NewExclusiveCommand or gate the runner with lock.LeaderGate. */
type RunnerCommand struct {
    entries []*scheduledRunEntry
    now     func() time.Time

    location    *time.Location
    unwindGrace time.Duration
    inFlight    sync.WaitGroup

    writeMutex sync.Mutex

    reporting           *runReporting
    userIgnoredCommands []string
}

type runReporting struct {
    commandContext clicontract.Context
    option         output.Option
    reportIdle     bool
}

/* NewRunnerCommand resolves names and parses schedules at construction. Unknown dialects, duplicate command names, missing commands and malformed schedules panic. Custom argv, multiple instances and destination-file routing require an external scheduler and are refused. Entry users remain accepted, but Run warns that all in-process jobs use the process user. */
func NewRunnerCommand(configuration *Configuration, dialect RunnerDialect, commands ...clicontract.Command) *RunnerCommand {
    if nil == configuration {
        configuration = NewConfiguration()
    }

    if _, dialectErr := resolveRunnerDialect(dialect); nil != dialectErr {
        exception.Panic(dialectErr)
    }

    lookup := make(map[string]clicontract.Command, len(commands))
    for _, command := range commands {
        if nil == command {
            continue
        }

        if _, alreadyRegistered := lookup[command.Name()]; true == alreadyRegistered {
            exception.Panic(
                exception.NewError(
                    "cron: two runner commands share one name; keeping the later one would silently drop the other (an exclusivity wrapper over its wrapped command, most likely)",
                    exceptioncontract.Context{
                        "commandName": command.Name(),
                    },
                    ErrDuplicateRunnerCommand,
                ),
            )
        }

        lookup[command.Name()] = command
    }

    userIgnoredCommands := make([]string, 0)

    entries := make([]*scheduledRunEntry, 0, len(configuration.Entries()))
    for _, scheduled := range configuration.Entries() {
        if nil != scheduled.Config && "" != scheduled.Config.User {
            userIgnoredCommands = append(userIgnoredCommands, scheduled.CommandName)
        }

        if nil != scheduled.Config && 0 < len(scheduled.Config.Command) {
            exception.Panic(
                exception.NewError(
                    "cron: the in-process runner supports only name-scheduled single-instance entries; the entry configures a custom argv (EntryConfig.Command), which only the generated manifests can run",
                    exceptioncontract.Context{
                        "commandName": scheduled.CommandName,
                        "command":     strings.Join(scheduled.Config.Command, " "),
                    },
                    ErrUnsupportedRunnerEntry,
                ),
            )
        }

        if nil != scheduled.Config && 1 < scheduled.Config.Instances {
            exception.Panic(
                exception.NewError(
                    "cron: the in-process runner supports only name-scheduled single-instance entries; the entry configures more than one instance (EntryConfig.Instances), which only the generated manifests can run",
                    exceptioncontract.Context{
                        "commandName": scheduled.CommandName,
                        "instances":   scheduled.Config.Instances,
                    },
                    ErrUnsupportedRunnerEntry,
                ),
            )
        }

        if nil != scheduled.Config && "" != scheduled.Config.DestinationFile {
            exception.Panic(
                exception.NewError(
                    "cron: the in-process runner supports only name-scheduled single-instance entries; the entry routes to another crontab file (EntryConfig.DestinationFile), which addresses an external scheduler — run here as well, it would execute twice whenever the generated manifests are live",
                    exceptioncontract.Context{
                        "commandName":     scheduled.CommandName,
                        "destinationFile": scheduled.Config.DestinationFile,
                    },
                    ErrUnsupportedRunnerEntry,
                ),
            )
        }

        command, resolved := lookup[scheduled.CommandName]
        if false == resolved {
            exception.Panic(
                exception.NewError(
                    "cron: scheduled command has no matching registered command",
                    exceptioncontract.Context{
                        "commandName": scheduled.CommandName,
                    },
                    ErrUnknownScheduledCommand,
                ),
            )
        }

        matcher, matcherErr := newScheduleMatcher(scheduleOfEntry(scheduled), dialect)
        if nil != matcherErr {
            exception.Panic(
                exception.NewError(
                    "cron: scheduled command has an invalid schedule",
                    exceptioncontract.Context{
                        "commandName": scheduled.CommandName,
                    },
                    matcherErr,
                ),
            )
        }

        entries = append(entries, &scheduledRunEntry{
            commandName:     scheduled.CommandName,
            command:         command,
            arguments:       argumentsOfEntry(scheduled),
            schedule:        scheduleOfEntry(scheduled).Expression(),
            matcher:         matcher,
            fixedTime:       matcher.fixedTime(),
            timeout:         timeoutOfEntry(scheduled),
            gracefulTimeout: gracefulTimeoutOfEntry(scheduled),
        })
    }

    return &RunnerCommand{
        entries:             entries,
        now:                 time.Now,
        location:            mustLoadConfiguredLocation(configuration.TimezoneName()),
        unwindGrace:         commandUnwindGrace,
        userIgnoredCommands: userIgnoredCommands,
    }
}

func mustLoadConfiguredLocation(name string) *time.Location {
    if "" == name {
        return nil
    }

    location, loadErr := loadLocation(name)
    if nil != loadErr {
        exception.Panic(exception.FromError(loadErr))
    }

    return location
}

func loadLocation(name string) (*time.Location, error) {
    location, loadErr := time.LoadLocation(name)
    if nil != loadErr {
        return nil, exception.NewError(
            ErrUnknownTimezone.Error(),
            exceptioncontract.Context{"timezone": name},
            ErrUnknownTimezone,
        )
    }

    return location, nil
}

func (instance *RunnerCommand) nowInZone() time.Time {
    now := instance.now()

    if nil == instance.location {
        return now
    }

    return now.In(instance.location)
}

func scheduleOfEntry(scheduled *ScheduledCommand) *Schedule {
    if nil == scheduled.Config {
        return nil
    }

    return scheduled.Config.Schedule
}

func argumentsOfEntry(scheduled *ScheduledCommand) []string {
    if nil == scheduled.Config {
        return nil
    }

    return scheduled.Config.Arguments
}

func timeoutOfEntry(scheduled *ScheduledCommand) time.Duration {
    if nil == scheduled.Config || 0 == scheduled.Config.Timeout {
        return defaultCommandTimeout
    }

    return scheduled.Config.Timeout
}

func gracefulTimeoutOfEntry(scheduled *ScheduledCommand) time.Duration {
    if nil == scheduled.Config || 0 >= scheduled.Config.GracefulTimeout {
        return 0
    }

    return scheduled.Config.GracefulTimeout
}

func (instance *RunnerCommand) gracefulTimeoutOf(entry *scheduledRunEntry) time.Duration {
    if 0 < entry.gracefulTimeout {
        return entry.gracefulTimeout
    }

    return instance.unwindGrace
}

func (instance *RunnerCommand) Name() string {
    return "melody:cron:run"
}

func (instance *RunnerCommand) Description() string {
    return "Run the cron Configuration in-process, invoking each scheduled command when it is due"
}

/* Flags includes standard output and verbosity flags. Each dispatched minute produces the selected output envelope. */
func (instance *RunnerCommand) Flags() []clicontract.Flag {
    return output.MergeFlags(
        output.StandardFlags(),
        []clicontract.Flag{
            &clicontract.BoolFlag{
                Name:  flagNameOnce,
                Usage: "evaluate every schedule once against the current time, run the commands that are due, and exit instead of running the scheduler loop",
            },
            &clicontract.BoolFlag{
                Name:  flagNameReportIdle,
                Usage: "in the scheduler loop, report every evaluated minute instead of only the ones that dispatched a command, so a consumer can tell a live scheduler with nothing due from a dead one",
            },
            &clicontract.StringFlag{
                Name:  flagNameTimezone,
                Usage: "the IANA zone name the schedules are evaluated under (for example Europe/Bucharest), overriding the one the configuration declared; unset, the configuration's zone applies, and without one the process zone does",
                Value: "",
            },
        },
    )
}

func (instance *RunnerCommand) Run(
    runtimeInstance runtimecontract.Runtime,
    commandContext clicontract.Context,
) error {
    if 0 < len(instance.userIgnoredCommands) {
        if logger := logging.LoggerFromRuntime(runtimeInstance); nil != logger {
            logger.Warning(
                "cron runner ignores EntryConfig.User: in-process every job runs as the process user, unlike the generated manifests which run it as the configured one",
                map[string]any{"commands": strings.Join(instance.userIgnoredCommands, ", ")},
            )
        }
    }

    instance.declareSchedule(runtimeInstance)

    option := output.NormalizeOption(output.ParseOptionFromCommand(commandContext))

    if timezoneName := commandContext.String(flagNameTimezone); "" != timezoneName {
        location, loadErr := loadLocation(timezoneName)
        if nil != loadErr {
            return loadErr
        }

        instance.location = location
    }

    if true == commandContext.Bool(flagNameOnce) {

        startedAt := instance.nowInZone()
        report, runErr := instance.dispatchDue(runtimeInstance, startedAt.Truncate(time.Minute), true, true)()

        return instance.renderRunReport(commandContext, option, startedAt, report, runErr)
    }

    instance.reporting = &runReporting{
        commandContext: commandContext,
        option:         option,
        reportIdle:     commandContext.Bool(flagNameReportIdle),
    }

    return instance.runLoop(runtimeInstance)
}

func (instance *RunnerCommand) declareSchedule(runtimeInstance runtimecontract.Runtime) {
    logger := logging.LoggerFromRuntime(runtimeInstance)
    if nil == logger {
        return
    }

    if 0 == len(instance.entries) {
        logger.Warning(
            "cron runner drives no scheduled entries: nothing will ever be dispatched",
            map[string]any{"entries": 0},
        )

        return
    }

    scheduled := make([]map[string]any, 0, len(instance.entries))
    for _, entry := range instance.entries {
        scheduled = append(scheduled, map[string]any{
            "command":   entry.commandName,
            "schedule":  entry.schedule,
            "arguments": entry.arguments,
        })
    }

    logger.Info(
        "cron runner drives the scheduled entries",
        map[string]any{
            "entries":   len(instance.entries),
            "scheduled": scheduled,
        },
    )
}

func (instance *RunnerCommand) renderRunReport(
    commandContext clicontract.Context,
    option output.Option,
    startedAt time.Time,
    report dueReport,
    runErr error,
) error {
    instance.writeMutex.Lock()
    defer instance.writeMutex.Unlock()

    if false == output.IsJsonFormat(option.Format) {
        _, _ = fmt.Fprintf(
            commandContext.Writer(),
            "%s  dispatched %d of %d configured entries%s\n",
            report.At.Format(time.RFC3339),
            len(report.Ran),
            report.Configured,
            failedSuffixOf(report),
        )

        return runErr
    }

    meta := output.NewMeta(instance.Name(), nil, option, startedAt, time.Since(startedAt), output.Version{})
    envelope := output.NewEnvelope(meta)

    envelope.Data = report

    if 0 == report.Configured {
        envelope.Warnings = append(
            envelope.Warnings,
            output.NewWarning(
                "cron.nothingScheduled",
                "the cron runner drives no scheduled entries: nothing will ever be dispatched",
                nil,
            ),
        )
    }

    if nil != runErr {
        envelope.SetError(
            "cron.runFailed",
            "one or more scheduled commands failed",
            map[string]any{"failed": failedCommandsOf(report)},
            output.NewErrorCause(runErr.Error(), nil),
        )
    }

    renderErr := output.Render(commandContext.Writer(), envelope, option)
    if nil != runErr {
        return runErr
    }

    return renderErr
}

func failedCommandsOf(report dueReport) []string {
    failed := make([]string, 0)
    for _, run := range report.Ran {
        if true == run.Failed {
            failed = append(failed, run.Command)
        }
    }

    return failed
}

func failedSuffixOf(report dueReport) string {
    failed := failedCommandsOf(report)
    if 0 == len(failed) {
        return ""
    }

    return ", failed: " + strings.Join(failed, ", ")
}

func (instance *RunnerCommand) runLoop(runtimeInstance runtimecontract.Runtime) error {
    now := instance.nowInZone()
    previousTarget := now.Truncate(time.Minute)
    dispatchedIndex := wallMinuteIndex(previousTarget)

    reporting := instance.reporting

    for {
        nextWake := now.Truncate(time.Minute).Add(time.Minute)

        timer := time.NewTimer(nextWake.Sub(now))

        select {
        case <-runtimeInstance.Context().Done():
            timer.Stop()

            instance.inFlight.Wait()

            return nil
        case <-timer.C:
        }

        now = instance.nowInZone()

        if wallMinuteIndex(nextWake) == dispatchedIndex {
            continue
        }

        evaluations, nextTarget, note := reconcileWallClock(previousTarget, nextWake)
        previousTarget = nextTarget
        dispatchedIndex = wallMinuteIndex(nextWake)

        if "" != note {
            if logger := logging.LoggerFromRuntime(runtimeInstance); nil != logger {
                logger.Warning("cron runner re-anchored the minute chain", map[string]any{"note": note})
            }
        }

        for _, evaluation := range evaluations {
            wait := instance.dispatchDue(runtimeInstance, evaluation.at, evaluation.runFixedTime, evaluation.runWildcard)

            if nil == reporting {
                continue
            }

            instance.inFlight.Add(1)

            go func(waitForMinute func() (dueReport, error), minuteStartedAt time.Time) {
                defer instance.inFlight.Done()

                report, runErr := waitForMinute()

                if 0 == len(report.Ran) && false == reporting.reportIdle {
                    return
                }

                _ = instance.renderRunReport(reporting.commandContext, reporting.option, minuteStartedAt, report, runErr)
            }(wait, instance.nowInZone())
        }
    }
}

func (instance *RunnerCommand) runDue(runtimeInstance runtimecontract.Runtime, at time.Time) error {
    _, runErr := instance.dispatchDue(runtimeInstance, at, true, true)()

    return runErr
}

type dueRun struct {
    Command              string   `json:"command"`
    Schedule             string   `json:"schedule"`
    Arguments            []string `json:"arguments"`
    RunId                string   `json:"runId"`
    DurationMilliseconds int64    `json:"durationMilliseconds"`
    Failed               bool     `json:"failed"`
    Cancelled            bool     `json:"cancelled"`
    Error                string   `json:"error"`
}

type dueReport struct {
    At         time.Time `json:"at"`
    Configured int       `json:"configured"`
    Ran        []dueRun  `json:"ran"`
}

func (instance *RunnerCommand) dispatchDue(
    runtimeInstance runtimecontract.Runtime,
    at time.Time,
    runFixedTime bool,
    runWildcard bool,
) func() (dueReport, error) {
    var launched sync.WaitGroup

    var failureMutex sync.Mutex
    failedCommands := make([]string, 0)
    failures := make([]error, 0)
    runs := make([]dueRun, 0)

    for _, entry := range instance.entries {
        if true == entry.fixedTime && false == runFixedTime {
            continue
        }

        if false == entry.fixedTime && false == runWildcard {
            continue
        }

        if false == entry.matcher.Matches(at) {
            continue
        }

        instance.inFlight.Add(1)
        launched.Add(1)

        go func(launchedEntry *scheduledRunEntry) {
            defer instance.inFlight.Done()
            defer launched.Done()

            runId := logging.GenerateProcessId()
            invokedAt := time.Now()

            row, failure, counted := instance.runDispatched(runtimeInstance, launchedEntry, runId, invokedAt)

            failureMutex.Lock()
            runs = append(runs, row)
            if true == counted {
                failedCommands = append(failedCommands, launchedEntry.commandName)
                failures = append(failures, failure)
            }
            failureMutex.Unlock()
        }(entry)
    }

    return func() (dueReport, error) {
        launched.Wait()

        failureMutex.Lock()
        report := dueReport{At: at, Configured: len(instance.entries), Ran: append([]dueRun{}, runs...)}
        aggregated := append([]error{}, failures...)
        aggregatedNames := append([]string{}, failedCommands...)
        failureMutex.Unlock()

        sort.Slice(report.Ran, func(first int, second int) bool {
            return report.Ran[first].Command < report.Ran[second].Command
        })

        if 0 < len(aggregatedNames) {
            return report, exception.NewError(
                "cron: one or more scheduled commands failed",
                exceptioncontract.Context{
                    "commands": strings.Join(aggregatedNames, ", "),
                },
                errors.Join(aggregated...),
            )
        }

        return report, nil
    }
}

func (instance *RunnerCommand) runDispatched(
    runtimeInstance runtimecontract.Runtime,
    entry *scheduledRunEntry,
    runId string,
    invokedAt time.Time,
) (row dueRun, failure error, counted bool) {
    defer func() {
        recovered := recover()
        if nil == recovered {
            return
        }

        failure = exception.NewError(
            "cron: the runner panicked preparing or reporting a scheduled run",
            exceptioncontract.Context{
                "commandName": entry.commandName,
                "cronRunId":   runId,
                "panicValue":  fmt.Sprintf("%v", recovered),
                "panicStack":  string(debug.Stack()),
            },
            exception.PanicCause(recovered),
        )

        row = dueRun{
            Command:              entry.commandName,
            Schedule:             entry.schedule,
            Arguments:            argumentsOrEmpty(entry.arguments),
            RunId:                runId,
            DurationMilliseconds: time.Since(invokedAt).Milliseconds(),
            Failed:               true,
            Cancelled:            false,
            Error:                failure.Error(),
        }
        counted = true

        instance.reportContained(runtimeInstance, entry, runId, failure)
    }()

    invokeErr := instance.invokeRun(runtimeInstance, entry, runId)

    cancelled := nil != invokeErr && true == isShutdownCancellation(runtimeInstance, invokeErr)

    row = dueRun{
        Command:              entry.commandName,
        Schedule:             entry.schedule,
        Arguments:            argumentsOrEmpty(entry.arguments),
        RunId:                runId,
        DurationMilliseconds: time.Since(invokedAt).Milliseconds(),
        Failed:               nil != invokeErr && false == cancelled,
        Cancelled:            cancelled,
        Error:                errorTextOrEmpty(invokeErr),
    }

    counted = instance.reportRunOutcome(runtimeInstance, entry, runId, invokeErr, cancelled)

    return row, invokeErr, counted
}

func (instance *RunnerCommand) reportContained(
    runtimeInstance runtimecontract.Runtime,
    entry *scheduledRunEntry,
    runId string,
    failure error,
) {
    defer func() {
        _ = recover()
    }()

    instance.reportRunOutcome(runtimeInstance, entry, runId, failure, false)
}

func argumentsOrEmpty(arguments []string) []string {
    if nil == arguments {
        return []string{}
    }

    return arguments
}

func errorTextOrEmpty(err error) string {
    if nil == err || true == isNilInterface(err) {
        return ""
    }

    return err.Error()
}

const clockJumpResetThreshold = 3 * time.Hour

type minuteEvaluation struct {
    at           time.Time
    runFixedTime bool
    runWildcard  bool
}

func wallMinuteIndex(at time.Time) int64 {
    _, offsetSeconds := at.Zone()

    return (at.Unix() + int64(offsetSeconds)) / 60
}

func wallMinuteTime(index int64, location *time.Location) time.Time {
    utc := time.Unix(index*60, 0).UTC()

    local := time.Date(utc.Year(), utc.Month(), utc.Day(), utc.Hour(), utc.Minute(), 0, 0, location)
    if wallMinuteIndex(local) == index {
        return local
    }

    return utc
}

func reconcileWallClock(previousTarget time.Time, current time.Time) ([]minuteEvaluation, time.Time, string) {

    currentMinute := current.Truncate(time.Minute)

    previousIndex := wallMinuteIndex(previousTarget)
    currentIndex := wallMinuteIndex(currentMinute)

    difference := currentIndex - previousIndex
    resetThresholdMinutes := int64(clockJumpResetThreshold / time.Minute)

    if difference >= resetThresholdMinutes || difference <= -resetThresholdMinutes {
        note := fmt.Sprintf(
            "the wall clock jumped %d minutes; re-anchoring to the current minute without catch-up",
            difference,
        )

        evaluations := []minuteEvaluation{{at: currentMinute, runFixedTime: true, runWildcard: true}}

        return evaluations, currentMinute, note
    }

    if 1 == difference {
        evaluations := []minuteEvaluation{{at: currentMinute, runFixedTime: true, runWildcard: true}}

        return evaluations, currentMinute, ""
    }

    if difference > 1 {
        evaluations := make([]minuteEvaluation, 0, difference+1)
        evaluations = append(evaluations, minuteEvaluation{at: currentMinute, runWildcard: true})

        for index := previousIndex + 1; index < currentIndex; index++ {
            evaluations = append(evaluations, minuteEvaluation{at: wallMinuteTime(index, current.Location()), runFixedTime: true})
        }

        evaluations = append(evaluations, minuteEvaluation{at: currentMinute, runFixedTime: true})

        return evaluations, currentMinute, ""
    }

    evaluations := []minuteEvaluation{{at: currentMinute, runWildcard: true}}

    return evaluations, previousTarget, ""
}

func (instance *RunnerCommand) invoke(runtimeInstance runtimecontract.Runtime, entry *scheduledRunEntry) (runId string, invokeErr error) {

    runId = logging.GenerateProcessId()

    return runId, instance.invokeRun(runtimeInstance, entry, runId)
}

func (instance *RunnerCommand) invokeRun(runtimeInstance runtimecontract.Runtime, entry *scheduledRunEntry, runId string) (invokeErr error) {
    childContext, cancel := commandContextOf(runtimeInstance.Context(), entry.timeout)
    defer cancel()

    childScope := runtimeInstance.Container().NewScope()
    defer func() {
        closeErr := childScope.Close()
        if nil == closeErr {
            return
        }

        scopeCloseFailure := exception.NewError(
            "cron: child scope close failed after the scheduled command",
            exceptioncontract.Context{
                "commandName": entry.commandName,
            },
            closeErr,
        )

        if nil == invokeErr {
            invokeErr = scopeCloseFailure

            return
        }

        invokeErr = exception.NewError(
            "cron: the scheduled command failed and its child scope close failed",
            withSiblingFailure(
                exceptioncontract.Context{
                    "commandName": entry.commandName,
                },
                "scopeCloseError",
                scopeCloseFailure,
            ),
            invokeErr,
        )
    }()

    if runLogger := logging.LoggerFromRuntime(runtimeInstance); nil != runLogger {
        childScope.MustOverrideProtectedInstance(
            logging.ServiceLogger,
            logging.NewProcessLogger(runLogger, runId, "cronRunId"),
        )
    }
    childScope.MustOverrideProtectedInstance(
        application.ServiceProcessContext,
        application.NewProcessContext(runId, time.Now()),
    )

    childRuntime := runtime.New(childContext, childScope, runtimeInstance.Container())

    capturedOutput := newScheduledOutputCapture()
    defer func() {
        instance.reportScheduledOutput(runtimeInstance, entry, runId, capturedOutput, invokeErr)
    }()

    completed := make(chan error, 1)
    go func() {
        completed <- runScheduledCommand(childContext, entry, childRuntime, capturedOutput)
    }()

    var abandon <-chan time.Time
    if 0 < entry.timeout {
        abandonTimer := time.NewTimer(abandonDelayOf(entry.timeout, instance.gracefulTimeoutOf(entry)))
        defer abandonTimer.Stop()

        abandon = abandonTimer.C
    }

    shutdown := runtimeInstance.Context().Done()

    var shutdownAbandon <-chan time.Time

    for {
        select {
        case runErr := <-completed:

            if nil != runErr && true == errors.Is(childContext.Err(), context.DeadlineExceeded) {
                return instance.timeoutError(entry, false, runErr)
            }

            return runErr
        case <-abandon:
            return instance.resolveAbandonedRun(runtimeInstance, entry, runId, childContext, completed, abandonedAfterDeadline)
        case <-shutdown:

            shutdownTimer := time.NewTimer(instance.gracefulTimeoutOf(entry))
            defer shutdownTimer.Stop()

            shutdownAbandon = shutdownTimer.C
            shutdown = nil
        case <-shutdownAbandon:
            return instance.resolveAbandonedRun(runtimeInstance, entry, runId, childContext, completed, abandonedAfterShutdown)
        }
    }
}

type abandonCause int

const (
    abandonedAfterDeadline abandonCause = iota
    abandonedAfterShutdown
)

const scheduledOutputCaptureLimit = 64 * 1024

type scheduledOutputCapture struct {
    mutex        sync.Mutex
    buffer       strings.Builder
    droppedBytes int
}

func newScheduledOutputCapture() *scheduledOutputCapture {
    return &scheduledOutputCapture{}
}

func (instance *scheduledOutputCapture) Write(content []byte) (int, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    remaining := scheduledOutputCaptureLimit - instance.buffer.Len()
    if 0 >= remaining {
        instance.droppedBytes = instance.droppedBytes + len(content)

        return len(content), nil
    }

    if len(content) > remaining {

        cut := remaining
        for 0 < cut && false == utf8.RuneStart(content[cut]) {
            cut--
        }

        instance.buffer.Write(content[:cut])
        instance.droppedBytes = instance.droppedBytes + len(content) - cut

        return len(content), nil
    }

    instance.buffer.Write(content)

    return len(content), nil
}

func (instance *scheduledOutputCapture) captured() (string, int) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return instance.buffer.String(), instance.droppedBytes
}

func (instance *RunnerCommand) reportScheduledOutput(
    runtimeInstance runtimecontract.Runtime,
    entry *scheduledRunEntry,
    runId string,
    capturedOutput *scheduledOutputCapture,
    invokeErr error,
) {
    commandOutput, droppedBytes := capturedOutput.captured()
    if "" == commandOutput {
        return
    }

    logger := logging.LoggerFromRuntime(runtimeInstance)
    if nil == logger {
        return
    }

    recordContext := exceptioncontract.Context{
        "commandName":   entry.commandName,
        "cronRunId":     runId,
        "commandOutput": commandOutput,
    }

    if 0 < droppedBytes {
        recordContext["commandOutputDroppedBytes"] = droppedBytes
    }

    if nil != invokeErr {
        logger.Warning("cron: scheduled command output", recordContext)

        return
    }

    logger.Info("cron: scheduled command output", recordContext)
}

func withSiblingFailure(base exceptioncontract.Context, prefix string, sibling error) exceptioncontract.Context {
    merged := exceptioncontract.Context{}
    for key, value := range base {
        merged[key] = value
    }

    if nil == sibling || true == isNilInterface(sibling) {
        return merged
    }

    merged[prefix] = sibling.Error()

    siblingContext := map[string]any{}

    var provider exceptioncontract.ContextProvider
    if true == errors.As(sibling, &provider) && false == isNilInterface(provider) {
        for key, value := range provider.Context() {
            siblingContext[key] = value
        }
    }

    merged[prefix+"Context"] = siblingContext

    causeChain := []string{}
    for current := errors.Unwrap(sibling); nil != current && false == isNilInterface(current); current = errors.Unwrap(current) {
        causeChain = append(causeChain, current.Error())

        if 8 <= len(causeChain) {
            break
        }
    }

    merged[prefix+"CauseChain"] = causeChain

    return merged
}

func (instance *RunnerCommand) reportRunOutcome(
    runtimeInstance runtimecontract.Runtime,
    entry *scheduledRunEntry,
    runId string,
    invokeErr error,
    cancelled bool,
) bool {
    if nil == invokeErr {
        return false
    }

    recordContext := exception.LogContext(
        invokeErr,
        exceptioncontract.Context{
            "commandName": entry.commandName,
            "cronRunId":   runId,
        },
    )

    logger := logging.LoggerFromRuntime(runtimeInstance)

    if true == cancelled {
        if nil != logger {
            logger.Warning("cron: scheduled command cancelled by shutdown", recordContext)
        }

        return false
    }

    if nil != logger {
        logger.Error("cron runner command failed", recordContext)
    }

    return true
}

func isShutdownCancellation(runtimeInstance runtimecontract.Runtime, invokeErr error) bool {
    if nil == runtimeInstance.Context().Err() {
        return false
    }

    return true == errors.Is(invokeErr, context.Canceled) && false == errors.Is(invokeErr, context.DeadlineExceeded)
}

func (instance *RunnerCommand) resolveAbandonedRun(
    runtimeInstance runtimecontract.Runtime,
    entry *scheduledRunEntry,
    runId string,
    childContext context.Context,
    completed <-chan error,
    cause abandonCause,
) error {
    select {
    case runErr := <-completed:
        if nil != runErr && true == errors.Is(childContext.Err(), context.DeadlineExceeded) {
            return instance.timeoutError(entry, false, runErr)
        }

        return runErr
    default:
    }

    if logger := logging.LoggerFromRuntime(runtimeInstance); nil != logger {
        if abandonedAfterShutdown == cause {
            logger.Warning(
                "cron: scheduled command ignored the shutdown cancellation for the whole graceful window and is being abandoned; its container scope is closed under it while it may still be running",
                exceptioncontract.Context{
                    "commandName":     entry.commandName,
                    "cronRunId":       runId,
                    "gracefulTimeout": instance.gracefulTimeoutOf(entry).String(),
                },
            )
        } else {
            logger.Warning(
                "cron: scheduled command ignored its cancelled context for the whole graceful window and is being abandoned; its container scope is closed under it while it may still be running",
                exceptioncontract.Context{
                    "commandName":     entry.commandName,
                    "cronRunId":       runId,
                    "timeout":         entry.timeout.String(),
                    "gracefulTimeout": instance.gracefulTimeoutOf(entry).String(),
                },
            )
        }
    }

    if abandonedAfterShutdown == cause {
        return instance.shutdownAbandonError(entry)
    }

    return instance.timeoutError(entry, true, nil)
}

func (instance *RunnerCommand) shutdownAbandonError(entry *scheduledRunEntry) error {
    return exception.NewError(
        "cron: scheduled command ignored the shutdown cancellation and did not return; it was abandoned and its container scope closed while it may still be running",
        exceptioncontract.Context{
            "commandName": entry.commandName,
            "unwindGrace": instance.gracefulTimeoutOf(entry).String(),
        },
        ErrCommandTimeout,
    )
}

func abandonDelayOf(timeout time.Duration, gracefulTimeout time.Duration) time.Duration {
    if gracefulTimeout > math.MaxInt64-timeout {
        return math.MaxInt64
    }

    return timeout + gracefulTimeout
}

func commandContextOf(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
    if 0 >= timeout {
        return context.WithCancel(parent)
    }

    return context.WithTimeout(parent, timeout)
}

func runScheduledCommand(
    ctx context.Context,
    entry *scheduledRunEntry,
    childRuntime runtimecontract.Runtime,
    capturedOutput io.Writer,
) (runErr error) {
    defer func() {
        if recovered := recover(); nil != recovered {

            panicFailure := exception.NewError(
                "cron: scheduled command panicked",
                exceptioncontract.Context{
                    "commandName": entry.commandName,
                    "panicValue":  fmt.Sprintf("%v", recovered),
                    "panicStack":  string(debug.Stack()),
                },
                exception.PanicCause(recovered),
            )

            if nil == runErr {

                runErr = panicFailure

                return
            }

            runErr = exception.NewError(
                "cron: the scheduled command failed and then panicked",
                withSiblingFailure(
                    exceptioncontract.Context{
                        "commandName": entry.commandName,
                    },
                    "commandError",
                    runErr,
                ),
                panicFailure,
            )
        }
    }()

    return melodycli.DispatchCommand(
        ctx,
        entry.command,
        childRuntime,
        append([]string{entry.commandName}, entry.arguments...),
        capturedOutput,
    )
}

func isNilInterface(value any) bool {
    if nil == value {
        return true
    }

    reflected := reflect.ValueOf(value)

    switch reflected.Kind() {
    case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func, reflect.Interface:
        return reflected.IsNil()
    default:
        return false
    }
}

func (instance *RunnerCommand) timeoutError(entry *scheduledRunEntry, abandoned bool, runErr error) error {
    if true == abandoned {
        return exception.NewError(
            "cron: scheduled command exceeded its timeout and did not return; it was abandoned and its container scope closed while it may still be running",
            exceptioncontract.Context{
                "commandName": entry.commandName,
                "timeout":     entry.timeout.String(),
                "unwindGrace": instance.gracefulTimeoutOf(entry).String(),
            },
            commandTimeoutCause(runErr),
        )
    }

    return exception.NewError(
        "cron: scheduled command was cancelled by its timeout",
        exceptioncontract.Context{
            "commandName": entry.commandName,
            "timeout":     entry.timeout.String(),
        },
        commandTimeoutCause(runErr),
    )
}

var _ clicontract.Command = (*RunnerCommand)(nil)
