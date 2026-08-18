package config

import (
    "testing"
)

/* newCronConfiguration itself is proven live: registering the cron module boots NewRunnerCommand, which panics over a scheduled name absent from the runner commands, so a drift between the two halves cannot survive a boot. What is pinned here is the runner half of that correspondence. */
func TestCronRunnerCommandsCarryTheThreeScheduledCommands(t *testing.T) {
    commands := cronRunnerCommands()
    if 3 != len(commands) {
        t.Fatalf("expected the three scheduled commands, got %d", len(commands))
    }

    expectedNames := map[string]bool{
        "catalog:report:refresh": true,
        "product:list":           true,
        "app:info":               true,
    }
    for _, commandInstance := range commands {
        if false == expectedNames[commandInstance.Name()] {
            t.Fatalf("unexpected runner command %q", commandInstance.Name())
        }

        delete(expectedNames, commandInstance.Name())
    }
    if 0 != len(expectedNames) {
        t.Fatalf("missing runner commands: %v", expectedNames)
    }
}
