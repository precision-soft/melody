package encrypt

import (
    "sync"

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

/* NewEncryptDatabaseCommandWithName is NewEncryptDatabaseCommand under an explicit command name, so a multi-context binary can expose one bulk command per compartment (melody:encrypt:database:<context>). */
func NewEncryptDatabaseCommandWithName(database *bun.DB, cipher Cipher, commandName string) *EncryptDatabaseCommand {
    if "" == commandName {
        exception.Panic(exception.NewError("encrypt database command name is empty", nil, nil))
    }

    return &EncryptDatabaseCommand{
        migrator:    NewMigrator(database, cipher),
        commandName: commandName,
    }
}

/* NewEncryptDatabaseCommandFromResolver builds the command against a lazily-resolved database: the resolver runs at the first command run rather than at construction, so a registry-backed database is opened once the container is fully booted instead of at module registration. A resolver success is memoized and reused by subsequent runs; a resolver error is returned from the run and retried on the next one. */
func NewEncryptDatabaseCommandFromResolver(databaseResolver func() (*bun.DB, error), cipher Cipher) *EncryptDatabaseCommand {
    if nil == databaseResolver {
        exception.Panic(exception.NewError("encrypt database command database resolver is nil", nil, nil))
    }

    if nil == cipher {
        exception.Panic(exception.NewError("encrypt database command cipher is nil", nil, nil))
    }

    return &EncryptDatabaseCommand{
        databaseResolver: databaseResolver,
        cipher:           cipher,
    }
}

/* NewEncryptDatabaseCommandFromResolverWithName is NewEncryptDatabaseCommandFromResolver under an explicit command name, so a multi-context binary can expose one lazily-resolved bulk command per compartment (melody:encrypt:database:<context>). */
func NewEncryptDatabaseCommandFromResolverWithName(
    databaseResolver func() (*bun.DB, error),
    cipher Cipher,
    commandName string,
) *EncryptDatabaseCommand {
    if "" == commandName {
        exception.Panic(exception.NewError("encrypt database command name is empty", nil, nil))
    }

    command := NewEncryptDatabaseCommandFromResolver(databaseResolver, cipher)
    command.commandName = commandName

    return command
}

type EncryptDatabaseCommand struct {
    migrator         *Migrator
    databaseResolver func() (*bun.DB, error)
    cipher           Cipher
    commandName      string
    resolveMutex     sync.Mutex
}

/* resolveMigrator returns the eagerly-built migrator, or builds it from the database resolver at the first run and memoizes the success, so a resolved database is reused across runs while a failed resolution surfaces as the run error and is retried on the next run. The memoization is synchronized and the resolver runs under the lock, so racing runs cannot each open a database and leak the loser's pool — that is all the lock promises: two runs migrating the same table still see each other's guarded updates as skipped rows and report the run incomplete, which a re-run then clears. */
func (instance *EncryptDatabaseCommand) resolveMigrator() (*Migrator, error) {
    instance.resolveMutex.Lock()
    defer instance.resolveMutex.Unlock()

    if nil != instance.migrator {
        return instance.migrator, nil
    }

    database, resolveErr := instance.databaseResolver()
    if nil != resolveErr {
        return nil, resolveErr
    }

    instance.migrator = NewMigrator(database, instance.cipher)

    return instance.migrator, nil
}

func (instance *EncryptDatabaseCommand) Name() string {
    if "" != instance.commandName {
        return instance.commandName
    }

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
    commandContext clicontract.Context,
) error {
    /* a negative batch silently became the default of 500, so the operator who believed they had throttled the run had not; it is refused by name, while zero keeps selecting the default the flag documents */
    batchFlag := commandContext.Int("batch")
    if 0 > batchFlag {
        return exception.NewError("--batch must not be negative; zero selects the default", map[string]any{"batch": batchFlag}, nil)
    }

    spec := TableSpec{
        Table:         commandContext.String("table"),
        PrimaryKey:    commandContext.String("primary-key"),
        Columns:       commandContext.StringSlice("column"),
        BatchSize:     batchFlag,
        Deterministic: commandContext.Bool("deterministic"),
    }

    mode := commandContext.String("mode")
    targetKey := commandContext.String("target-key")
    ctx := runtimeInstance.Context()

    migrator, migratorErr := instance.resolveMigrator()
    if nil != migratorErr {
        return migratorErr
    }

    if migrateModeReencrypt == mode && "" == targetKey {
        return exception.NewError("mode reencrypt requires --target-key", nil, nil)
    }

    /* both writing modes size their columns up front themselves (see MigrateEncrypt / MigrateReencrypt): a server left in a non-strict sql_mode accepts an overflowing UPDATE, keeps a ciphertext that will never authenticate and reports the row as migrated, so the check lives inside the run and covers the programmatic caller too. Decrypting only ever shortens a value, so it needs no room. */
    var processed int
    var runErr error

    switch mode {
    case migrateModeEncrypt:
        processed, runErr = migrator.MigrateEncrypt(ctx, spec)
    case migrateModeReencrypt:
        processed, runErr = migrator.MigrateReencrypt(ctx, spec, targetKey)
    case migrateModeDecrypt:
        processed, runErr = migrator.MigrateDecrypt(ctx, spec)
    default:
        return exception.NewError("unknown mode", map[string]any{"mode": mode}, nil)
    }

    if nil != runErr {
        /* a run that stops halfway leaves the column mixed, and the rows already converted are the one number that says what a re-run costs and whether the state is explainable — it used to be dropped with the error */
        return exception.NewError(
            "encrypt database migration failed",
            map[string]any{"table": spec.Table, "mode": mode, "processedRows": processed},
            runErr,
        )
    }

    if logger := logging.LoggerFromRuntime(runtimeInstance); nil != logger {
        logger.Info("encrypt database migration finished", map[string]any{"table": spec.Table, "mode": mode, "rows": processed})
    }

    return nil
}

var _ clicontract.Command = (*EncryptDatabaseCommand)(nil)
