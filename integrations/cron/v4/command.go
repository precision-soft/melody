package cron

import (
    clicontract "github.com/precision-soft/melody/v4/cli/contract"
)

/* Commands returns the commands derivable from the configuration alone, the generator; the runner needs a RunnerConfig, so Module.RegisterCliCommands appends it when ModuleConfig.Runner lists commands. A nil configuration is read as an empty one; the generator carries the builtin dialects only, ModuleConfig.Templates being the door for others. */
func Commands(configuration *Configuration) []clicontract.Command {
    return []clicontract.Command{
        newGenerateCommand(configuration, nil),
    }
}
