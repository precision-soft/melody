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

/* scheduledRunEntry pairs a parsed schedule with the registered command it fires, resolved once at construction so the tick loop never looks a command up by name at run time. fixedTime carries the vixie-cron entry class the wall-clock reconciliation reads: a fixed-time entry pins both a minute and an hour, a wildcard entry leaves either as a plain or stepped wildcard. */
type scheduledRunEntry struct {
    commandName string
    command     clicontract.Command
    arguments   []string
    /* the rendered expression is kept beside the parsed matcher because the runner's own document and its declaration record name the schedule an entry runs under, and the matcher is a set of admitted values with no way back to the text that produced it */
    schedule        string
    matcher         *scheduleMatcher
    fixedTime       bool
    timeout         time.Duration
    gracefulTimeout time.Duration
}

/* defaultCommandTimeout bounds one run of a scheduled command when its entry sets no timeout of its own. It is zero — no deadline — which is what every entry configured before the deadline existed already ran under, so upgrading does not begin cutting a job short at a duration nobody chose.

What zero costs is worth naming, because nothing else bounds a run. The runner starts a goroutine per due entry per matching minute, and a command's context is derived from the runtime's, which is cancelled at shutdown and never before — so a command wedged on a deadline-less network read holds its goroutine AND its container scope until the process ends, one of each per matching minute, 1440 a day for a per-minute entry, and nothing in the logs marks the run that never finished. An entry that wants the bound asks for it with EntryConfig.Timeout; an hour is a reasonable value for work that normally finishes in seconds.

A deadline is not a kill. It cancels the command's context, which a command that watches it answers by unwinding; only after EntryConfig.GracefulTimeout on top of it does the runner stop waiting and tear the run's scope down under a command that never looked. */
const defaultCommandTimeout = 0

/* commandUnwindGrace is how long a command that has hit its deadline is given to unwind before the runner stops waiting for it, when its entry names no window of its own. The deadline cancels the command's context; a command that watches it returns well inside this window and has its own error reported together with the timeout.

Five minutes rather than seconds, because what happens at the end of it is not graceful: the run's container scope is closed under a command that may still be executing, which is the only way to give the resources back and is why the window before it has to be long enough for any honest unwind — flushing a batch, rolling a transaction back, finishing an in-flight request. An entry whose unwind is legitimately slower names its own with EntryConfig.GracefulTimeout. */
const commandUnwindGrace = 300 * time.Second

/* RunnerCommand runs the same cron Configuration in-process instead of emitting a manifest for an external scheduler: it evaluates each entry's schedule against the wall clock and invokes the corresponding registered command when it is due. A single-binary deployment (no crontab, no kubernetes) gets its scheduled work from the one Configuration that already drives the generator. The day-of-month / day-of-week combination follows the configured RunnerDialect — crontab by default, the vixie crond rule where a star-based day field (plain or stepped wildcard) counts as unrestricted and the day fields combine with and; the kubernetes dialect opts into the robfig scheduler behind the k8s template, where only the star-bit shapes (the plain or the unit-stepped wildcard, alone or inside a list) are unrestricted and a stepped wildcard day field with a step above one combines with or. Two genuinely restricted day fields combine with or in both dialects; the two real schedulers diverge only on the star-based shapes, which is inherent to the targets, so pick the dialect of the manifests the same Configuration generates.

Due commands run concurrently, each in its own goroutine, the way crontab starts an independent process per entry: one slow job delays neither the commands sharing its minute nor the scheduler loop, and an entry that runs longer than its own interval overlaps itself — wrap the command in lock.NewExclusiveCommand to serialize successive runs. A run is bounded only where EntryConfig.Timeout asks for it, because nothing in the runtime context would ever end a command wedged on a deadline-less read and a bound melody picked would cut short a job that had always been allowed to take as long as it takes. Where a deadline is set, reaching it cancels the command's context; a command still running one EntryConfig.GracefulTimeout later is reported at warning, has its scope closed under it and stops counting towards the shutdown wait, since waiting on it would never end either. Wall-clock jumps follow the vixie-cron virtual-time algorithm, documented on reconcileWallClock, so a schedule pinned inside a daylight-saving gap still runs exactly once. Multi-instance safety is left to composition — wrap each command in lock.NewExclusiveCommand, or gate the whole runner behind a lock.LeaderGate, before handing the commands in. */
type RunnerCommand struct {
    entries []*scheduledRunEntry
    now     func() time.Time
    /* the zone every minute this runner evaluates is rendered in. The schedule matcher reads calendar fields and the wall-minute chain folds the offset in, so the zone is applied to the CLOCK READING and nothing below it needs to know about it. Nil means the process zone, which is what the runner always did. */
    location    *time.Location
    unwindGrace time.Duration
    inFlight    sync.WaitGroup
    /* the scheduler loop hands each minute's wait to a goroutine of its own, so two minutes' documents can complete in any order: the writer is guarded to keep one document one line */
    writeMutex sync.Mutex
    /* the output posture the scheduler loop reports under, installed by Run from the parsed flags before the loop starts and read by it alone. It is nil for a loop driven directly, which is how the loop's own tests reach it without a cli command context, and a nil reporting loop writes nothing — the behaviour every caller had before the document existed. */
    reporting           *runReporting
    userIgnoredCommands []string
}

