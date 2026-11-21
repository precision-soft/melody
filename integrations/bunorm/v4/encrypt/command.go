package encrypt

import (
    clicontract "github.com/precision-soft/melody/v4/cli/contract"
)

/* Commands answers the bulk command as the slice a hand-wired RegisterCliCommands appends; the config is NewEncryptDatabaseCommand's */
func Commands(cipher Cipher, config EncryptDatabaseCommandConfig) []clicontract.Command {
    return []clicontract.Command{
        NewEncryptDatabaseCommand(cipher, config),
    }
}
