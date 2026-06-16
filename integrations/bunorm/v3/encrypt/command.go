package encrypt

import (
    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/uptrace/bun"
)

func Commands(database *bun.DB, cipher Cipher) []clicontract.Command {
    return []clicontract.Command{
        NewEncryptDatabaseCommand(database, cipher),
    }
}
