package encrypt

import (
    "context"
    "fmt"
    "io"
    "os"
    "reflect"
    "strings"
    "sync"

    "github.com/precision-soft/melody/v4/bound"
    clicontract "github.com/precision-soft/melody/v4/cli/contract"
    "github.com/precision-soft/melody/v4/cli/output"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/logging"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
    "github.com/uptrace/bun"
)

const (
    migrateModeEncrypt   = "encrypt"
    migrateModeReencrypt = "reencrypt"
    migrateModeDecrypt   = "decrypt"
)

/* EncryptDatabaseCommandConfig declares what the bulk command runs against beside its cipher: exactly one of Database, a prebuilt handle, and DatabaseResolver, run at the first command run and memoized on success, a failure returned from the run and retried on the next one, so a registry-backed database is opened once the container is booted; both or neither is refused naming the two fields. CommandName names the command, melody:encrypt:database when left out, so a multi-context binary exposes one per compartment. */
type EncryptDatabaseCommandConfig struct {
    Database         *bun.DB
    DatabaseResolver func(ctx context.Context) (*bun.DB, error)
    CommandName      string

    /* SealBindings are the bound columns of the command's compartment, the module handing each command its own: a --column whose <table>.<column> is a declared label runs bound, and --seal-binding column=label binds a column of a table spelled otherwise to a declared label. A nil Reference and a label declared twice are refused at construction, naming the field and the index. */
    SealBindings []SealBindingConfig
}

/* NewEncryptDatabaseCommand builds the bulk encrypt, re-encrypt and decrypt command; a nil or typed-nil cipher and a config with both or neither database field panic, and so does a Database that is not a MySQL dialect, at construction. */
func NewEncryptDatabaseCommand(cipher Cipher, config EncryptDatabaseCommandConfig) *EncryptDatabaseCommand {
    if true == isNilCipher(cipher) {
        exception.Panic(exception.NewError("encrypt database command cipher is nil", map[string]any{"argument": "cipher"}, nil))
    }

    if (nil == config.Database) == (nil == config.DatabaseResolver) {
        exception.Panic(
            exception.NewError(
                "encrypt database command needs exactly one of a database and a database resolver",
                map[string]any{"fields": []string{"EncryptDatabaseCommandConfig.Database", "EncryptDatabaseCommandConfig.DatabaseResolver"}},
                nil,
            ),
        )
    }

    sealBindingsByLabel := make(map[string]SealBindingConfig, len(config.SealBindings))
    for position, bindingConfig := range config.SealBindings {
        if true == isNilSealBindingReference(bindingConfig.Reference) {
            exception.Panic(exception.NewError(
                "encrypt database command seal binding has no Reference",
                map[string]any{"field": "EncryptDatabaseCommandConfig.SealBindings", "index": position},
                nil,
            ))
        }

        label := bindingConfig.Reference.SealBinding()
        if _, repeated := sealBindingsByLabel[label]; true == repeated {
            exception.Panic(exception.NewError(
                "encrypt database command seal binding declares a label twice",
                map[string]any{"field": "EncryptDatabaseCommandConfig.SealBindings", "index": position, "sealBinding": label},
                nil,
            ))
        }

        sealBindingsByLabel[label] = bindingConfig
    }

    command := &EncryptDatabaseCommand{
        databaseResolver:    config.DatabaseResolver,
        cipher:              cipher,
        commandName:         config.CommandName,
        sealBindingsByLabel: sealBindingsByLabel,
    }

    if nil != config.Database {
        command.migrator = NewMigrator(config.Database, cipher)
    }

    return command
}

type EncryptDatabaseCommand struct {
    migrator            *Migrator
    databaseResolver    func(ctx context.Context) (*bun.DB, error)
    cipher              Cipher
    commandName         string
    sealBindingsByLabel map[string]SealBindingConfig
    resolveMutex        sync.Mutex
}

/* resolveMigrator runs the resolver under the lock, so racing runs cannot each open a database and leak the loser's pool; a success is memoized and a failure is retried on the next run. */
func (instance *EncryptDatabaseCommand) resolveMigrator(ctx context.Context) (*Migrator, error) {
    instance.resolveMutex.Lock()
    defer instance.resolveMutex.Unlock()

    if nil != instance.migrator {
        return instance.migrator, nil
    }

    database, resolveErr := instance.databaseResolver(ctx)
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
        &clicontract.IntFlag{Name: "batch", Value: defaultMigrateBatchSize, Usage: "rows per batch", Config: output.DecimalIntegerConfig},
        &clicontract.BoolFlag{Name: "deterministic", Usage: "seal the columns deterministically (searchable); without it a rotation keeps each stored value's mode, and a run onto the key a bound seal or an unbound column's seal already carries converts it to random"},
        &clicontract.StringSliceFlag{Name: "seal-binding", Usage: "column=label binding a --column to a declared seal binding whose label is not <table>.<column> (repeatable)"},
    }
}