/* runReporting is the output posture one invocation of the scheduler loop reports under. */
type runReporting struct {
    commandContext clicontract.Context
    option         output.Option
    reportIdle     bool
}

/* NewRunnerCommand resolves every scheduled command name against the supplied commands and parses each schedule up front under the given day-combination dialect (the zero value is the crontab default); an entry naming a command that was not supplied, or carrying a malformed schedule, is a wiring error and panics at construction so it surfaces at boot rather than at the first tick, and a value naming no known RunnerDialect panics the same way with ErrUnknownRunnerDialect. The runner only supports name-scheduled single-instance entries, so an entry carrying a custom argv (EntryConfig.Command), more than one instance (EntryConfig.Instances) or a routed crontab file (EntryConfig.DestinationFile) also panics at construction: such entries belong to an external scheduler produced by the generator, and the routed one would otherwise run twice — in-process and in the generated crontab — whenever both are live. Two supplied commands sharing one name panic with ErrDuplicateRunnerCommand — resolving the collision silently would drop one of them (an exclusivity wrapper over its wrapped command, most likely) and schedule the survivor unnoticed. A command whose Flags() returns the same flag instances on every call panics with ErrSharedRunnerCommandFlags: the runner dispatches overlapping invocations of one command, the cli library writes parse state into the flag instances, and shared instances would race. An entry that names a system user (EntryConfig.User) stays runnable — in-process every job runs as the process user, so the runner keeps the entry and Run logs one warning naming the affected commands, letting the one Configuration keep driving both the generated manifests and the runner. */
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

        if true == sharesFlagInstances(command.Flags(), command.Flags()) {
            exception.Panic(
                exception.NewError(
                    "cron: runner command returns the same flag instances on every Flags() call; the runner dispatches overlapping invocations and the cli library writes parse state into the instances, so Flags() must build fresh instances per call",
                    exceptioncontract.Context{
                        "commandName": command.Name(),
                    },
                    ErrSharedRunnerCommandFlags,
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

/* mustLoadConfiguredLocation resolves the zone a configuration declared, nil when it declared none. A name the standard library cannot load is a wiring mistake and panics here, beside the malformed schedules and the unknown command names — a scheduler that silently fell back to the process zone would run every job at the right clock time in the wrong place, which is the one failure nobody notices until the reports are wrong. */
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

/* nowInZone is the one clock reading this runner makes. Everything below it — the schedule matcher's calendar fields, the wall-minute chain's offset folding, the document's own timestamp — reads the time it answers, so the zone is applied here and in no other place. */
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

/* argumentsOfEntry reads the arguments this entry runs under, none when it declares none. The slice needs no copy here: Schedule already copied what the registrant handed in, which is where the configuration states that ownership, and a second copy would be a guard nothing could ever observe. */
func argumentsOfEntry(scheduled *ScheduledCommand) []string {
    if nil == scheduled.Config {
        return nil
    }

    return scheduled.Config.Arguments
}

/* timeoutOfEntry resolves the deadline one entry runs under: its own when it sets one, the runner default when it leaves it at zero, and none at all when it sets a negative one — the explicit opt-out that reads the same as the default and is kept so an entry can say so deliberately rather than by omission. */
func timeoutOfEntry(scheduled *ScheduledCommand) time.Duration {
    if nil == scheduled.Config || 0 == scheduled.Config.Timeout {
        return defaultCommandTimeout
    }

    return scheduled.Config.Timeout
}

/* gracefulTimeoutOfEntry reads the unwind window this entry names for itself, zero when it names none. The runner default is applied where the window is used rather than here, so a runner whose default was replaced governs every entry that did not ask for its own — which is what a caller replacing it means by it. A negative value is not an opt-out: there is nothing to opt out of, since without a deadline the window is never reached, and it reads as unset so an entry cannot ask for its scope to be torn down the instant the deadline lands. */
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

/* sharesFlagInstances reports whether the two flag slices contain a common flag instance. The cli library writes parse state into the flag instances it is handed, so a command whose Flags() memoizes and returns the same instances would make the runner's overlapping invocations race on them. */
func sharesFlagInstances(first []clicontract.Flag, second []clicontract.Flag) bool {
    for _, firstFlag := range first {
        if nil == firstFlag {
            continue
        }

        firstValue := reflect.ValueOf(firstFlag)
        if reflect.Ptr != firstValue.Kind() {
            continue
        }

        for _, secondFlag := range second {
            if nil == secondFlag {
                continue
            }

            secondValue := reflect.ValueOf(secondFlag)
            if reflect.Ptr == secondValue.Kind() && firstValue.Pointer() == secondValue.Pointer() {
                return true
            }
        }
    }

    return false
}

func (instance *RunnerCommand) Name() string {
    return "melody:cron:run"
}

func (instance *RunnerCommand) Description() string {
    return "Run the cron Configuration in-process, invoking each scheduled command when it is due"
}

/* the standard flags join the runner's own because the framework rewrites -v/-vv into --verbosity for every command: without them, the runner was among the only melody commands to die on the framework's own convention with "flag provided but not defined". They are honoured rather than merely accepted: every dispatched minute renders the same envelope every other melody command renders, so `melody:cron:run --once --format=json | jq` is a document rather than the empty stream it used to be — a stream indistinguishable, to the deploy step reading it, from a missing binary. */
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

    /* the flag WINS over the configuration: the zone is a property of the schedule, but an operator running one invocation against another region's calendar — a catch-up after a migration, a rehearsal — says so at the invocation. It is refused by name rather than by panic, because a flag is what the caller typed, and a mistyped zone deserves the command's own error rather than a stack trace. */
    if timezoneName := commandContext.String(flagNameTimezone); "" != timezoneName {
        location, loadErr := loadLocation(timezoneName)
        if nil != loadErr {
            return loadErr
        }

        instance.location = location
    }

    if true == commandContext.Bool(flagNameOnce) {
        startedAt := instance.nowInZone()
        report, runErr := instance.dispatchDue(runtimeInstance, startedAt, true, true)()

        return instance.renderRunReport(commandContext, option, startedAt, report, runErr)
    }

    instance.reporting = &runReporting{
        commandContext: commandContext,
        option:         option,
        reportIdle:     commandContext.Bool(flagNameReportIdle),
    }

    return instance.runLoop(runtimeInstance)
}

/*
declareSchedule writes what this process is driving before it drives anything.
A scheduler built over a configuration emptied by a refactor, or whose entries
an environment gate filtered away, used to run forever, exit successfully and
write not one byte on any channel: nothing distinguished a healthy scheduler
from one that would never dispatch anything, and the absence was noticed days
later, when the nightly sweep turned out not to have run. The empty case is a
warning for the same reason the generator's nothingToWrite is one.
*/
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

/*
renderRunReport writes one evaluated minute as the envelope every other melody
command writes: meta, data, warnings, error. The runner used to declare and
validate --format and then answer zero bytes on every path, so a deploy step
spelled `app melody:cron:run --once --format=json | jq` received an empty
stream — indistinguishable, to the step reading it, from a missing binary.

Under json each minute is one closed document on its own line, so the scheduler
loop is a stream a consumer can follow live at constant memory rather than a
summary that would only arrive when the process dies. Under text it is one
line, which is what a supervisor's log wants.

The writer is guarded because the loop hands each minute's wait to a goroutine
of its own — two minutes' documents may complete in any order and must not
interleave inside one line.
*/
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

/* runLoop wakes at each minute boundary until the runtime context is cancelled. The evaluation is pinned to the minute the (monotonic) timer was armed for, so a wall-clock step between arming and firing neither replays nor skips a minute; the armed minute's wall rendering is then reconciled against the last evaluated minute by reconcileWallClock, which resolves daylight-saving transitions and larger clock jumps with vixie-cron semantics. The chain anchor and the first armed minute derive from one clock read — two reads could straddle a minute boundary and manufacture a jump that never happened. A wake that re-arrives at the wall minute the previous wake already dispatched is skipped: a backward wall step inside the armed window makes the loop arm for that minute a second time, and dispatching it again would run every wildcard entry twice seconds apart — the repeated wall minute of a daylight-saving fall-back is a different case, since a whole hour of other minutes runs in between. Due commands are dispatched without waiting for them, so arming the next minute never blocks on a running job; command failures are logged by the dispatch as the commands complete. On cancellation the loop stops ticking and waits for the in-flight jobs — their contexts derive from the runtime context, so the cancellation has already reached them — before returning. */
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

            /* the wait runs on a goroutine of its own so arming the next minute never blocks on a running job — the loop's own rule — and it counts towards the in-flight wait so a shutdown does not cut the last minute's document off mid-line */
            instance.inFlight.Add(1)

            go func(waitForMinute func() (dueReport, error), minuteStartedAt time.Time) {
                defer instance.inFlight.Done()

                report, runErr := waitForMinute()

                /* a minute that dispatched nothing is silent by default: a per-minute document for an entry that runs once a night is 1439 lines saying nothing. --report-idle asks for them, which is how a consumer tells a live scheduler with nothing due from a dead one */
                if 0 == len(report.Ran) && false == reporting.reportIdle {
                    return
                }

                _ = instance.renderRunReport(reporting.commandContext, reporting.option, minuteStartedAt, report, runErr)
            }(wait, instance.nowInZone())
        }
    }
}

