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

/* scheduledRunEntry pairs a parsed schedule with the registered command it fires, resolved once at construction so the tick loop never looks a command up by name. fixedTime is the vixie-cron entry class the wall-clock reconciliation reads: a fixed-time entry pins both a minute and an hour. */
type scheduledRunEntry struct {
    commandName string
    command     clicontract.Command
    arguments   []string
    /* the rendered expression is kept beside the matcher because the runner's documents name the schedule, and the matcher has no way back to its text */
    schedule        string
    matcher         *scheduleMatcher
    fixedTime       bool
    timeout         time.Duration
    gracefulTimeout time.Duration
}

/* defaultCommandTimeout bounds one run when its entry sets no timeout of its own. It is zero, no deadline, so nothing bounds a run while the runner is live: a command wedged on a deadline-less read holds its goroutine and its container scope until shutdown, one of each per matching minute. Only the shutdown ends such a run, cancelling its context and abandoning it one EntryConfig.GracefulTimeout later; a deadline cancels the context and is not a kill. */
const defaultCommandTimeout = 0

/* commandUnwindGrace is how long a command whose context was cancelled, by its entry's deadline or by the shutdown, is given to unwind before the runner stops waiting, when its entry names no window. It is long because what follows is not graceful: the run's container scope is closed under a command that may still be executing. */
const commandUnwindGrace = 300 * time.Second

/* RunnerCommand runs the cron Configuration in-process instead of emitting a manifest: it evaluates each entry's schedule against the wall clock under the configured RunnerDialect and invokes the registered command when it is due. Due commands run concurrently, each on its own goroutine, so an entry that outlives its interval overlaps itself; wrap the command in lock.NewExclusiveCommand to serialise runs, or gate the runner behind a lock.LeaderGate for multi-instance safety. A deadline set by EntryConfig.Timeout, or the shutdown, cancels the command's context, and a command still running one EntryConfig.GracefulTimeout later is reported at warning, has its scope closed under it and stops counting towards the shutdown wait; wall-clock jumps follow the vixie-cron algorithm documented on reconcileWallClock. */
type RunnerCommand struct {
    entries []*scheduledRunEntry
    now     func() time.Time
    /* the zone every evaluated minute is rendered in, applied to the clock reading so nothing below needs to know it; nil means the process zone */
    location    *time.Location
    unwindGrace time.Duration
    inFlight    sync.WaitGroup
    /* the scheduler loop hands each minute's wait to a goroutine of its own, so two minutes' documents can complete in any order: the writer is guarded to keep one document one line */
    writeMutex          sync.Mutex
    userIgnoredCommands []string
}

type runReporting struct {
    commandContext clicontract.Context
    option         output.Option
    reportIdle     bool
}

/* NewRunnerCommand resolves every scheduled command name against the supplied commands and parses each schedule under the given dialect, the zero value being crontab. It panics at construction on an unknown command name, a malformed schedule, an unknown RunnerDialect (ErrUnknownRunnerDialect), two commands sharing a name (ErrDuplicateRunnerCommand), and an entry carrying EntryConfig.Command, several Instances or a DestinationFile, which belong to the generated manifests and would otherwise run twice. An entry naming a system user stays runnable as the process user, and Run logs one warning naming those commands. */
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

/* mustLoadConfiguredLocation resolves the zone a configuration declared, nil when it declared none. A name the standard library cannot load panics here, since falling back to the process zone would run every job at the right clock time in the wrong place. */
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

/* loadLocation reads an IANA zone name, naming the refusal rather than handing on the standard library's own. */
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

/* nowInZone is the one clock reading this runner makes, so the zone, the configured one or the one --timezone named for this invocation, is applied here and nowhere else. */
func (instance *RunnerCommand) nowInZone(location *time.Location) time.Time {
    now := instance.now()

    if nil == location {
        return now
    }

    return now.In(location)
}

func scheduleOfEntry(scheduled *ScheduledCommand) *Schedule {
    if nil == scheduled.Config {
        return nil
    }

    return scheduled.Config.Schedule
}

/* argumentsOfEntry reads the arguments this entry runs under, none when it declares none; Schedule already copied them from the registrant. */
func argumentsOfEntry(scheduled *ScheduledCommand) []string {
    if nil == scheduled.Config {
        return nil
    }

    return scheduled.Config.Arguments
}