func (instance *EncryptDatabaseCommand) Run(
    runtimeInstance runtimecontract.Runtime,
    commandContext *clicontract.CommandContext,
) error {
    specification := TableSpecification{
        Table:         commandContext.String("table"),
        PrimaryKey:    commandContext.String("primary-key"),
        Columns:       commandContext.StringSlice("column"),
        BatchSize:     batchSizeOf(commandContext.Int("batch")),
        Deterministic: commandContext.Bool("deterministic"),
    }

    /* the flag is named in the message, since the exit line does not print the cause */
    if _, batchSizeErr := resolveBatchSize(specification); nil != batchSizeErr {
        return exception.NewError(
            "--batch must be at least 1; zero selects the default",
            map[string]any{"batch": commandContext.Int("batch"), "field": "TableSpecification.BatchSize"},
            batchSizeErr,
        )
    }

    mode := commandContext.String("mode")
    targetKey := commandContext.String("target-key")
    ctx := runtimeInstance.Context()

    migrator, migratorErr := instance.resolveMigrator(ctx)
    if nil != migratorErr {
        return migratorErr
    }

    /* the run is refused naming the flags it lacks, before any row is read: the migrator refuses the same specification, but as the cause of the run's failure, which the terminal does not print. The database is resolved first, so a factory failure still surfaces at the first run. A flag present with an empty value is refused as one that is missing */
    if "" == specification.Table || "" == specification.PrimaryKey || false == everyColumnNamed(specification.Columns) {
        return exception.NewError("--table, --primary-key and at least one --column are required, none of them empty", map[string]any{"table": specification.Table, "primaryKey": specification.PrimaryKey, "columns": specification.Columns}, nil)
    }

    if migrateModeReencrypt == mode && "" == targetKey {
        return exception.NewError("mode reencrypt requires --target-key", nil, nil)
    }

    if migrateModeReencrypt != mode && "" != targetKey {
        return exception.NewError("--target-key is read by mode reencrypt alone", map[string]any{"mode": mode}, nil)
    }

    if migrateModeDecrypt == mode && true == specification.Deterministic {
        return exception.NewError("--deterministic is not read by mode decrypt", map[string]any{"mode": mode}, nil)
    }

    sealBindings, sealBindingsErr := instance.resolveSealBindings(specification.Table, specification.Columns, commandContext.StringSlice("seal-binding"))
    if nil != sealBindingsErr {
        return sealBindingsErr
    }

    specification.SealBindings = sealBindings

    /* both writing modes size their columns inside the run, since a non-strict sql_mode keeps an overflowing ciphertext that never authenticates; decrypting only shortens a value, so it needs no room */
    var stats migrateStats
    var runErr error

    switch mode {
    case migrateModeEncrypt:
        stats, runErr = migrator.migrateEncrypt(ctx, specification)
    case migrateModeReencrypt:
        stats, runErr = migrator.migrateReencrypt(ctx, specification, targetKey)
    case migrateModeDecrypt:
        stats, runErr = migrator.migrateDecrypt(ctx, specification)
    default:
        return exception.NewError("unknown mode", map[string]any{"mode": mode}, nil)
    }

    processed := stats.rows

    if nil != runErr {
        /* the reason of a refused run — an unknown or narrow column, an unlisted key — lives in the cause chain, which the process's exit line does not print, so the run writes it on the error stream, where the engine reports a failure and never where the command's document goes, escaped as the engine escapes its own banner since a driver refusal echoes the values it is handed; the error keeps its message, which the journal records */
        _, _ = fmt.Fprintln(
            commandErrorWriter(commandContext),
            escapeControlCharacters("encrypt database migration failed: "+readableCauseChain(exception.BuildCauseChain(runErr, 8)), false),
        )

        /* the rows already converted travel with the error, since they are what says what a re-run costs */
        return exception.NewError(
            "encrypt database migration failed",
            map[string]any{"table": specification.Table, "mode": mode, "processedRows": processed},
            runErr,
        )
    }

    if logger := logging.LoggerFromRuntime(runtimeInstance); nil != logger {
        logger.Info(
            "encrypt database migration finished",
            map[string]any{
                "table":               specification.Table,
                "mode":                mode,
                "rows":                processed,
                "boundFromUnbound":    stats.boundFromUnbound,
                "sealedFromPlaintext": stats.sealedFromPlaintext,
                "sealBindings":        boundLabels(specification.SealBindings),
            },
        )
    }

    return nil
}

