package cron

import (
    "github.com/precision-soft/melody/v4/bound"
    clicontract "github.com/precision-soft/melody/v4/cli/contract"
)

func CommandName[T clicontract.Command](factory func() T) string {
    return factory().Name()
}

/* EntryConfig is one scheduled entry. Schedule is required; User, the log file fields, DestinationFile and Command are the manifest's. A limit outside its rule is refused naming the field and the entry's command name, at NewRunnerCommand and, for Instances, by the generator too. */
type EntryConfig struct {
    Schedule        *Schedule
    User            string
    LogFileName     string
    LogFileNameRaw  bool
    LogDisabled     bool
    DestinationFile string
    Command         []string
    /* Arguments are the entry's own command-line arguments, appended after the command name wherever the entry runs: the in-process runner hands them to the child command and the generator renders them into the manifest line. A job declares its own output posture here, for example "--format=json"; Command instead replaces the binary and the command name, which only the generated manifests run: the crontab renders it as a shell line, the k8s template as the exec-form "command" array that replaces the image entrypoint, with no shell. */
    Arguments []string
    /* Instances is the parallel runs the generated manifests expand the entry into: 1 when left out, at least 1, unlimited refused; more than 1 is refused by the in-process runner. */
    Instances bound.Count
    /* Timeout bounds one run of this entry under the in-process runner: RunnerConfig.Timeout when left out, at least 1ms, unlimited admitted for a run no deadline should end. The generated manifests ignore it, since an external scheduler bounds its own jobs. */
    Timeout bound.Duration
    /* GracefulTimeout is how long the entry's command is given to unwind after Timeout or the runner's shutdown cancelled its context, before the runner stops waiting and closes the run's container scope under it: RunnerConfig.GracefulTimeout when left out, at least 1ms, unlimited refused. It is reached only by a command that ignores its cancelled context. */
    GracefulTimeout bound.Duration
}

type ScheduledCommand struct {
    CommandName string
    Config      EntryConfig
}

type Configuration struct {
    entries      []*ScheduledCommand
    timezoneName string
}

func NewConfiguration() *Configuration {
    return &Configuration{
        entries: []*ScheduledCommand{},
    }
}

/* InTimezone names the IANA zone, "Europe/Bucharest" for instance, under which the in-process runner evaluates every schedule; unset, it evaluates on the process zone. The runner's --timezone flag overrides either for the invocation that passes it. A zone the standard library cannot load panics at construction. The generated manifests are run by an external scheduler that owns its zone, so the generator emits nothing for it. */
func (instance *Configuration) InTimezone(name string) *Configuration {
    instance.timezoneName = name

    return instance
}

/* TimezoneName answers the zone this configuration declared, empty when it declared none. */
func (instance *Configuration) TimezoneName() string {
    return instance.timezoneName
}

/* Schedule copies the entry configuration, its schedule and its lists, so a caller mutating its own schedule or lists after registration changes neither the manifests the generator emits nor a runner built later. */
func (instance *Configuration) Schedule(commandName string, config EntryConfig) *Configuration {
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

func copyEntryConfig(config EntryConfig) EntryConfig {
    copied := config
    copied.Schedule = copySchedule(config.Schedule)

    if nil != config.Command {
        copied.Command = append(make([]string, 0, len(config.Command)), config.Command...)
    }

    if nil != config.Arguments {
        copied.Arguments = append(make([]string, 0, len(config.Arguments)), config.Arguments...)
    }

    return copied
}

/* Entries hands out copies all the way down, the list, each ScheduledCommand and each EntryConfig behind it with its schedule, so a caller writing through a returned pointer cannot rewrite the registration. */
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