/* runDue invokes every entry whose schedule matches the given minute and waits for all of them, aggregating command failures so one bad job neither aborts the others nor is silently dropped. It answers the failure alone; the caller that also wants the document dispatches directly. */
func (instance *RunnerCommand) runDue(runtimeInstance runtimecontract.Runtime, at time.Time) error {
    _, runErr := instance.dispatchDue(runtimeInstance, at, true, true)()

    return runErr
}

/* dueRun is one dispatched command inside a minute's document: what ran, under which schedule and argv, the run id its own records carry, how long it took and whether it failed. The error text is empty rather than absent on a run that succeeded and the argument list is empty rather than null for an entry that declares none, so both fields keep their json type across rows. */
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

/* dueReport is one evaluated minute: the minute itself, how many entries the runner drives at all, and the runs it dispatched for it. Both counts matter to a consumer — a minute with an empty ran list and a configured count of zero is a scheduler that will never do anything, while the same list with a configured count of seven is simply a quiet minute. */
type dueReport struct {
    At         time.Time `json:"at"`
    Configured int       `json:"configured"`
    Ran        []dueRun  `json:"ran"`
}

/* dispatchDue starts every entry of the requested classes whose schedule matches the given minute, each in its own goroutine, so scheduled commands run as independently as crontab processes: one slow job delays neither its minute-mates nor the caller. Each failure is logged as its command completes, and the returned wait function blocks until every command launched here has finished and answers the minute's document beside their aggregated failure — the --once mode calls it directly, while the scheduler loop hands it to a goroutine of its own so arming the next minute never waits on a running job. */
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

            invokedAt := time.Now()
            runId, invokeErr := instance.invoke(runtimeInstance, launchedEntry)

            /* the document classifies the run the same way the aggregate does: a shutdown cancellation is a clean stop, so it is reported as cancelled rather than failed and the two never disagree about the same run */
            cancelled := nil != invokeErr && true == isShutdownCancellation(runtimeInstance, invokeErr)

            failureMutex.Lock()
            runs = append(runs, dueRun{
                Command:              launchedEntry.commandName,
                Schedule:             launchedEntry.schedule,
                Arguments:            argumentsOrEmpty(launchedEntry.arguments),
                RunId:                runId,
                DurationMilliseconds: time.Since(invokedAt).Milliseconds(),
                Failed:               nil != invokeErr && false == cancelled,
                Cancelled:            cancelled,
                Error:                errorTextOrEmpty(invokeErr),
            })
            failureMutex.Unlock()

            if true == instance.reportRunOutcome(runtimeInstance, launchedEntry, runId, invokeErr, cancelled) {
                failureMutex.Lock()
                failedCommands = append(failedCommands, launchedEntry.commandName)
                failures = append(failures, invokeErr)
                failureMutex.Unlock()
            }
        }(entry)
    }

    return func() (dueReport, error) {
        launched.Wait()

        failureMutex.Lock()
        report := dueReport{At: at, Configured: len(instance.entries), Ran: append([]dueRun{}, runs...)}
        aggregated := append([]error{}, failures...)
        aggregatedNames := append([]string{}, failedCommands...)
        failureMutex.Unlock()

        /* the runs are ordered by the command name rather than by the order they happened to finish in: the entries of one minute run concurrently, so completion order is scheduling luck and a document that changes shape between two identical minutes cannot be diffed */
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

/* argumentsOrEmpty keeps the document's arguments field a list on every row, empty rather than null for the entry that declares none — which is most of them. It is the same rule errorTextOrEmpty keeps for the error text and failedCommandsOf for the failed list: a field whose json type changes with the outcome cannot be consumed at all, and `jq '.data.ran[].arguments | length'` died on the first row for a job configured without arguments. The empty slice is the document's own, so the configuration it was read from is not touched. */
func argumentsOrEmpty(arguments []string) []string {
    if nil == arguments {
        return []string{}
    }

    return arguments
}

/* errorTextOrEmpty keeps the document's error field a string on every row: absent on a run that succeeded means a field whose json type changes with the outcome, which is the shape the machine contracts of this family were taken off. */
func errorTextOrEmpty(err error) string {
    if nil == err || true == isNilInterface(err) {
        return ""
    }

    return err.Error()
}

/* clockJumpResetThreshold is the vixie-cron three-hour bound on wall-clock reconciliation: a jump smaller than this (a daylight-saving transition, a suspend, an ntp step) is resolved minute by minute, while a jump of at least this much in either direction is treated as a clock reset and the minute chain re-anchors to the current minute without catch-up. */
const clockJumpResetThreshold = 3 * time.Hour

/* minuteEvaluation names one wall minute the runner evaluates and which entry classes run at it. */
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

/* wallMinuteTime materializes a wall minute index as a utc time whose rendered calendar fields equal that wall minute. Schedule matching reads only the rendered fields, so a minute that never existed on the local calendar — the span a daylight-saving spring-forward skips — still evaluates, which is what lets fixed-time entries catch up across the gap. */
func wallMinuteTime(index int64) time.Time {
    return time.Unix(index*60, 0).UTC()
}

/* reconcileWallClock is the vixie-cron virtual-time algorithm: it compares the wall rendering of the minute the loop woke for against the last wall minute already evaluated and decides which wall minutes to evaluate for which entry class, where the chain continues, and whether the wake deserves a log line. A one-minute advance evaluates the current minute for both classes. A larger forward jump below the reset threshold — a daylight-saving spring-forward, a suspend, an ntp step — evaluates the current minute for wildcard entries and every skipped wall minute plus the current one for fixed-time entries, so a schedule pinned inside the gap still runs exactly once. A jump of zero or backward below the threshold — a daylight-saving fall-back repeats the wall hour, a sub-minute backward step repeats one wall minute — evaluates the current minute for wildcard entries only and leaves the chain anchored, exactly as vixie crond does: wildcard entries follow the wall clock (an every-minute job fires once per absolute minute, even on the minute whose wall rendering repeats the anchor), while fixed-time entries stay suppressed until the wall clock passes the anchor again and cannot run twice. A jump of at least the threshold in either direction re-anchors to the current minute without catch-up and returns a note for the log. The function is pure — no clock, no timer — so the callers own every side effect. */
func reconcileWallClock(previousTarget time.Time, current time.Time) ([]minuteEvaluation, time.Time, string) {
    /* the current minute is evaluated and reported as the real local time it is, offset and all: schedule matching reads only the rendered calendar fields, which the local form and the offset-folded utc materialization share by construction, but the minute also becomes the document's timestamp — and the utc materialization used to be printed there as a real instant, off from the truth by the whole zone offset and disagreeing with the --once mode, which always reported local time. Only the catch-up minutes strictly before the current one stay utc-materialized: they are the span a spring-forward skipped, which has no local representation to print. */
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
            evaluations = append(evaluations, minuteEvaluation{at: wallMinuteTime(index), runFixedTime: true})
        }

        evaluations = append(evaluations, minuteEvaluation{at: currentMinute, runFixedTime: true})

        return evaluations, currentMinute, ""
    }

    /* the wall clock stepped back less than the threshold (or repeats the anchor minute itself), so the just-woken minute repeats a span that already ran: wildcard entries follow the wall clock, fixed-time entries stay suppressed behind the unchanged anchor. */
    evaluations := []minuteEvaluation{{at: currentMinute, runWildcard: true}}

    return evaluations, previousTarget, ""
}