/* timeoutOfEntry resolves the deadline one entry runs under: its own, the runner default when it is zero, and none when it is negative. */
func timeoutOfEntry(scheduled *ScheduledCommand) time.Duration {
    if nil == scheduled.Config || 0 == scheduled.Config.Timeout {
        return defaultCommandTimeout
    }

    return scheduled.Config.Timeout
}

/* gracefulTimeoutOfEntry reads the unwind window the entry names for itself, zero when it names none; the runner default is applied where the window is used, so a replaced default governs every entry that did not ask for its own. A negative value reads as unset, so an entry cannot ask for its scope to be torn down the instant a cancellation lands. */
func gracefulTimeoutOfEntry(scheduled *ScheduledCommand) time.Duration {
    if nil == scheduled.Config || 0 >= scheduled.Config.GracefulTimeout {
        return 0
    }

    return scheduled.Config.GracefulTimeout
}

/* gracefulTimeoutOf resolves the window a run actually gets: the entry's own, or this runner's default when it named none. */
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

/* the standard flags join the runner's own because the framework rewrites -v/-vv into --verbosity for every command, and they are honoured: every dispatched minute renders the envelope every other melody command renders, so --format=json yields a document */
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

    /* the flag wins over the configuration, so one invocation can run against another region's calendar; a mistyped zone is the command's error rather than a panic */
    location := instance.location
    if timezoneName := commandContext.String(flagNameTimezone); "" != timezoneName {
        flagLocation, loadErr := loadLocation(timezoneName)
        if nil != loadErr {
            return loadErr
        }

        location = flagLocation
    }

    if true == commandContext.Bool(flagNameOnce) {
        /* the minute is dispatched and reported as the wall minute, the form the scheduler loop reports; the clock reading keeps its seconds for the envelope's meta */
        startedAt := instance.nowInZone(location)
        report, runErr := instance.dispatchDue(runtimeInstance, startedAt.Truncate(time.Minute), true, true)()

        return instance.renderRunReport(commandContext, option, startedAt, report, runErr)
    }

    return instance.runLoop(
        runtimeInstance,
        location,
        &runReporting{
            commandContext: commandContext,
            option:         option,
            reportIdle:     commandContext.Bool(flagNameReportIdle),
        },
    )
}

/* declareSchedule writes what this process drives before it drives anything, so a scheduler whose configuration is empty or filtered away is visible; the empty case is a warning. */
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

/* renderRunReport writes one evaluated minute as the envelope every melody command writes: meta, data, warnings, error. Under json each minute is one closed document on its own line, a stream a consumer follows at constant memory; under text it is one line. The writer is guarded because two minutes' documents may complete in any order and must not interleave. */
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

/* failedCommandsOf answers the failed commands of a minute as a list on every row, empty rather than null when nothing failed */
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

/* runLoop wakes at each minute boundary until the runtime context is cancelled. The evaluation is pinned to the minute the monotonic timer was armed for and reconciled against the last evaluated minute by reconcileWallClock; the chain anchor and the first armed minute come from one clock read, since two reads could straddle a boundary and manufacture a jump. A wake that re-arrives at the wall minute already dispatched, which a backward step inside the armed window causes, is skipped. Due commands are dispatched without waiting; on cancellation the loop waits for the in-flight jobs, abandoning one that ignores the cancellation after one graceful window, and a nil reporting writes nothing. */
func (instance *RunnerCommand) runLoop(runtimeInstance runtimecontract.Runtime, location *time.Location, reporting *runReporting) error {
    now := instance.nowInZone(location)
    previousTarget := now.Truncate(time.Minute)
    dispatchedIndex := wallMinuteIndex(previousTarget)

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

        now = instance.nowInZone(location)

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

            /* the wait runs on its own goroutine so arming the next minute never blocks on a running job, and it counts towards the in-flight wait so a shutdown does not cut the last minute's document off mid-line */
            instance.inFlight.Add(1)

            go func(waitForMinute func() (dueReport, error), minuteStartedAt time.Time) {
                defer instance.inFlight.Done()

                report, runErr := waitForMinute()

                /* a minute that dispatched nothing is silent by default; --report-idle asks for it, which is how a consumer tells a live scheduler with nothing due from a dead one */
                if 0 == len(report.Ran) && false == reporting.reportIdle {
                    return
                }

                _ = instance.renderRunReport(reporting.commandContext, reporting.option, minuteStartedAt, report, runErr)
            }(wait, instance.nowInZone(location))
        }
    }
}

