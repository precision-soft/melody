package cron

import (
    clicontract "github.com/precision-soft/melody/v3/cli/contract"
)

func Commands(configuration *Configuration) []clicontract.Command {
    return []clicontract.Command{
        NewGenerateCommand(configuration),
    }
}