/* invoke runs one command on a child runtime: a fresh scope so scoped services do not bleed across ticks, and a context derived from the runner's so a shutdown reaches the command in flight, carrying the entry's deadline so a command that never finishes does not run for the life of the process. The command context is dispatched through the cli library with the command's declared flags, so unset flags read their declared defaults, the output writers are usable and the parsed arguments are initialized — the same surface a command sees under the cli entry point, except that an error carrying an exit code is returned instead of exiting: the cli library's default handler calls os.Exit on such an error, which under the cli entry point ends a finished process but here would take the whole scheduler down with the one job. A panic inside the command is recovered and reported as an error, and a child scope close failure is joined onto the command's own error, so one bad job neither takes the scheduler down nor hides a shutdown failure.

The command runs on its own goroutine, which is what lets the deadline be enforced against a command that never looks at its context. The escalation is deliberate and has three steps. The deadline cancels the command's context — a signal, not a kill. A command that watches it unwinds inside the graceful window and has its own error reported together with the timeout, which is the path essentially every command takes. Only a command that ignores the cancellation reaches the third step: once the window lapses the run is abandoned, the failure is reported at warning naming the entry and how long it overran, the scope is closed under it, and it stops counting towards the shutdown wait.

That last step is a kill and is described as one. Closing the scope gives the resources back — the pools, the handles, everything the run built — and the alternative is the leak this exists to stop, one scope and one goroutine per matching minute until the process ends. It is not free of consequence: a scope.Get from the closed scope returns an error, but scope.MustGet panics, and the recover here covers only the goroutine this runner started. A command that hands work to a goroutine of its own and resolves from the scope there can therefore take the process down. That is why the window before the kill is measured in minutes rather than seconds, and why a command whose unwind is legitimately slow should name its own with EntryConfig.GracefulTimeout rather than rely on the default. */
func (instance *RunnerCommand) invoke(runtimeInstance runtimecontract.Runtime, entry *scheduledRunEntry) (runId string, invokeErr error) {
    /* the run's id is minted first and returned beside the outcome, so the runner's own records about this run — the failure line, the abandon warning — carry the same cronRunId the run's records carry */
    runId = logging.GenerateProcessId()

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

        /* the command's own failure stays the wrapped one, so its context and its whole cause chain still reach the record, and the close failure travels beside it in the context rather than through a join that would empty both */
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

    /* each run gets the console identity the cli entry point installs into its own scope, because the child scope is a sibling and inherits nothing: a fresh ProcessContext under the per-run id minted above — overlapping runs of one entry stay distinguishable — and a logger derived from the run's own, so the job's records carry the runner's processId AND the run's cronRunId. Without these, a command reading the documented console doors works launched by hand and fails under the scheduler. */
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

    /* the job's own output belongs in the journal, not on the process stdout. Due entries run concurrently, one goroutine each, and they all held the same os.Stdout: the reports of several jobs arrived interleaved, in ANSI colour nobody asked for, and nothing of them reached the log file the operator actually reads. Captured here, one job's output is one record carrying the entry and the run id, so the runner's promise that it writes nothing to the command output itself is true on every format. */
    capturedOutput := newScheduledOutputCapture()
    defer func() {
        instance.reportScheduledOutput(runtimeInstance, entry, runId, capturedOutput, invokeErr)
    }()

    completed := make(chan error, 1)
    go func() {
        completed <- runScheduledCommand(childContext, entry, childRuntime, capturedOutput)
    }()

    /* the abandon signal sits one unwind grace PAST the deadline, so a command that honours its cancelled context always reports its own outcome and only a command that ignores it is abandoned. An entry that opted out of the deadline never abandons: a nil channel blocks forever. */
    var abandon <-chan time.Time
    if 0 < entry.timeout {
        abandonTimer := time.NewTimer(abandonDelayOf(entry.timeout, instance.gracefulTimeoutOf(entry)))
        defer abandonTimer.Stop()

        abandon = abandonTimer.C
    }

    select {
    case runErr := <-completed:
        /* a command that returned nil finished its work, whatever the clock did in the same instant; only a failure is attributed to the deadline */
        if nil != runErr && true == errors.Is(childContext.Err(), context.DeadlineExceeded) {
            return runId, instance.timeoutError(entry, false, runErr)
        }

        return runId, runErr
    case <-abandon:
        return runId, instance.resolveAbandonedRun(runtimeInstance, entry, runId, childContext, completed)
    }
}

