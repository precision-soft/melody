package cron

import (
    "context"
    "errors"
    "fmt"
    "os"
    "reflect"
    "strings"
    "sync"
    "time"

    clicontract "github.com/precision-soft/melody/v2/cli/contract"
    "github.com/precision-soft/melody/v2/exception"
    exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
    "github.com/precision-soft/melody/v2/logging"
    "github.com/precision-soft/melody/v2/runtime"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

const flagNameOnce = "once"

/* scheduledRunEntry pairs a parsed schedule with the registered command it fires, resolved once at construction so the tick loop never looks a command up by name at run time. fixedTime carries the vixie-cron entry class the wall-clock reconciliation reads: a fixed-time entry pins both a minute and an hour, a wildcard entry leaves either as a plain or stepped wildcard. */
type scheduledRunEntry struct {
    commandName string
    command     clicontract.Command
    matcher     *scheduleMatcher
    fixedTime   bool
}

/* RunnerCommand runs the same cron Configuration in-process instead of emitting a manifest for an external scheduler: it evaluates each entry's schedule against the wall clock and invokes the corresponding registered command when it is due. A single-binary deployment (no crontab, no kubernetes) gets its scheduled work from the one Configuration that already drives the generator. The day-of-month / day-of-week combination follows the configured RunnerDialect — crontab by default, the vixie crond rule where a star-based day field (plain or stepped wildcard) counts as unrestricted and the day fields combine with and; the kubernetes dialect opts into the robfig scheduler behind the k8s template, where only the star-bit shapes (the plain or the unit-stepped wildcard, alone or inside a list) are unrestricted and a stepped wildcard day field with a step above one combines with or. Two genuinely restricted day fields combine with or in both dialects; the two real schedulers diverge only on the star-based shapes, which is inherent to the targets, so pick the dialect of the manifests the same Configuration generates.

Due commands run concurrently, each in its own goroutine, the way crontab starts an independent process per entry: one slow job delays neither the commands sharing its minute nor the scheduler loop, and an entry that runs longer than its own interval overlaps itself — wrap the command in a locker-backed exclusivity wrapper to serialize successive runs. Wall-clock jumps follow the vixie-cron virtual-time algorithm, documented on reconcileWallClock, so a schedule pinned inside a daylight-saving gap still runs exactly once. Multi-instance safety is left to composition — wrap each command in a distributed-lock exclusivity wrapper, or gate the whole runner behind a leader gate, before handing the commands in. */
type RunnerCommand struct {
    entries             []*scheduledRunEntry
    now                 func() time.Time
    inFlight            sync.WaitGroup
    userIgnoredCommands []string
}

/* NewRunnerCommand resolves every scheduled command name against the supplied commands and parses each schedule up front under the given day-combination dialect (the zero value is the crontab default); an entry naming a command that was not supplied, or carrying a malformed schedule, is a wiring error and panics at construction so it surfaces at boot rather than at the first tick, and a value naming no known RunnerDialect panics the same way with ErrUnknownRunnerDialect. The runner only supports name-scheduled single-instance entries, so an entry carrying a custom argv (EntryConfig.Command) or more than one instance (EntryConfig.Instances) also panics at construction: such entries belong to an external scheduler produced by the generator and would otherwise run without their configured shape. Two supplied commands sharing one name panic with ErrDuplicateRunnerCommand — resolving the collision silently would drop one of them (an exclusivity wrapper over its wrapped command, most likely) and schedule the survivor unnoticed. A command whose Flags() returns the same flag instances on every call panics with ErrSharedRunnerCommandFlags: the runner dispatches overlapping invocations of one command, the cli library writes parse state into the flag instances, and shared instances would race. An entry that names a system user (EntryConfig.User) stays runnable — in-process every job runs as the process user, so the runner keeps the entry and Run logs one warning naming the affected commands, letting the one Configuration keep driving both the generated manifests and the runner. */
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
            commandName: scheduled.CommandName,
            command:     command,
            matcher:     matcher,
            fixedTime:   matcher.fixedTime(),
        })
    }

    return &RunnerCommand{
        entries:             entries,
        now:                 time.Now,
        userIgnoredCommands: userIgnoredCommands,
    }
}

