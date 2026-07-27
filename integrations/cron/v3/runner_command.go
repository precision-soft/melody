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

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/precision-soft/melody/v3/logging"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

const flagNameOnce = "once"

/* scheduledRunEntry pairs a parsed schedule with the registered command it fires, resolved once at construction so the tick loop never looks a command up by name at run time. fixedTime carries the vixie-cron entry class the wall-clock reconciliation reads: a fixed-time entry pins both a minute and an hour, a wildcard entry leaves either as a plain or stepped wildcard. */
type scheduledRunEntry struct {
    commandName     string
    command         clicontract.Command
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
    entries             []*scheduledRunEntry
    now                 func() time.Time
    unwindGrace         time.Duration
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
            commandName:     scheduled.CommandName,
            command:         command,
            matcher:         matcher,
            fixedTime:       matcher.fixedTime(),
            timeout:         timeoutOfEntry(scheduled),
            gracefulTimeout: gracefulTimeoutOfEntry(scheduled),
        })
    }

    return &RunnerCommand{
        entries:             entries,
        now:                 time.Now,
        unwindGrace:         commandUnwindGrace,
        userIgnoredCommands: userIgnoredCommands,
    }
}

func scheduleOfEntry(scheduled *ScheduledCommand) *Schedule {
    if nil == scheduled.Config {
        return nil
    }

    return scheduled.Config.Schedule
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

/* invoke runs one command on a child runtime: a fresh scope so scoped services do not bleed across ticks, and a context derived from the runner's so a shutdown reaches the command in flight, carrying the entry's deadline so a command that never finishes does not run for the life of the process. The command context is dispatched through the cli library with the command's declared flags, so unset flags read their declared defaults, the output writers are usable and the parsed arguments are initialized — the same surface a command sees under the cli entry point, except that an error carrying an exit code is returned instead of exiting: the cli library's default handler calls os.Exit on such an error, which under the cli entry point ends a finished process but here would take the whole scheduler down with the one job. A panic inside the command is recovered and reported as an error, and a child scope close failure is joined onto the command's own error, so one bad job neither takes the scheduler down nor hides a shutdown failure.

The command runs on its own goroutine, which is what lets the deadline be enforced against a command that never looks at its context. The escalation is deliberate and has three steps. The deadline cancels the command's context — a signal, not a kill. A command that watches it unwinds inside the graceful window and has its own error reported together with the timeout, which is the path essentially every command takes. Only a command that ignores the cancellation reaches the third step: once the window lapses the run is abandoned, the failure is reported at warning naming the entry and how long it overran, the scope is closed under it, and it stops counting towards the shutdown wait.

That last step is a kill and is described as one. Closing the scope gives the resources back — the pools, the handles, everything the run built — and the alternative is the leak this exists to stop, one scope and one goroutine per matching minute until the process ends. It is not free of consequence: a scope.Get from the closed scope returns an error, but scope.MustGet panics, and the recover here covers only the goroutine this runner started. A command that hands work to a goroutine of its own and resolves from the scope there can therefore take the process down. That is why the window before the kill is measured in minutes rather than seconds, and why a command whose unwind is legitimately slow should name its own with EntryConfig.GracefulTimeout rather than rely on the default. */
func (instance *RunnerCommand) invoke(runtimeInstance runtimecontract.Runtime, entry *scheduledRunEntry) (invokeErr error) {
    childContext, cancel := commandContextOf(runtimeInstance.Context(), entry.timeout)
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

    completed := make(chan error, 1)
    go func() {
        completed <- runScheduledCommand(childContext, commandContext, entry)
    }()

    /* the abandon signal sits one unwind grace PAST the deadline, so a command that honours its cancelled context always reports its own outcome and only a command that ignores it is abandoned. An entry that opted out of the deadline never abandons: a nil channel blocks forever. */
    var abandon <-chan time.Time
    if 0 < entry.timeout {
        abandonTimer := time.NewTimer(entry.timeout + instance.gracefulTimeoutOf(entry))
        defer abandonTimer.Stop()

        abandon = abandonTimer.C
    }

    select {
    case runErr := <-completed:
        /* a command that returned nil finished its work, whatever the clock did in the same instant; only a failure is attributed to the deadline */
        if nil != runErr && true == errors.Is(childContext.Err(), context.DeadlineExceeded) {
            return errors.Join(instance.timeoutError(entry, false), runErr)
        }

        return runErr
    case <-abandon:
        /* the kill is announced where an operator reads logs, not only in the aggregated dispatch error: this is the one path on which the runner tears a scope down under code that is still executing, and it names the entry and the window it overran so the answer — a longer GracefulTimeout, or a command that watches its context — is readable from the line itself */
        if logger := logging.LoggerFromRuntime(runtimeInstance); nil != logger {
            logger.Warning(
                "cron: scheduled command ignored its cancelled context for the whole graceful window and is being abandoned; its container scope is closed under it while it may still be running",
                exceptioncontract.Context{
                    "commandName":     entry.commandName,
                    "timeout":         entry.timeout.String(),
                    "gracefulTimeout": instance.gracefulTimeoutOf(entry).String(),
                },
            )
        }

        return instance.timeoutError(entry, true)
    }
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
    commandContext *clicontract.CommandContext,
    entry *scheduledRunEntry,
) (runErr error) {
    defer func() {
        if recovered := recover(); nil != recovered {
            runErr = errors.Join(
                runErr,
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

    return commandContext.Run(ctx, []string{entry.commandName})
}

/* timeoutError reports a run the deadline cut short. The abandoned form is the louder one, and deliberately so: that command is still running, against a cancelled context and a closed scope, and it no longer counts towards the shutdown wait — a state an operator has to be able to read out of a log line rather than infer from a memory graph. */
func (instance *RunnerCommand) timeoutError(entry *scheduledRunEntry, abandoned bool) error {
    if true == abandoned {
        return exception.NewError(
            "cron: scheduled command exceeded its timeout and did not return; it was abandoned and its container scope closed while it may still be running",
            exceptioncontract.Context{
                "commandName": entry.commandName,
                "timeout":     entry.timeout.String(),
                "unwindGrace": instance.unwindGrace.String(),
            },
            ErrCommandTimeout,
        )
    }

    return exception.NewError(
        "cron: scheduled command was cancelled by its timeout",
        exceptioncontract.Context{
            "commandName": entry.commandName,
            "timeout":     entry.timeout.String(),
        },
        ErrCommandTimeout,
    )
}

var _ clicontract.Command = (*RunnerCommand)(nil)