/* scheduledOutputCaptureLimit bounds what one run may contribute to the journal. A job that prints a row per record would otherwise write its whole result set into a single log line, and the failure of the log — a rotated file filled in one run, a line no reader can open — would be caused by the very reporting meant to make the run visible. What was cut is counted and named in the record, so a truncated report never reads as a complete one. */
const scheduledOutputCaptureLimit = 64 * 1024

/* scheduledOutputCapture is the writer a scheduled command is handed in place of the process stdout. It always reports the full length as written: a command must not discover the runner's budget as a short write on its own report, which is an io.Writer contract violation and would be answered by half the standard library with an error the job then returns as its failure. */
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
        instance.buffer.Write(content[:remaining])
        instance.droppedBytes = instance.droppedBytes + len(content) - remaining

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

/* reportScheduledOutput files what the job wrote, once, under the identity of the run that wrote it. A run that printed nothing files nothing: the record exists to carry output, and an empty one per entry per minute would bury the runner's own lines. The level follows the run — a job that failed has its report read beside its failure — and the failure itself is filed by the dispatch, which is where the aggregation happens. */
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

/*
withSiblingFailure carries a second failure inside the context of the error the
runner returns, instead of joining the two.

errors.Join answers an Unwrap of []error, and exception.LogContext resolves a
record's context and cause chain from the nearest *Error a deep errors.As
reaches — one branch of the join. A joined pair of failures therefore filed
whichever branch the search reached first and dropped the other whole: a
scope-close failure joined onto a job's own rich error left exactly one of the
two stories in the record the operator reads. The same two failures carried
down one single-valued chain file completely, which is the difference this
exists to remove.

The primary error stays the one wrapped, so every errors.Is a caller performs
still answers, and the sibling travels under the given prefix with its own
context and cause chain where it has them. Both extra fields keep their json
type on every row — an empty object and an empty list rather than null — since
a field whose type changes with the outcome cannot be consumed.
*/
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

