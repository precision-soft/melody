package migrate

import (
    "errors"
    "fmt"
    "io"
    "reflect"
    "strconv"
    "time"

    "github.com/precision-soft/melody/cli"
    "github.com/precision-soft/melody/cli/output"
    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
)

type commandOutput struct {
    writer    io.Writer
    arguments []string
    option    output.Option

    /* the json accumulation: under --format=json every print records instead of writing, and finish renders the one machine-readable document the cli runner's silenced banner promises — the flag was accepted and validated long before this package honoured it */
    messages   []string
    warnings   []string
    database   *databaseIdentity
    details    map[string]string
    migrations map[string][]string
    files      []string
}

/* newCommandOutput takes the command's positional arguments beside its writer and flags: the machine document declares an arguments field, and built without them it answered an empty list for every command, db:create included, whose one argument names the migration the document reports on. */
func newCommandOutput(writer io.Writer, arguments []string, option output.Option) *commandOutput {
    return &commandOutput{
        writer:    writer,
        arguments: append([]string{}, arguments...),
        option:    option,
    }
}

/* runnerOptionForCommand derives the per-query printer posture from the command's parsed flags: the command's writer and colour choice in text mode, a discarded writer under json — the document is the only byte the command may emit there. */
func runnerOptionForCommand(writer io.Writer, option output.Option) RunnerOption {
    if true == output.IsJsonFormat(option.Format) {
        return RunnerOption{Writer: io.Discard, NoColor: true}
    }

    return RunnerOption{Writer: writer, NoColor: option.NoColor}
}

func (instance *commandOutput) isJson() bool {
    return output.IsJsonFormat(instance.option.Format)
}

/* wantsDetail decides whether the detail blocks are collected at all, and it is deliberately not the same question as --verbose. Verbosity is a rendering decision about TEXT — the README says so in as many words — while the json document is the machine contract, and shaping it with a display flag left `db:migrate --format=json` answering {"data":{}} for a run that applied five migrations: a pipeline recording what it deployed learned nothing, and the flag its author would have needed is documented as affecting the plain-text output alone. Under json the blocks are always collected; under text --verbose still decides. The cost is one extra query per run — the database identity block — paid only by a run that asked for the machine document. */
func (instance *commandOutput) wantsDetail() bool {
    if true == instance.isJson() {
        return true
    }

    return instance.option.Verbose
}

/* finish is the command's one exit door: under --format=json it renders the accumulated document — the failure included — and in every mode it answers the error the command should return. The command's own failure stays the verdict; a rendering failure becomes one only when the command itself succeeded. */
func (instance *commandOutput) finish(command string, startedAt time.Time, runErr error) error {
    if false == instance.isJson() {
        return runErr
    }

    meta := output.NewMeta(command, instance.arguments, instance.option, startedAt, time.Since(startedAt), output.Version{})
    envelope := output.NewEnvelope(meta)

    data := map[string]any{}
    if 0 < len(instance.messages) {
        data["messages"] = instance.messages
    }
    if nil != instance.database {
        /* the absent database is json null, not the placeholder the text block renders: as a string, "no current database" was indistinguishable from a database named literally <null>, and a consumer had to know melody's own placeholder to read the field at all */
        currentDatabaseValue := (any)(nil)
        if nil != instance.database.CurrentDatabase {
            currentDatabaseValue = *instance.database.CurrentDatabase
        }

        data["database"] = map[string]any{
            "database": currentDatabaseValue,
            "host":     instance.database.Hostname,
            "port":     instance.database.Port,
            "user":     instance.database.CurrentUser,
            "version":  instance.database.Version,
        }
    }
    if 0 < len(instance.details) {
        data["details"] = instance.details
    }
    if 0 < len(instance.migrations) {
        data["migrations"] = instance.migrations
    }
    if 0 < len(instance.files) {
        data["files"] = instance.files
    }
    envelope.Data = data

    for _, warning := range instance.warnings {
        envelope.Warnings = append(envelope.Warnings, output.NewWarning("migrate.warning", warning, nil))
    }

    if nil != runErr {
        envelope.Error = output.NewError(
            "migrate.failed",
            runErr.Error(),
            errorDetailsOf(runErr),
            errorCauseOf(runErr),
        )
    }

    renderErr := output.Render(instance.writer, envelope, instance.option)
    if nil != runErr {
        return runErr
    }

    return renderErr
}

/* errorDetailsOf and errorCauseOf fill the two fields the json envelope always declared and always answered null. The machine document is the contract a pipeline reads, and it was the one rendering that threw away what the error already carried: at the same instant, over the same value, the journal filed the connection, the pool sizing, the deadlines and the whole cause chain, while stdout answered `"details":null,"cause":null` beside a single sentence.

   The details object is empty rather than null when the error carries no context, so the field keeps its json type on every failure — the rule the machine contracts of this family were put on. */
