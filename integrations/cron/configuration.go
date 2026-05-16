package cron

import (
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