/* runDue invokes every entry whose schedule matches the given minute and waits for all of them, aggregating command failures so one bad job neither aborts the others nor is silently dropped. It answers the failure alone; the caller that also wants the document dispatches directly. */
func (instance *RunnerCommand) runDue(runtimeInstance runtimecontract.Runtime, at time.Time) error {
    _, runErr := instance.dispatchDue(runtimeInstance, at, true, true)()

    return runErr
}

/* dueRun is one dispatched command inside a minute's document: what ran, under which schedule and argv, its run id, how long it took and whether it failed. The error text and the argument list are empty rather than absent, so both keep their json type across rows. */
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

/* dueRunSortsBefore orders two runs of one minute by the command, then by the schedule and the arguments, since two entries may schedule one command and completion order is scheduling luck. */
func dueRunSortsBefore(first dueRun, second dueRun) bool {
    if first.Command != second.Command {
        return first.Command < second.Command
    }

    if first.Schedule != second.Schedule {
        return first.Schedule < second.Schedule
    }

    return strings.Join(first.Arguments, "\x00") < strings.Join(second.Arguments, "\x00")
}

/* dueReport is one evaluated minute: the minute, how many entries the runner drives and the runs it dispatched. The configured count is what tells a quiet minute from a scheduler that will never do anything. */
type dueReport struct {
    At         time.Time `json:"at"`
    Configured int       `json:"configured"`
    Ran        []dueRun  `json:"ran"`
}

/* dispatchDue starts every entry of the requested classes whose schedule matches the minute, each on its own goroutine, and logs each failure as its command completes. The returned wait blocks until every launched command finished and answers the minute's document with the aggregated failure; a command that ignores its cancelled context is abandoned one graceful window after the cancellation, and a panic in the runner's own code around a run is that run's failure. */
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

            /* the row and its verdict are built outside the lock, so a collaborator's code that panics, an Error() or the record, never leaves the minute's lock held under the wait */
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

        /* the runs are ordered by command name rather than completion order, which is scheduling luck, so two identical minutes render identical documents */
        sort.Slice(report.Ran, func(first int, second int) bool {
            return dueRunSortsBefore(report.Ran[first], report.Ran[second])
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

/* runDispatched runs one dispatched entry through to its row and verdict, and contains a panic of the runner's own preparation and reporting around the run: the child scope, the run identity, the scope close and the records call the application's container, logger and error types. Such a panic is filed as that run's failure with its value and stack, and its record is attempted under a containment of its own, since the logger may be what panicked; the command's body is contained separately in runScheduledCommand. */
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

        row = newDueRun(entry, runId, invokedAt, failure, false)
        counted = true

        instance.reportContained(runtimeInstance, entry, runId, failure)
    }()

    invokeErr := instance.invokeRun(runtimeInstance, entry, runId)

    /* a shutdown cancellation is a clean stop, so the document reports the run as cancelled, the same classification the aggregate makes */
    cancelled := nil != invokeErr && true == isShutdownCancellation(runtimeInstance, invokeErr)

    row = newDueRun(entry, runId, invokedAt, invokeErr, cancelled)

    counted = instance.reportRunOutcome(runtimeInstance, entry, runId, invokeErr, cancelled)

    return row, invokeErr, counted
}

/* newDueRun is the document's row for one run, the one shape both the run that returned and the run the runner panicked on are reported in: a shutdown cancellation is a clean stop, reported as cancelled and never as failed */
func newDueRun(entry *scheduledRunEntry, runId string, invokedAt time.Time, runErr error, cancelled bool) dueRun {
    return dueRun{
        Command:              entry.commandName,
        Schedule:             entry.schedule,
        Arguments:            argumentsOrEmpty(entry.arguments),
        RunId:                runId,
        DurationMilliseconds: time.Since(invokedAt).Milliseconds(),
        Failed:               nil != runErr && false == cancelled,
        Cancelled:            cancelled,
        Error:                errorTextOrEmpty(runErr),
    }
}

/* reportContained files the record of a run the runner itself panicked on and swallows a second panic: the logger is one of the collaborators that may have raised the first, and the row and the minute's aggregate carry the failure whether or not the record could be written. */
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

