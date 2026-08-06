package cron

import (
    "testing"
    "time"

    clicontract "github.com/precision-soft/melody/cli/contract"
)

func TestCommandName_AnswersTheNameTheFactoryBuilds(t *testing.T) {
    name := CommandName(func() clicontract.Command { return newRecordingCommand("backup:run") })

    if "backup:run" != name {
        t.Fatalf("CommandName = %q, want %q", name, "backup:run")
    }
}

func TestNewConfiguration_StartsEmptyAndNonNil(t *testing.T) {
    configuration := NewConfiguration()

    if nil == configuration {
        t.Fatal("NewConfiguration must answer a configuration")
    }

    if 0 != len(configuration.Entries()) {
        t.Fatalf("a fresh configuration holds no entries, got %d", len(configuration.Entries()))
    }
}

/* Schedule keeps the order it was called in: the generator renders the entries in that order and the runner parses them in it, so a registry that reordered them would emit a crontab the caller cannot predict. */
func TestSchedule_KeepsEveryEntryInRegistrationOrder(t *testing.T) {
    configuration := NewConfiguration().
        Schedule("first:command", &EntryConfig{Schedule: &Schedule{Minute: "0"}}).
        Schedule("second:command", nil).
        Schedule("third:command", &EntryConfig{Timeout: time.Minute})

    entries := configuration.Entries()
    if 3 != len(entries) {
        t.Fatalf("expected the three scheduled entries, got %d", len(entries))
    }

    expectedNames := []string{"first:command", "second:command", "third:command"}
    for index, expectedName := range expectedNames {
        if expectedName != entries[index].CommandName {
            t.Fatalf("entry %d = %q, want %q", index, entries[index].CommandName, expectedName)
        }
    }

    if nil != entries[1].Config {
        t.Fatal("an entry scheduled without a config keeps that absence rather than being given one")
    }

    if time.Minute != entries[2].Config.Timeout {
        t.Fatalf("the entry's own config must reach the registry, got %s", entries[2].Config.Timeout)
    }
}

/* Schedule answers the configuration so registrations chain; a copy would drop every entry added after the first. */
func TestSchedule_AnswersTheSameConfiguration(t *testing.T) {
    configuration := NewConfiguration()

    if configuration != configuration.Schedule("backup:run", nil) {
        t.Fatal("Schedule must answer the configuration it registered into")
    }
}

/* the same command name can be scheduled twice on purpose — two different minutes of the same job — so the registry keeps both rather than folding them. */
func TestSchedule_KeepsTwoEntriesThatShareOneCommandName(t *testing.T) {
    configuration := NewConfiguration().
        Schedule("backup:run", &EntryConfig{Schedule: &Schedule{Minute: "0"}}).
        Schedule("backup:run", &EntryConfig{Schedule: &Schedule{Minute: "30"}})

    entries := configuration.Entries()
    if 2 != len(entries) {
        t.Fatalf("expected both registrations to survive, got %d", len(entries))
    }

    if "0" == entries[1].Config.Schedule.Minute {
        t.Fatal("the second registration must keep its own schedule")
    }
}
