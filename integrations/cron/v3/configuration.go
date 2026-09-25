package cron

import (
    "time"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
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
    /* Arguments are the entry's own command-line arguments, appended after the command name wherever the entry runs: the in-process runner hands them to the child command and the generator renders them into the manifest line. A job declares its own output posture here, for example "--format=json"; Command instead replaces the binary and the command name as a whole shell line, which only the generated manifests run. */
    Arguments []string
    Instances int
    /* Timeout bounds one run of this entry under the in-process runner. Zero takes the runner default, which is no deadline, and a negative value asks for no deadline explicitly. The generated manifests ignore it, since an external scheduler bounds its own jobs. */
    Timeout time.Duration
    /* GracefulTimeout is how long the entry's command is given to unwind after Timeout cancelled its context, before the runner stops waiting and closes the run's container scope under it; zero takes the runner default. It is reached only by a command that ignores its cancelled context, and only when Timeout set one. */
    GracefulTimeout time.Duration
}

type ScheduledCommand struct {
    CommandName string
    Config      *EntryConfig
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

/* InTimezone names the IANA zone, "Europe/Bucharest" for instance, under which the in-process runner evaluates every schedule; unset, it evaluates on the process zone. A zone the standard library cannot load panics at construction. The generated manifests are run by an external scheduler that owns its zone, so the generator emits nothing for it. */
func (instance *Configuration) InTimezone(name string) *Configuration {
    instance.timezoneName = name

    return instance
}

/* TimezoneName answers the zone this configuration declared, empty when it declared none. */
func (instance *Configuration) TimezoneName() string {
    return instance.timezoneName
}

/* Schedule copies the entry configuration instead of retaining the caller's pointer, so a caller mutating its own struct after registration changes neither the manifests the generator emits nor a runner built later. */
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