func errorDetailsOf(runErr error) map[string]any {
    details := map[string]any{}

    var provider exceptioncontract.ContextProvider
    /* the As target is read through the typed-nil door, not through a plain nil comparison: a typed-nil link satisfies As, passes that comparison and then takes a read lock on a nil receiver inside Context(), panicking in the very rendering that was reporting the failure — and the panic unwinds finish, so the document the pipeline reads is never written and the failure is lost. bun's migrator produces exactly that link: it wraps the application's migration-function error with %w after a plain nil test, and fmt records the operand before formatting it. errorCauseOf below reaches the same conclusion through BuildCauseChain, which skips typed-nil links of its own accord. */
    if true == errors.As(runErr, &provider) && false == isNilInterface(provider) {
        for key, value := range provider.Context() {
            details[key] = value
        }
    }

    return details
}

/* errorCauseOf answers the first link under the failure together with the whole chain beneath it, and nothing at all when the failure has no cause — a null there is the honest answer, unlike the null the field used to carry on every failure alike. */
func errorCauseOf(runErr error) *output.ErrorCause {
    causeChain := exception.BuildCauseChain(errors.Unwrap(runErr), 8)
    if 0 == len(causeChain) {
        return nil
    }

    return output.NewErrorCause(causeChain[0], map[string]any{"chain": causeChain})
}

/* isNilInterface answers whether the interface value is nil outright or holds a nil pointer, map, slice, channel or function: a typed nil passes a plain nil comparison and then panics on first use, far from the wiring mistake that produced it. Duplicated from the framework's internal package, which a separate module cannot import. */
func isNilInterface(value any) bool {
    if nil == value {
        return true
    }

    reflected := reflect.ValueOf(value)

    switch reflected.Kind() {
    case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func, reflect.Interface:
        return reflected.IsNil()
    default:
        return false
    }
}

/* pluralizeMigrations renders the applied count the way a log line reads it, so a single migration does not report "1 migrations" */
func pluralizeMigrations(count int) string {
    if 1 == count {
        return "1 migration"
    }

    return strconv.Itoa(count) + " migrations"
}

/* printTextSuccess writes the line a person reads and leaves the machine document untouched: under json the same run already carries the applied count, the group and the migration names as structured fields, so a prose duplicate would be a second and weaker spelling of what the consumer already has. */
func (instance *commandOutput) printTextSuccess(message string) {
    if true == instance.isJson() {
        return
    }

    instance.printSuccess(message)
}

/* every text door of the output escapes what it did not write itself before the terminal sees it — the applied line names the manager, the files block names the paths bun answered, the warning carries the close error off the wire — while the json branch hands the value to the document, whose printer escapes the C1 block itself. The warning was the one door that let its message through as sent, and its one caller with foreign text is the close failure of the migration connection. */
func (instance *commandOutput) printSuccess(message string) {
    if true == instance.isJson() {
        instance.messages = append(instance.messages, message)

        return
    }

    escapedMessage := escapeControlCharacters(message, false)

    if false == instance.option.NoColor {
        _, _ = fmt.Fprintf(instance.writer, "%s%s%s\n", cli.AnsiGreen, escapedMessage, cli.AnsiReset)
    } else {
        _, _ = fmt.Fprintln(instance.writer, escapedMessage)
    }
}

func (instance *commandOutput) printWarning(message string) {
    if true == instance.isJson() {
        instance.warnings = append(instance.warnings, message)

        return
    }

    escapedMessage := escapeControlCharacters(message, false)

    if false == instance.option.NoColor {
        _, _ = fmt.Fprintf(instance.writer, "%s%sWARNING: %s%s\n", cli.AnsiYellow, cli.AnsiBold, escapedMessage, cli.AnsiReset)
    } else {
        _, _ = fmt.Fprintf(instance.writer, "WARNING: %s\n", escapedMessage)
    }
}

func (instance *commandOutput) printError(err error) {
    if nil == err {
        return
    }

    /* under json the side-report joins the document as a warning: the main failure travels through finish, and a raw text line would make the one document unparseable from that byte on */
    if true == instance.isJson() {
        instance.warnings = append(instance.warnings, err.Error())

        return
    }

    /* the message came off the wire, so its control characters are escaped before the terminal sees them; the json branch above hands it to the document, whose printer escapes the C1 block the encoder leaves raw and writes a byte that is not valid UTF-8 as U+FFFD, the encoder's documented answer */
    escapedMessage := escapeControlCharacters(err.Error(), false)

    if false == instance.option.NoColor {
        _, _ = fmt.Fprintf(instance.writer, "%s%sERROR: %s%s\n", cli.AnsiRed, cli.AnsiBold, escapedMessage, cli.AnsiReset)
    } else {
        _, _ = fmt.Fprintf(instance.writer, "ERROR: %s\n", escapedMessage)
    }
}

