package cron

import (
    "time"

    clicontract "github.com/precision-soft/melody/v2/cli/contract"
)

func CommandName[T clicontract.Command](factory func() T) string {
    return factory().Name()
}

type EntryConfig struct {
    Schedule        *Schedule
    User            string
    LogFileName     string
    LogFileNameRaw  bool
    LogDisabled     bool
    DestinationFile string
    Command         []string
    /* Arguments are the entry's own command-line arguments, appended after the command name wherever this entry runs: the in-process runner hands them to the child command, and the generator renders them into the manifest line, so one Configuration keeps driving both without the two halves running different commands. This is where a job declares its own output posture — Arguments: []string{"--format=json"} — because the runner has no posture to lend it and every job decides its own. Command is the other thing: a whole shell line replacing the binary and the command name, which only the generated manifests can run. */
    Arguments []string
    Instances int
    /* Timeout bounds one run of this entry under the in-process runner. Zero takes the runner default, which is no deadline at all, so a run is bounded only where an entry asks to be; a NEGATIVE value says the same thing deliberately rather than by omission. The generated manifests ignore it — an external scheduler bounds its own jobs, a kubernetes CronJob through activeDeadlineSeconds and a crontab line through timeout(1) — so setting it never changes what the generator emits. */
    Timeout time.Duration
    /* GracefulTimeout is how long this entry's command is given to unwind after Timeout cancelled its context, before the runner stops waiting for it and closes the run's container scope under it. Zero takes the runner default. It is reached only by a command that ignores its cancelled context, and only when Timeout set one; a command that watches it returns well inside the window and reports its own error alongside the timeout. Set it above the default for work whose honest unwind is slower — a large batch to flush, a long transaction to roll back. */
    GracefulTimeout time.Duration
}

type ScheduledCommand struct {
    CommandName string
    Config      *EntryConfig
}

type Configuration struct {
    entries []*ScheduledCommand
}

func NewConfiguration() *Configuration {
    return &Configuration{
        entries: []*ScheduledCommand{},
    }
}

/* Schedule copies the entry configuration instead of retaining the caller's pointer: the generator re-reads an entry's fields at every generation, so a caller mutating its own struct after registration changed the manifests emitted afterwards. The in-process runner is not the consumer at risk — it photographs each entry's deadlines into its own run entry at construction, so a mutation landing after that cannot reach a scheduler already running — but a runner built later reads the same fields the generator does. What was registered is what stays in force, for both. */
func (instance *Configuration) Schedule(commandName string, config *EntryConfig) *Configuration {
    instance.entries = append(instance.entries, &ScheduledCommand{
        CommandName: commandName,
        Config:      copyEntryConfig(config),
    })

    return instance
}

func copySchedule(schedule *Schedule) *Schedule {
    if nil == schedule {
        return nil
    }

    copied := *schedule

    return &copied
}

func copyEntryConfig(config *EntryConfig) *EntryConfig {
    if nil == config {
        return nil
    }

    copied := *config
    copied.Schedule = copySchedule(config.Schedule)

    if nil != config.Command {
        copied.Command = append(make([]string, 0, len(config.Command)), config.Command...)
    }

    if nil != config.Arguments {
        copied.Arguments = append(make([]string, 0, len(config.Arguments)), config.Arguments...)
    }

    return &copied
}

/* Entries hands out copies all the way down: the list, each ScheduledCommand and each EntryConfig behind it, schedule included. Copying the list alone was not enough, because ScheduledCommand and EntryConfig are exported structs with exported fields — a caller writing through the pointer it was handed rewrote the registration itself, which is the exact mutation Schedule took a copy to prevent, arriving through the other door. */
func (instance *Configuration) Entries() []*ScheduledCommand {
    copied := make([]*ScheduledCommand, 0, len(instance.entries))

    for _, scheduled := range instance.entries {
        copied = append(copied, &ScheduledCommand{
            CommandName: scheduled.CommandName,
            Config:      copyEntryConfig(scheduled.Config),
        })
    }

    return copied
}
