package cron

import (
    clicontract "github.com/precision-soft/melody/v3/cli/contract"
)

/* Commands returns the commands derivable from the configuration alone, the generator; the runner needs the RunnerCommands list and the RunnerDialect, so Module.RegisterCliCommands appends it when they are wired. A nil configuration is read as an empty one. */
func Commands(configuration *Configuration) []clicontract.Command {
    return []clicontract.Command{
        NewGenerateCommand(configuration),
    }
}