func (instance *commandOutput) printDatabaseBlock(identity *databaseIdentity) {
    if true == instance.isJson() {
        instance.database = identity

        return
    }

    currentDatabaseString := "<null>"
    if nil != identity.CurrentDatabase {
        currentDatabaseString = *identity.CurrentDatabase
    }

    /* every value here is the server's own answer, so control characters are escaped BEFORE the cell is truncated and padded — the alignment must count the escaped spelling, not the raw bytes it replaces */
    _, _ = fmt.Fprintln(instance.writer, "DATABASE")
    _, _ = fmt.Fprintln(instance.writer, "| key      | value              |")
    _, _ = fmt.Fprintln(instance.writer, "| -------- | ------------------ |")
    _, _ = fmt.Fprintf(instance.writer, "| database | %-18s |\n", truncateString(escapeControlCharacters(currentDatabaseString, false), 18))
    _, _ = fmt.Fprintf(instance.writer, "| host     | %-18s |\n", truncateString(escapeControlCharacters(identity.Hostname, false), 18))
    _, _ = fmt.Fprintf(instance.writer, "| port     | %-18s |\n", strconv.FormatUint(uint64(identity.Port), 10))
    _, _ = fmt.Fprintf(instance.writer, "| user     | %-18s |\n", truncateString(escapeControlCharacters(identity.CurrentUser, false), 18))
    _, _ = fmt.Fprintf(instance.writer, "| version  | %-18s |\n", truncateString(escapeControlCharacters(identity.Version, false), 18))
}

func (instance *commandOutput) printDetailsBlock(fields map[string]string) {
    if true == instance.isJson() {
        instance.details = fields

        return
    }

    _, _ = fmt.Fprintln(instance.writer, "DETAILS")
    _, _ = fmt.Fprintln(instance.writer, "| key     | value                                             |")
    _, _ = fmt.Fprintln(instance.writer, "| ------- | ------------------------------------------------- |")

    keys := []string{"manager", "group", "applied", "status", "name"}
    for _, key := range keys {
        if value, exists := fields[key]; exists {
            _, _ = fmt.Fprintf(instance.writer, "| %-7s | %-49s |\n", key, truncateString(escapeControlCharacters(value, false), 49))
        }
    }
}

/* printMigrationsBlock takes the document key apart from the display title: the two used to be one string, so the json document was keyed on the heading a person reads — data.migrations.APPLIED from db:status against data.migrations["APPLIED MIGRATIONS"] from db:migrate, for the same thing — with no enumerable set of keys and a rename for readability breaking every consumer silently. The key is the contract; the title is rendering. */
func (instance *commandOutput) printMigrationsBlock(key string, title string, names []string) {
    if 0 == len(names) {
        return
    }

    if true == instance.isJson() {
        if nil == instance.migrations {
            instance.migrations = make(map[string][]string)
        }
        instance.migrations[key] = names

        return
    }

    _, _ = fmt.Fprintln(instance.writer, title)
    _, _ = fmt.Fprintln(instance.writer, "| name                                              |")
    _, _ = fmt.Fprintln(instance.writer, "| ------------------------------------------------- |")
    for _, name := range names {
        _, _ = fmt.Fprintf(instance.writer, "| %-49s |\n", truncateString(escapeControlCharacters(name, false), 49))
    }
}

func (instance *commandOutput) printFilesBlock(files []string) {
    if 0 == len(files) {
        return
    }

    if true == instance.isJson() {
        instance.files = files

        return
    }

    _, _ = fmt.Fprintln(instance.writer, "FILES")
    for _, file := range files {
        _, _ = fmt.Fprintf(instance.writer, "  %s\n", escapeControlCharacters(file, false))
    }
}

func (instance *commandOutput) newline() {
    if true == instance.isJson() {
        return
    }

    _, _ = fmt.Fprintln(instance.writer)
}

/* truncateString bounds the cell to maxLen runes, not bytes: the format widths in the blocks above pad by rune count, so a byte-sliced multi-byte value would be truncated even when it fits the column, with the cut landing mid-rune. A budget with no room for the ellipsis answers the clipped runes alone rather than slicing negative. */
func truncateString(s string, maxLen int) string {
    if 0 >= maxLen {
        return ""
    }

    runes := []rune(s)
    if len(runes) <= maxLen {
        return s
    }

    if 3 >= maxLen {
        return string(runes[:maxLen])
    }

    return string(runes[:maxLen-3]) + "..."
}
