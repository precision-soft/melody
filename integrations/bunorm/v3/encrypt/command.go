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

/* CommandsFromResolver is Commands against a lazily-resolved database: the resolver runs at the first command run instead of at construction, so the database is opened once the container is fully booted. */
func CommandsFromResolver(databaseResolver func() (*bun.DB, error), cipher Cipher) []clicontract.Command {
    return []clicontract.Command{
        NewEncryptDatabaseCommandFromResolver(databaseResolver, cipher),
    }
}
