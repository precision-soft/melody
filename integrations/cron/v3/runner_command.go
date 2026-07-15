package cron

import (
    "context"
    "strings"
    "time"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/precision-soft/melody/v3/logging"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

const flagNameOnce = "once"

/* scheduledRunEntry pairs a parsed schedule with the registered command it fires, resolved once at construction so the tick loop never looks a command up by name at run time. */
type scheduledRunEntry struct {
    commandName string
    command     clicontract.Command
    matcher     *scheduleMatcher
}

/* RunnerCommand runs the same cron Configuration in-process instead of emitting a manifest for an external scheduler: it evaluates each entry's schedule against the wall clock and invokes the corresponding registered command when it is due. A single-binary deployment (no crontab, no kubernetes) gets its scheduled work from the one Configuration that already drives the generator, so the two can never drift. Multi-instance safety is left to composition — wrap each command in lock.NewExclusiveCommand, or gate the whole runner behind a lock.LeaderGate, before handing the commands in. */
type RunnerCommand struct {
    entries []*scheduledRunEntry
    now     func() time.Time
}

/* NewRunnerCommand resolves every scheduled command name against the supplied commands and parses each schedule up front; an entry naming a command that was not supplied, or carrying a malformed schedule, is a wiring error and panics at construction so it surfaces at boot rather than at the first tick. */
func NewRunnerCommand(configuration *Configuration, commands ...clicontract.Command) *RunnerCommand {
    if nil == configuration {
        configuration = NewConfiguration()
    }

    lookup := make(map[string]clicontract.Command, len(commands))
    for _, command := range commands {
        if nil == command {
            continue
        }

        lookup[command.Name()] = command
    }

    entries := make([]*scheduledRunEntry, 0, len(configuration.Entries()))
    for _, scheduled := range configuration.Entries() {
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

        matcher, matcherErr := newScheduleMatcher(scheduleOfEntry(scheduled))
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
        })
    }

    return &RunnerCommand{
        entries: entries,
        now:     time.Now,
    }
}

func scheduleOfEntry(scheduled *ScheduledCommand) *Schedule {
    if nil == scheduled.Config {
        return nil
    }

    return scheduled.Config.Schedule
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
    if true == commandContext.Bool(flagNameOnce) {
        return instance.runDue(runtimeInstance, instance.now())
    }

    return instance.runLoop(runtimeInstance)
}

/* runLoop ticks on the minute boundary and runs whatever is due until the runtime context is cancelled; a tick that a command fails is logged and the loop continues, because one failing job must not stop the scheduler. */
func (instance *RunnerCommand) runLoop(runtimeInstance runtimecontract.Runtime) error {
    for {
        now := instance.now()
        nextMinute := now.Truncate(time.Minute).Add(time.Minute)

        timer := time.NewTimer(nextMinute.Sub(now))

        select {
        case <-runtimeInstance.Context().Done():
            timer.Stop()

            return nil
        case <-timer.C:
        }

        if runErr := instance.runDue(runtimeInstance, instance.now()); nil != runErr {
            if logger := logging.LoggerFromRuntime(runtimeInstance); nil != logger {
                logger.Error("cron runner tick reported command failures", map[string]any{"error": runErr.Error()})
            }
        }
    }
}

/* runDue invokes every entry whose schedule matches the given minute, aggregating command failures so one bad job neither aborts the others nor is silently dropped. */
func (instance *RunnerCommand) runDue(runtimeInstance runtimecontract.Runtime, at time.Time) error {
    failedCommands := make([]string, 0)

    for _, entry := range instance.entries {
        if false == entry.matcher.Matches(at) {
            continue
        }

        if invokeErr := instance.invoke(runtimeInstance, entry); nil != invokeErr {
            failedCommands = append(failedCommands, entry.commandName)

            if logger := logging.LoggerFromRuntime(runtimeInstance); nil != logger {
                logger.Error(
                    "cron runner command failed",
                    map[string]any{"commandName": entry.commandName, "error": invokeErr.Error()},
                )
            }
        }
    }

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

/* invoke runs one command on a child runtime: a fresh scope so scoped services do not bleed across ticks, and a cancellable context derived from the runner's so a shutdown reaches the command in flight. */
func (instance *RunnerCommand) invoke(runtimeInstance runtimecontract.Runtime, entry *scheduledRunEntry) error {
    childContext, cancel := context.WithCancel(runtimeInstance.Context())
    defer cancel()

    childScope := runtimeInstance.Container().NewScope()
    defer childScope.Close()

    childRuntime := runtime.New(childContext, childScope, runtimeInstance.Container())

    commandContext := &clicontract.CommandContext{Name: entry.commandName}

    return entry.command.Run(childRuntime, commandContext)
}

var _ clicontract.Command = (*RunnerCommand)(nil)
