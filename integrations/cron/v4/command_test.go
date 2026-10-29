package cron

import (
    "testing"
)

func TestCommandsReturnsGenerateCommand(t *testing.T) {
    commands := Commands(NewConfiguration())

    if 1 != len(commands) {
        t.Fatalf("expected 1 command, got %d", len(commands))
    }

    if "melody:cron:generate" != commands[0].Name() {
        t.Fatalf("unexpected command name %q", commands[0].Name())
    }
}