/* argumentsOrEmpty keeps the document's arguments field a list on every row, empty rather than null, without touching the configuration it was read from. */
func argumentsOrEmpty(arguments []string) []string {
    if nil == arguments {
        return []string{}
    }

    return arguments
}

/* errorTextOrEmpty keeps the document's error field a string on every row, so it keeps its json type. */
func errorTextOrEmpty(err error) string {
    if true == isNilInterface(err) {
        return ""
    }

    return err.Error()
}

/* clockJumpResetThreshold is the vixie-cron three-hour bound on wall-clock reconciliation: a jump smaller than this (a daylight-saving transition, a suspend, an ntp step) is resolved minute by minute, while a jump of at least this much in either direction is treated as a clock reset and the minute chain re-anchors to the current minute without catch-up. */
const clockJumpResetThreshold = 3 * time.Hour

type minuteEvaluation struct {
    at           time.Time
    runFixedTime bool
    runWildcard  bool
}

/* wallMinuteIndex renders a time as whole minutes since the unix epoch on its wall clock, folding the zone offset in, so consecutive indices are consecutive wall minutes and a zone-offset change (daylight saving) steps the index by the offset change even though absolute time advanced one minute. */
func wallMinuteIndex(at time.Time) int64 {
    _, offsetSeconds := at.Zone()

    return (at.Unix() + int64(offsetSeconds)) / 60
}

/* wallMinuteTime materialises a wall minute index as the instant of the location whose rendered calendar fields equal it. A minute the location skips at a spring-forward has no such instant, so it stays in utc with the fields it needs and still evaluates, which lets a fixed-time entry pinned inside the gap run exactly once; a minute a fall-back repeats has two instants, and the one time.Date picks is answered. */
func wallMinuteTime(index int64, location *time.Location) time.Time {
    utc := time.Unix(index*60, 0).UTC()

    local := time.Date(utc.Year(), utc.Month(), utc.Day(), utc.Hour(), utc.Minute(), 0, 0, location)
    if wallMinuteIndex(local) == index {
        return local
    }

    return utc
}

/* reconcileWallClock is the vixie-cron virtual-time algorithm: it compares the wall rendering of the minute the loop woke for with the last wall minute evaluated and decides which minutes to evaluate for which entry class, where the chain continues and whether the wake needs a log line. A one-minute advance evaluates the current minute for both classes; a larger forward jump below the reset threshold evaluates the current minute for wildcard entries and every skipped minute plus the current one for fixed-time entries, so a schedule pinned inside a gap runs exactly once. A zero or backward jump below the threshold evaluates the current minute for wildcard entries only and keeps the anchor, so fixed-time entries cannot run twice; a jump of at least the threshold re-anchors without catch-up and returns a note. The function is pure, so callers own every side effect. */
func reconcileWallClock(previousTarget time.Time, current time.Time) ([]minuteEvaluation, time.Time, string) {
    /* every evaluated minute is reported as its real local instant, since it becomes the document's timestamp: the current minute is taken from the clock as it came, and the catch-up minutes before it are materialised by wallMinuteTime, which keeps the utc form only for minutes a spring-forward skipped */
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

    /* the wall clock stepped back less than the threshold (or repeats the anchor minute itself), so the just-woken minute repeats a span that already ran: wildcard entries follow the wall clock, fixed-time entries stay suppressed behind the unchanged anchor. */
    evaluations := []minuteEvaluation{{at: currentMinute, runWildcard: true}}

    return evaluations, previousTarget, ""
}

