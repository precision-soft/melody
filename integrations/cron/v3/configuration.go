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
    /* Arguments are the entry's own command-line arguments, appended after the command name wherever this entry runs: the in-process runner hands them to the child command, and the generator renders them into the manifest line, so one Configuration keeps driving both without the two halves running different commands. This is where a job declares its own output posture — Arguments: []string{"--format=json"} — because the runner has no posture to lend it and every job decides its own. Command is the other thing: a whole shell line replacing the binary and the command name, which only the generated manifests can run. */
    Arguments []string
    Instances int
    /* Timeout bounds one run of this entry under the in-process runner. Zero takes the runner default, which is no deadline at all, so a run is bounded only where an entry asks to be; a NEGATIVE value says the same thing deliberately rather than by omission. The generated manifests ignore it — an external scheduler bounds its own jobs, a kubernetes CronJob through activeDeadlineSeconds and a crontab line through timeout(1) — so setting it never changes what the generator emits. */
    Timeout time.Duration
    /* GracefulTimeout bounds the runner’s wait after Timeout cancels the command. When it expires, the runner closes the scope while uncooperative command code may continue. Zero uses the runner default. */
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

/* InTimezone names the zone the IN-PROCESS runner evaluates its schedules under, as an IANA name — "Europe/Bucharest". Unset, the runner evaluates on the process zone, which is what it always did.

   It is declared once for the whole configuration rather than per entry because a zone is a property of the schedule an operator reads, not of one job: an entry written "0 3 * * *" means three in the morning wherever the business keeps its books, and two entries meaning two different three-in-the-mornings is a crontab nobody can read. A zone the standard library cannot load is a wiring mistake and panics at construction, beside the malformed schedules and the unknown command names.

   It reaches the in-process runner ALONE. The generated manifests are run by an external scheduler whose zone belongs to that scheduler and to the container it runs in, not to this configuration; the generator therefore emits nothing for it, and the readme says so where the zone is documented. */
func (instance *Configuration) InTimezone(name string) *Configuration {
    instance.timezoneName = name

    return instance
}

/* TimezoneName answers the zone this configuration declared, empty when it declared none. */
func (instance *Configuration) TimezoneName() string {
    return instance.timezoneName
}

/* Schedule copies the entry configuration. Later caller mutations do not change future manifests or runners built from the schedule. */
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

/* Entries returns independent copies of the list, scheduled commands, entry configurations and schedules. */
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
