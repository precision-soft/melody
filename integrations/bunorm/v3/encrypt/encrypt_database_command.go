package encrypt

import (
    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/logging"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "github.com/uptrace/bun"
)

const (
    migrateModeEncrypt   = "encrypt"
    migrateModeReencrypt = "reencrypt"
    migrateModeDecrypt   = "decrypt"
)

func NewEncryptDatabaseCommand(database *bun.DB, cipher Cipher) *EncryptDatabaseCommand {
    return &EncryptDatabaseCommand{
        migrator: NewMigrator(database, cipher),
    }
}

type EncryptDatabaseCommand struct {
    migrator *Migrator
}

func (instance *EncryptDatabaseCommand) Name() string {
    return "melody:encrypt:database"
}

func (instance *EncryptDatabaseCommand) Description() string {
    return "bulk encrypt, re-encrypt (rotate key) or decrypt the given columns of a table"
}

func (instance *EncryptDatabaseCommand) Flags() []clicontract.Flag {
    return []clicontract.Flag{
        &clicontract.StringFlag{Name: "table", Usage: "table to process"},
        &clicontract.StringFlag{Name: "primary-key", Value: "id", Usage: "orderable primary key column used for pagination"},
        &clicontract.StringSliceFlag{Name: "column", Usage: "encrypted column to process (repeatable)"},
        &clicontract.StringFlag{Name: "mode", Value: migrateModeEncrypt, Usage: "encrypt | reencrypt | decrypt"},
        &clicontract.StringFlag{Name: "target-key", Usage: "key id to re-encrypt under (mode=reencrypt)"},
        &clicontract.IntFlag{Name: "batch", Value: defaultMigrateBatchSize, Usage: "rows per batch"},
        &clicontract.BoolFlag{Name: "deterministic", Usage: "use deterministic (searchable) encryption for the columns"},
    }
}

func (instance *EncryptDatabaseCommand) Run(
    runtimeInstance runtimecontract.Runtime,
    commandContext *clicontract.CommandContext,
) error {
    spec := TableSpec{
        Table:         commandContext.String("table"),
        PrimaryKey:    commandContext.String("primary-key"),
        Columns:       commandContext.StringSlice("column"),
        BatchSize:     int(commandContext.Int("batch")),
        Deterministic: commandContext.Bool("deterministic"),
    }

    mode := commandContext.String("mode")
    ctx := runtimeInstance.Context()

    var processed int
    var runErr error

    switch mode {
    case migrateModeEncrypt:
        processed, runErr = instance.migrator.MigrateEncrypt(ctx, spec)
    case migrateModeReencrypt:
        targetKey := commandContext.String("target-key")
        if "" == targetKey {
            return exception.NewError("mode reencrypt requires --target-key", nil, nil)
        }
        processed, runErr = instance.migrator.MigrateReencrypt(ctx, spec, targetKey)
    case migrateModeDecrypt:
        processed, runErr = instance.migrator.MigrateDecrypt(ctx, spec)
    default:
        return exception.NewError("unknown mode", map[string]any{"mode": mode}, nil)
    }

    if nil != runErr {
        return runErr
    }

    if logger := logging.LoggerFromRuntime(runtimeInstance); nil != logger {
        logger.Info("encrypt database migration finished", map[string]any{"table": spec.Table, "mode": mode, "rows": processed})
    }

    return nil
}

var _ clicontract.Command = (*EncryptDatabaseCommand)(nil)