/* reportRunOutcome files the record one dispatched run leaves behind and answers whether it counts towards the minute's failure aggregate. A command that watched its context and unwound when the shutdown cancelled it did exactly what the runner's GoDoc asks of it: that is a clean stop, recorded at warning, named, and kept out of the aggregate. A deadline the entry asked for stays a failure.

It is handed the classification instead of computing its own, and that is the whole point of the parameter: the document row a few lines above is built from the same answer, and computing it twice read the runtime's context twice. A shutdown landing between the two reads made the row report failed while this branch treated the same run as a clean stop and returned early — one run with two verdicts, a document saying a job failed and a process exiting 0 over it. The window is microseconds wide and cannot be produced from the outside, which is why this is a function: a test hands it the two answers separately. */
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

    /* the record carries the cronRunId the run's own records carry: the id is minted precisely so overlapping runs of one entry stay distinguishable, and a failure record tied to neither stream defeated it at the one moment it matters */
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

/* resolveAbandonedRun answers a run whose abandon window lapsed. It reads the completion channel one last time before announcing the kill, because the outer select picks at random between two ready cases: a command that returned in the same instant the timer fired has already done its work, and without this read its outcome would be discarded and the scope reported as closed under running code — by nothing but scheduling luck. The window cannot be produced from the outside, which is why the branch is a function: a test hands it a channel that already carries the answer. */
func (instance *RunnerCommand) resolveAbandonedRun(
    runtimeInstance runtimecontract.Runtime,
    entry *scheduledRunEntry,
    runId string,
    childContext context.Context,
    completed <-chan error,
) error {
    select {
    case runErr := <-completed:
        if nil != runErr && true == errors.Is(childContext.Err(), context.DeadlineExceeded) {
            return instance.timeoutError(entry, false, runErr)
        }

        return runErr
    default:
    }

    /* the kill is announced where an operator reads logs, not only in the aggregated dispatch error: this is the one path on which the runner tears a scope down under code that is still executing, and it names the entry and the window it overran so the answer — a longer GracefulTimeout, or a command that watches its context — is readable from the line itself */
    if logger := logging.LoggerFromRuntime(runtimeInstance); nil != logger {
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

    return instance.timeoutError(entry, true, nil)
}

/* abandonDelayOf places the abandon signal one unwind grace past the deadline without letting the sum wrap. Both durations are caller-supplied and unbounded, so a graceful window written as the largest duration — the natural spelling of "never abandon this one" — would overflow into a negative delay, and a timer armed with one fires at once: every run of that entry would be killed the instant it started, its scope closed under it, and reported as having ignored a cancellation it never received. Saturating at the maximum keeps that spelling meaning what it says. */
func abandonDelayOf(timeout time.Duration, gracefulTimeout time.Duration) time.Duration {
    if gracefulTimeout > math.MaxInt64-timeout {
        return math.MaxInt64
    }

    return timeout + gracefulTimeout
}

/* commandContextOf derives the command's context from the runner's, adding the entry's deadline when it has one. A non-positive timeout is the explicit opt-out and yields a merely cancellable context — the shape a job whose duration is genuinely unbounded needs, and the shape every entry had before the deadline existed. */
func commandContextOf(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
    if 0 >= timeout {
        return context.WithCancel(parent)
    }

    return context.WithTimeout(parent, timeout)
}

/* runScheduledCommand dispatches one command and turns a panic inside it into an error. The recovery belongs on the goroutine that runs the command and nowhere else: a panic is only recoverable on the goroutine that raises it, so a recover left behind on invoke's goroutine would let a panicking job take the whole scheduler process down. */
func runScheduledCommand(
    ctx context.Context,
    entry *scheduledRunEntry,
    childRuntime runtimecontract.Runtime,
    capturedOutput io.Writer,
) (runErr error) {
    defer func() {
        if recovered := recover(); nil != recovered {
            /* the panic value travels as the CAUSE and the stack is captured here, where the frames of the job that raised it are still on the goroutine. Stringified into the context alone, an error-shaped panic collapsed to its bare message: the context naming the parameter, the chain naming the connection that refused, and any file and line were all gone from the record, and the operator woken by a nightly job was told only that it panicked. The event dispatcher's recovery boundary states the same reason for the same shape. */
            panicFailure := exception.NewError(
                "cron: scheduled command panicked",
                exceptioncontract.Context{
                    "commandName": entry.commandName,
                    "panicValue":  fmt.Sprintf("%v", recovered),
                    "panicStack":  string(debug.Stack()),
                },
                panicCause(recovered),
            )

            if nil == runErr {
                /* the ordinary case: the command never returned, so there is nothing to join — the panic error IS the run's outcome and is assigned as such. The genuine two-failure case is the branch below, which does not join either, because a join files one branch and drops the sibling */
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

    /* the entry's own arguments travel with the command name, so a job declaring Arguments: []string{"--format=json"} runs under the posture it asked for. The dispatch used to hand the child nothing but its name, which left every job on the declared defaults of its flags whatever the entry configured, and the generated manifest for the same entry ran a different command line. */
    return melodycli.DispatchCommand(
        ctx,
        entry.command,
        childRuntime,
        append([]string{entry.commandName}, entry.arguments...),
        capturedOutput,
    )
}

/* panicCause reads a recovered panic value as the cause of the error the recovery boundary fabricates in its place. It mirrors the PanicCause reading of the frozen majors' framework rather than calling a framework door, because melody/v3 exposes none for an integration to call. A typed nil answers no cause: its Error() would dereference a nil receiver at the first render. */
func panicCause(recovered any) error {
    recoveredErr, isRecoveredError := recovered.(error)
    if false == isRecoveredError || true == isNilInterface(recoveredErr) {
        return nil
    }

    return recoveredErr
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

/* timeoutError classifies a run the deadline ended, carrying the command's own failure where there is one. The two used to be joined, which answered every errors.Is a caller performs and nothing at all to exception.LogContext — the record lost the command's context and its whole cause chain. They now share one single-valued chain through commandTimeoutCause: the classification stays reachable by identity, the command's failure stays reachable by identity, and the chain the record is built from walks straight through both. */
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
