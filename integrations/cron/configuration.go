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

/* Schedule copies the entry configuration instead of retaining the caller's pointer: the runner reads an entry's deadlines at every run and the generator re-reads its fields at every generation, so a caller mutating its own struct after registration changed validated behavior mid-process — an unsynchronized write racing the scheduler goroutine's reads. What was registered is what stays in force. */
func (instance *Configuration) Schedule(commandName string, config *EntryConfig) *Configuration {
    instance.entries = append(instance.entries, &ScheduledCommand{
        CommandName: commandName,
        Config:      copyEntryConfig(config),
    })

    return instance
}

func copyEntryConfig(config *EntryConfig) *EntryConfig {
    if nil == config {
        return nil
    }

    copied := *config

    if nil != config.Schedule {
        copiedSchedule := *config.Schedule
        copied.Schedule = &copiedSchedule
    }

    if nil != config.Command {
        copied.Command = append(make([]string, 0, len(config.Command)), config.Command...)
    }

    return &copied
}

/* Entries hands out a copy of the list; the entries behind the pointers belong to the registration, private since Schedule copies what it is given. */
func (instance *Configuration) Entries() []*ScheduledCommand {
    return append(make([]*ScheduledCommand, 0, len(instance.entries)), instance.entries...)
}