/* invokeRun runs one command on a child runtime under the run id the dispatch minted, a fresh scope so scoped services do not bleed across ticks and a context derived from the runner's carrying the entry's deadline. The command line goes through cli.DispatchCommand with the command's declared flags, the surface a command sees under the cli entry point minus the banner, the scope close and the exit handling; an error carrying an exit code comes back as an error, since melody's inert exit handler replaces the engine's os.Exit. A panic inside the command is recovered as an error, and a child scope close failure travels beside the command's error in its context. The command runs on its own goroutine so a cancellation can be enforced against a command that never looks at its context, in three steps whether the deadline or the shutdown cancels it: the context is cancelled; a command that watches it unwinds inside the graceful window and reports its own error with the timeout; a command that ignores it is abandoned when the window lapses, reported at warning, has its scope closed under it and stops counting towards the shutdown wait. That last step is a kill: scope.MustGet on the closed scope panics, and the recover here covers only the goroutine this runner started, so a command resolving from the scope on a goroutine of its own can take the process down. That is why the window is minutes long and a slow unwind names its own EntryConfig.GracefulTimeout. */
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

        /* the command's failure stays the wrapped one so its context and cause chain reach the record, and the close failure travels beside it in the context rather than through a join */
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

    /* each run gets the console identity the cli entry point installs, because the child scope is a sibling and inherits nothing: a fresh ProcessContext under the per-run id, so overlapping runs stay distinguishable, and a logger derived from the run's, so the job's records carry the processId and the cronRunId */
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

    /* the job's output is captured into the journal rather than the shared process stdout, where concurrent jobs would interleave: one job's output is one record carrying the entry and the run id */
    capturedOutput := newScheduledOutputCapture()
    defer func() {
        instance.reportScheduledOutput(runtimeInstance, entry, runId, capturedOutput, invokeErr)
    }()

    completed := make(chan error, 1)
    go func() {
        completed <- runScheduledCommand(childContext, entry, childRuntime, capturedOutput)
    }()

    /* the abandon signal sits one unwind grace past whichever cancellation reaches the command, so a command that honours its cancelled context always reports its own outcome. The deadline arms it at the start of the run and the shutdown arms it on arrival; an entry that opted out of the deadline is never abandoned while the runner is live, a nil channel blocking forever, and is abandoned at shutdown like every other. */
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
            return instance.completedRunError(entry, childContext, runErr)
        case <-abandon:
            return instance.resolveAbandonedRun(runtimeInstance, entry, runId, childContext, completed, abandonedAfterDeadline)
        case <-shutdown:
            /* the shutdown has reached the command through its context; from here it gets the same window the deadline gives, and the case is disarmed so the timer is armed once */
            shutdownTimer := time.NewTimer(instance.gracefulTimeoutOf(entry))
            defer shutdownTimer.Stop()

            shutdownAbandon = shutdownTimer.C
            shutdown = nil
        case <-shutdownAbandon:
            return instance.resolveAbandonedRun(runtimeInstance, entry, runId, childContext, completed, abandonedAfterShutdown)
        }
    }
}

/* completedRunError reads the answer of a run that returned: a command that returned nil finished its work, whatever the clock did in the same instant; only a failure is attributed to the deadline */
func (instance *RunnerCommand) completedRunError(entry *scheduledRunEntry, childContext context.Context, runErr error) error {
    if nil != runErr && true == errors.Is(childContext.Err(), context.DeadlineExceeded) {
        return instance.timeoutError(entry, false, runErr)
    }

    return runErr
}

/* abandonCause names which cancellation a run ignored for the whole of its graceful window: the deadline its entry asked for, or the runner's shutdown. */
type abandonCause int

const (
    abandonedAfterDeadline abandonCause = iota
    abandonedAfterShutdown
)

/* scheduledOutputCaptureLimit bounds what one run may contribute to the journal, so a job printing a row per record cannot write its whole result set into one log line; what was cut is counted and named in the record. */
const scheduledOutputCaptureLimit = 64 * 1024

/* scheduledOutputCapture is the writer a scheduled command is handed in place of the process stdout. It always reports the full length as written, since a short write is an io.Writer contract violation the command would return as its failure. */
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
        /* the cut backs off to the start of the rune it would split, so the kept text is valid in the record's json field */
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

/* reportScheduledOutput files what the job wrote, once, under the identity of its run; a run that printed nothing files nothing. The level follows the run's outcome, and the failure itself is filed by the dispatch. */
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

