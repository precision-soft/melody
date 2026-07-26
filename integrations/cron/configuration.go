package cron

import (
    "time"

    clicontract "github.com/precision-soft/melody/cli/contract"
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
    Instances       int
    /* Timeout bounds one run of this entry under the in-process runner. Zero takes the runner default; a NEGATIVE value opts out of the deadline entirely, for work whose duration is genuinely unbounded. The generated manifests ignore it — an external scheduler bounds its own jobs, a kubernetes CronJob through activeDeadlineSeconds and a crontab line through timeout(1) — so setting it never changes what the generator emits. */
    Timeout time.Duration
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

func (instance *Configuration) Schedule(commandName string, config *EntryConfig) *Configuration {
    instance.entries = append(instance.entries, &ScheduledCommand{
        CommandName: commandName,
        Config:      config,
    })

    return instance
}

func (instance *Configuration) Entries() []*ScheduledCommand {
    return instance.entries
}