func scheduleOfEntry(scheduled *ScheduledCommand) *Schedule {
    if nil == scheduled.Config {
        return nil
    }

    return scheduled.Config.Schedule
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

func (instance *RunnerCommand) Flags() []clicontract.Flag {
    return []clicontract.Flag{
        &clicontract.BoolFlag{
            Name:  flagNameOnce,
            Usage: "evaluate every schedule once against the current time, run the commands that are due, and exit instead of running the scheduler loop",
        },
    }
}

func (instance *RunnerCommand) Run(
    runtimeInstance runtimecontract.Runtime,
    commandContext *clicontract.CommandContext,
) error {
    if 0 < len(instance.userIgnoredCommands) {
        if logger := logging.LoggerFromRuntime(runtimeInstance); nil != logger {
            logger.Warning(
                "cron runner ignores EntryConfig.User: in-process every job runs as the process user, unlike the generated manifests which run it as the configured one",
                map[string]any{"commands": strings.Join(instance.userIgnoredCommands, ", ")},
            )
        }
    }

    if true == commandContext.Bool(flagNameOnce) {
        return instance.runDue(runtimeInstance, instance.now())
    }

    return instance.runLoop(runtimeInstance)
}

/* runLoop wakes at each minute boundary until the runtime context is cancelled. The evaluation is pinned to the minute the (monotonic) timer was armed for, so a wall-clock step between arming and firing neither replays nor skips a minute; the armed minute's wall rendering is then reconciled against the last evaluated minute by reconcileWallClock, which resolves daylight-saving transitions and larger clock jumps with vixie-cron semantics. The chain anchor and the first armed minute derive from one clock read — two reads could straddle a minute boundary and manufacture a jump that never happened. A wake that re-arrives at the wall minute the previous wake already dispatched is skipped: a backward wall step inside the armed window makes the loop arm for that minute a second time, and dispatching it again would run every wildcard entry twice seconds apart — the repeated wall minute of a daylight-saving fall-back is a different case, since a whole hour of other minutes runs in between. Due commands are dispatched without waiting for them, so arming the next minute never blocks on a running job; command failures are logged by the dispatch as the commands complete. On cancellation the loop stops ticking and waits for the in-flight jobs — their contexts derive from the runtime context, so the cancellation has already reached them — before returning. */
func (instance *RunnerCommand) runLoop(runtimeInstance runtimecontract.Runtime) error {
    now := instance.now()
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

        now = instance.now()

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
            instance.dispatchDue(runtimeInstance, evaluation.at, evaluation.runFixedTime, evaluation.runWildcard)
        }
    }
}

/* runDue invokes every entry whose schedule matches the given minute and waits for all of them, aggregating command failures so one bad job neither aborts the others nor is silently dropped. */
func (instance *RunnerCommand) runDue(runtimeInstance runtimecontract.Runtime, at time.Time) error {
    return instance.dispatchDue(runtimeInstance, at, true, true)()
}