/* withSiblingFailure carries a second failure inside the context of the error the runner returns, instead of joining the two: exception.LogContext follows only the branch of a join that a deep errors.As reaches, so a joined pair would file one failure and drop the other. The primary error stays the wrapped one, so every errors.Is still answers; the sibling travels under the prefix with its context and cause chain, both fields an empty object and an empty list rather than null. */
func withSiblingFailure(base exceptioncontract.Context, prefix string, sibling error) exceptioncontract.Context {
    merged := exceptioncontract.Context{}
    for key, value := range base {
        merged[key] = value
    }

    if true == isNilInterface(sibling) {
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
    for current := errors.Unwrap(sibling); false == isNilInterface(current); current = errors.Unwrap(current) {
        causeChain = append(causeChain, current.Error())

        if 8 <= len(causeChain) {
            break
        }
    }

    merged[prefix+"CauseChain"] = causeChain

    return merged
}

/* reportRunOutcome files the record one dispatched run leaves and answers whether it counts towards the minute's failure aggregate: a command that unwound when the shutdown cancelled it is a clean stop, recorded at warning and kept out, while a deadline the entry asked for stays a failure. It is handed the classification the document row was built from, since reading the runtime's context twice could give one run two verdicts; being a function lets a test hand it both answers separately. */
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

    /* the record carries the run's cronRunId, so a failure record ties to the run whose own records it explains */
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

/* isShutdownCancellation classifies a run's failure as the runner's own shutdown reaching it: the runner's context is cancelled and the outcome is the cancellation, not an entry deadline. Only the parent's cancellation qualifies — a deadline the entry asked for stays a failure whatever the shutdown is doing. */
func isShutdownCancellation(runtimeInstance runtimecontract.Runtime, invokeErr error) bool {
    if nil == runtimeInstance.Context().Err() {
        return false
    }

    return true == errors.Is(invokeErr, context.Canceled) && false == errors.Is(invokeErr, context.DeadlineExceeded)
}

/* resolveAbandonedRun answers a run whose abandon window lapsed. It reads the completion channel once more before announcing the kill, because select picks at random between two ready cases and a command that returned as the timer fired has already done its work; being a function lets a test hand it a channel that already carries the answer. */
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
        return instance.completedRunError(entry, childContext, runErr)
    default:
    }

    /* the kill is logged naming the entry and the window it overran, since it is the one path that tears a scope down under running code and the remedy, a longer GracefulTimeout or a command that watches its context, should be readable from the line */
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

/* shutdownAbandonError reports a run the shutdown ended after the command ignored the cancellation for its whole graceful window. It carries ErrCommandTimeout, the classification a lapsed deadline gets, and not the cancellation, since context.Canceled is what a command that stopped when asked reports and the document must say failed here. */
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

/* abandonDelayOf places the abandon signal one unwind grace past the deadline, saturating at the maximum duration: both are caller-supplied, and a sum that wrapped negative would fire at once and kill every run of the entry as it started. */
func abandonDelayOf(timeout time.Duration, gracefulTimeout time.Duration) time.Duration {
    if gracefulTimeout > math.MaxInt64-timeout {
        return math.MaxInt64
    }

    return timeout + gracefulTimeout
}

/* commandContextOf derives the command's context from the runner's, adding the entry's deadline when it has one; a non-positive timeout yields a merely cancellable context. */
func commandContextOf(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
    if 0 >= timeout {
        return context.WithCancel(parent)
    }

    return context.WithTimeout(parent, timeout)
}

/* runScheduledCommand dispatches one command and turns a panic inside it into an error. The recover has to be on the goroutine that runs the command, since a panic is recoverable only there; runDispatched's recover covers the runner's own code on the dispatch goroutine. */
func runScheduledCommand(
    ctx context.Context,
    entry *scheduledRunEntry,
    childRuntime runtimecontract.Runtime,
    capturedOutput io.Writer,
) (runErr error) {
    defer func() {
        if recovered := recover(); nil != recovered {
            /* the panic value travels as the cause and the stack is captured here, where the job's frames are still on the goroutine, so an error-shaped panic keeps its context and chain in the record */
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
                /* the command never returned, so the panic error is the run's outcome; the two-failure case below does not join either, since a join files one branch and drops the other */
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

    /* the entry's own arguments travel with the command name, so the job runs under the posture it declared and the same command line the generated manifest runs */
    return melodycli.DispatchCommand(
        ctx,
        entry.command,
        childRuntime,
        append([]string{entry.commandName}, entry.arguments...),
        capturedOutput,
    )
}

/* isNilInterface duplicates the framework's internal helper, which an integration module may not import. */
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

/* timeoutError classifies a run the deadline ended, carrying the command's own failure where there is one on one single-valued chain through commandTimeoutCause, so both stay reachable by errors.Is and exception.LogContext walks through both. */
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