/* resolveSealBindings binds each --column whose label is declared: the label --seal-binding names for it, <table>.<column> otherwise; a column with no declared label runs unbound. A pair that is not column=label, a column that is not one of --column or named twice, and a label the command was not handed are refused naming the flag before any query, since an unbound column run bound would write seals its plain type refuses. */
func (instance *EncryptDatabaseCommand) resolveSealBindings(table string, columns []string, flagValues []string) (map[string]ColumnSealBinding, error) {
    listed := make(map[string]bool, len(columns))
    for _, column := range columns {
        listed[column] = true
    }

    labelByColumn := make(map[string]string, len(flagValues))
    for _, flagValue := range flagValues {
        column, label, paired := strings.Cut(flagValue, "=")
        if false == paired || "" == column || "" == label {
            return nil, exception.NewError("--seal-binding must be column=label", map[string]any{"sealBinding": flagValue}, nil)
        }

        if false == listed[column] {
            return nil, exception.NewError("--seal-binding names a column that is not one of --column", map[string]any{"sealBinding": flagValue, "column": column}, nil)
        }

        if _, repeated := labelByColumn[column]; true == repeated {
            return nil, exception.NewError("--seal-binding names a column twice", map[string]any{"sealBinding": flagValue, "column": column}, nil)
        }

        if _, declared := instance.sealBindingsByLabel[label]; false == declared {
            return nil, exception.NewError("--seal-binding names a label that is not a declared seal binding of this command", map[string]any{"sealBinding": flagValue, "label": label}, nil)
        }

        /* the flag is for a table spelled otherwise than its label; a column whose own <table>.<column> is declared is bound by that declaration alone */
        if canonical := table + "." + column; canonical != label {
            if _, declared := instance.sealBindingsByLabel[canonical]; true == declared {
                return nil, exception.NewError("--seal-binding names a label other than the column's own declared seal binding", map[string]any{"sealBinding": flagValue, "declared": canonical}, nil)
            }
        }

        for boundColumn, boundLabel := range labelByColumn {
            if label == boundLabel {
                return nil, exception.NewError("--seal-binding names one label for two columns", map[string]any{"sealBinding": flagValue, "label": label, "column": boundColumn}, nil)
            }
        }

        labelByColumn[column] = label
    }

    var bindings map[string]ColumnSealBinding

    for _, column := range columns {
        label, named := labelByColumn[column]
        if false == named {
            label = table + "." + column
        }

        bindingConfig, declared := instance.sealBindingsByLabel[label]
        if false == declared {
            /* MySQL folds the case of a column name, the label does not: a --column spelled with another case would run unbound without a word */
            if foldedLabel := instance.declaredLabelFolding(label); "" != foldedLabel {
                return nil, exception.NewError("--table and --column differ only by case from a declared seal binding; spell them as the label does", map[string]any{"column": column, "sealBinding": foldedLabel}, nil)
            }

            continue
        }

        if nil == bindings {
            bindings = make(map[string]ColumnSealBinding, len(columns))
        }

        bindings[column] = ColumnSealBinding{Label: label, RefuseUnbound: bindingConfig.RefuseUnbound}
    }

    return bindings, nil
}

/* declaredLabelFolding answers the declared label equal to the given one under case folding, empty when none is */
func (instance *EncryptDatabaseCommand) declaredLabelFolding(label string) string {
    for declared := range instance.sealBindingsByLabel {
        if true == strings.EqualFold(declared, label) {
            return declared
        }
    }

    return ""
}

/* isNilSealBindingReference answers true for a typed nil behind the interface too, whose SealBinding would panic */
func isNilSealBindingReference(reference SealBindingReference) bool {
    if nil == reference {
        return true
    }

    reflected := reflect.ValueOf(reference)

    return reflect.Pointer == reflected.Kind() && true == reflected.IsNil()
}

/* boundLabels answers the label each bound column ran under, empty when the run bound none, so the closing record tells a bound run from an unbound one */
func boundLabels(sealBindings map[string]ColumnSealBinding) map[string]string {
    labels := make(map[string]string, len(sealBindings))
    for column, columnBinding := range sealBindings {
        labels[column] = columnBinding.Label
    }

    return labels
}

func batchSizeOf(flagValue int) bound.Count {
    if 0 == flagValue {
        return bound.Count{}
    }

    return bound.Of(flagValue)
}

/* commandErrorWriter is the stream a failure is reported on: the command's error writer, which the engine defaults to standard error, and standard error itself for one left nil, a typed nil included, as the engine reads it */
func commandErrorWriter(commandContext *clicontract.CommandContext) io.Writer {
    if false == isNilWriter(commandContext.ErrWriter) {
        return commandContext.ErrWriter
    }

    return os.Stderr
}

/* isNilWriter answers true for a typed nil behind the interface too, so a nil buffer set as the error writer falls back to standard error rather than panicking at the first write */
func isNilWriter(writer io.Writer) bool {
    if nil == writer {
        return true
    }

    reflected := reflect.ValueOf(writer)
    switch reflected.Kind() {
    case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func, reflect.Interface:
        return reflected.IsNil()
    default:
        return false
    }
}

/* readableCauseChain joins the chain's links for a terminal, leaving out a link the previous one already ends with, as an *fs.PathError ends with the errno it wraps */
func readableCauseChain(linkList []string) string {
    partList := make([]string, 0, len(linkList))

    for _, link := range linkList {
        if 0 < len(partList) {
            previous := partList[len(partList)-1]
            if previous == link || true == strings.HasSuffix(previous, ": "+link) {
                continue
            }
        }

        partList = append(partList, link)
    }

    return strings.Join(partList, ": ")
}

var _ clicontract.Command = (*EncryptDatabaseCommand)(nil)

/* everyColumnNamed answers whether the column list holds at least one entry and no empty one */
func everyColumnNamed(columnList []string) bool {
    if 0 == len(columnList) {
        return false
    }

    for _, column := range columnList {
        if "" == column {
            return false
        }
    }

    return true
}
