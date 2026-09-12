package cron

import (
    clicontract "github.com/precision-soft/melody/v3/cli/contract"
)

/* Commands returns the commands derivable from the configuration alone — today the generator. The runner is deliberately absent: it needs the RunnerCommands list and the RunnerDialect, which only the module configuration carries, so Module.RegisterCliCommands appends it beside these when they are wired. A nil configuration is read as an empty one, the reading NewGenerateCommand has always had; the module door is where nil is a wiring error and is refused as such. */
func Commands(configuration *Configuration) []clicontract.Command {
    return []clicontract.Command{
        NewGenerateCommand(configuration),
    }
}