/* dispatchDue starts every entry of the requested classes whose schedule matches the given minute, each in its own goroutine, so scheduled commands run as independently as crontab processes: one slow job delays neither its minute-mates nor the caller. Each failure is logged as its command completes, and the returned wait function blocks until every command launched here has finished and reports their aggregated failure — the --once mode calls it, while the scheduler loop deliberately does not. */
func (instance *RunnerCommand) dispatchDue(
    runtimeInstance runtimecontract.Runtime,
    at time.Time,
    runFixedTime bool,
    runWildcard bool,
) func() error {
    var launched sync.WaitGroup

    var failureMutex sync.Mutex
    failedCommands := make([]string, 0)

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

            if invokeErr := instance.invoke(runtimeInstance, launchedEntry); nil != invokeErr {
                failureMutex.Lock()
                failedCommands = append(failedCommands, launchedEntry.commandName)
                failureMutex.Unlock()

                if logger := logging.LoggerFromRuntime(runtimeInstance); nil != logger {
                    logger.Error(
                        "cron runner command failed",
                        map[string]any{"commandName": launchedEntry.commandName, "error": invokeErr.Error()},
                    )
                }
            }
        }(entry)
    }

    return func() error {
        launched.Wait()

        if 0 < len(failedCommands) {
            return exception.NewError(
                "cron: one or more scheduled commands failed",
                exceptioncontract.Context{
                    "commands": strings.Join(failedCommands, ", "),
                },
                nil,
            )
        }

        return nil
    }
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
    previousIndex := wallMinuteIndex(previousTarget)
    currentIndex := wallMinuteIndex(current.Truncate(time.Minute))

    difference := currentIndex - previousIndex
    resetThresholdMinutes := int64(clockJumpResetThreshold / time.Minute)

    if difference >= resetThresholdMinutes || difference <= -resetThresholdMinutes {
        note := fmt.Sprintf(
            "the wall clock jumped %d minutes; re-anchoring to the current minute without catch-up",
            difference,
        )

        evaluations := []minuteEvaluation{{at: wallMinuteTime(currentIndex), runFixedTime: true, runWildcard: true}}

        return evaluations, wallMinuteTime(currentIndex), note
    }

    if 1 == difference {
        evaluations := []minuteEvaluation{{at: wallMinuteTime(currentIndex), runFixedTime: true, runWildcard: true}}

        return evaluations, wallMinuteTime(currentIndex), ""
    }

    if difference > 1 {
        evaluations := make([]minuteEvaluation, 0, difference+1)
        evaluations = append(evaluations, minuteEvaluation{at: wallMinuteTime(currentIndex), runWildcard: true})

        for index := previousIndex + 1; index <= currentIndex; index++ {
            evaluations = append(evaluations, minuteEvaluation{at: wallMinuteTime(index), runFixedTime: true})
        }

        return evaluations, wallMinuteTime(currentIndex), ""
    }

    /* the wall clock stepped back less than the threshold (or repeats the anchor minute itself), so the just-woken minute repeats a span that already ran: wildcard entries follow the wall clock, fixed-time entries stay suppressed behind the unchanged anchor. */
    evaluations := []minuteEvaluation{{at: wallMinuteTime(currentIndex), runWildcard: true}}

    return evaluations, previousTarget, ""
}

/* invoke runs one command on a child runtime: a fresh scope so scoped services do not bleed across ticks, and a cancellable context derived from the runner's so a shutdown reaches the command in flight. The command context is dispatched through the cli library with the command's declared flags, so unset flags read their declared defaults, the output writers are usable and the parsed arguments are initialized — the same surface a command sees under the cli entry point, except that an error carrying an exit code is returned instead of exiting: the cli library's default handler calls os.Exit on such an error, which under the cli entry point ends a finished process but here would take the whole scheduler down with the one job. A panic inside the command is recovered and reported as an error, and a child scope close failure is joined onto the command's own error, so one bad job neither takes the scheduler down nor hides a shutdown failure. */
func (instance *RunnerCommand) invoke(runtimeInstance runtimecontract.Runtime, entry *scheduledRunEntry) (invokeErr error) {
    childContext, cancel := context.WithCancel(runtimeInstance.Context())
    defer cancel()

    childScope := runtimeInstance.Container().NewScope()
    defer func() {
        if closeErr := childScope.Close(); nil != closeErr {
            invokeErr = errors.Join(
                invokeErr,
                exception.NewError(
                    "cron: child scope close failed after the scheduled command",
                    exceptioncontract.Context{
                        "commandName": entry.commandName,
                    },
                    closeErr,
                ),
            )
        }
    }()

    defer func() {
        if recovered := recover(); nil != recovered {
            invokeErr = errors.Join(
                invokeErr,
                exception.NewError(
                    "cron: scheduled command panicked",
                    exceptioncontract.Context{
                        "commandName": entry.commandName,
                        "panicValue":  fmt.Sprintf("%v", recovered),
                    },
                    nil,
                ),
            )
        }
    }()

    childRuntime := runtime.New(childContext, childScope, runtimeInstance.Container())

    commandContext := &clicontract.CommandContext{
        Name:      entry.commandName,
        Flags:     entry.command.Flags(),
        Writer:    os.Stdout,
        ErrWriter: os.Stderr,
        Action: func(actionContext context.Context, actionCommandContext *clicontract.CommandContext) error {
            return entry.command.Run(childRuntime, actionCommandContext)
        },
        ExitErrHandler: func(handlerContext context.Context, handlerCommandContext *clicontract.CommandContext, handlerErr error) {
        },
    }

    return commandContext.Run(childContext, []string{entry.commandName})
}

var _ clicontract.Command = (*RunnerCommand)(nil)
