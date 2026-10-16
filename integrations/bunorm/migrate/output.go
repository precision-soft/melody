package migrate

import (
    "errors"
    "fmt"
    "io"
    "reflect"
    "strconv"
    "sync"
    "time"

    "github.com/precision-soft/melody/cli"
    "github.com/precision-soft/melody/cli/output"
    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    loggingcontract "github.com/precision-soft/melody/logging/contract"
)

type commandOutput struct {
    writer    *errorTrackingWriter
    arguments []string
    option    output.Option

    /* under --format=json every print records instead of writing, and finish renders the one machine-readable document */
    messages   []string
    warnings   []string
    database   *databaseIdentity
    details    map[string]string
    migrations map[string][]string
    files      []string

    /* lostReportJournal is set by a command whose result is not its report, db:create, whose result is the file it wrote: finish records a report the writer lost there instead of failing the run, since a re-run would create a second migration. Every other command keeps the refusal. */
    lostReportJournal loggingcontract.Logger
}

/* reportLostWritesTo tells finish that the command's result is elsewhere than its report, and where a report the writer lost is recorded instead of failing the run. */
func (instance *commandOutput) reportLostWritesTo(journal loggingcontract.Logger) {
    instance.lostReportJournal = journal
}

/* lostReport answers what a report the writer lost is worth: the run's failure for a command whose report is its result, and a warning in the journal — with nil as the run's answer — for one that told finish its result is elsewhere. */
func (instance *commandOutput) lostReport(command string, lostWrite error) error {
    lost := exception.NewError("the report could not be written in full", map[string]any{"command": command}, lostWrite)

    if nil == instance.lostReportJournal {
        return lost
    }

    instance.lostReportJournal.Warning("the report could not be written in full; the command's result is in place", exception.LogContext(lost))

    return nil
}

/* newCommandOutput takes the command's positional arguments beside its writer and flags, since the machine document carries them. */
func newCommandOutput(writer io.Writer, arguments []string, option output.Option) *commandOutput {
    return &commandOutput{
        writer:    &errorTrackingWriter{writer: writer},
        arguments: append([]string{}, arguments...),
        option:    option,
    }
}

/* errorTrackingWriter remembers the first write failure and swallows the rest, so finish can refuse a report cut short by a full disk or a failing writer instead of exiting zero. It is shared by the per-query printer and, during the run, the process-wide fallback, so a lock keeps the remembered failure one value. */
type errorTrackingWriter struct {
    writer   io.Writer
    mutex    sync.Mutex
    firstErr error
}

func (instance *errorTrackingWriter) Write(payload []byte) (int, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if nil != instance.firstErr {
        return len(payload), nil
    }

    _, writeErr := instance.writer.Write(payload)
    if nil != writeErr {
        instance.firstErr = writeErr
    }

    return len(payload), nil
}

/* lostWrite answers the first write failure the writer remembered, under the same lock the writes take, so finish reads a settled value rather than one a late query line is still writing */
func (instance *errorTrackingWriter) lostWrite() error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return instance.firstErr
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

/* wantsDetail decides whether the detail blocks are collected, apart from --verbose: verbosity shapes the text, while the json document is the machine contract and always carries them. The cost is one query per json run, the database identity block. */
func (instance *commandOutput) wantsDetail() bool {
    if true == instance.isJson() {
        return true
    }

    return instance.option.Verbose
}

/* finishRun renders the command's document from the outcome the run had, a panic included, and is the door every command of this family defers to, so a run that died is never rendered as a success. recovered is passed in because recover answers only when called directly by the deferred function; the panic is re-raised unchanged once the document says what happened. */
func (instance *commandOutput) finishRun(commandName string, startedAt time.Time, runErr error, recovered any) error {
    if nil == recovered {
        return instance.finish(commandName, startedAt, runErr)
    }

    /* a run that had already failed keeps its own failure as the verdict: the panic came after it, and the reason the command failed is the one the operator needs */
    if nil == runErr {
        runErr = exception.NewError(
            commandName+" panicked",
            /* the recovered value travels beside its type, since a panic that is not an error has no cause to carry */
            map[string]any{
                "command":        commandName,
                "recoveredType":  fmt.Sprintf("%T", recovered),
                "recoveredValue": fmt.Sprintf("%v", recovered),
            },
            exception.PanicCause(recovered),
        )
    }

    _ = instance.finish(commandName, startedAt, runErr)

    panic(recovered)
}

/* finish is the command's one exit door: under --format=json it renders the document, the failure included, and in every mode it answers the command's error. The command's own failure stays the verdict; a lost report becomes one only when the command succeeded and its report is its result, and a command that called reportLostWritesTo has the loss journaled and its run answered nil. */
func (instance *commandOutput) finish(command string, startedAt time.Time, runErr error) error {
    if false == instance.isJson() {
        if nil != runErr {
            return runErr
        }

        if lostWrite := instance.writer.lostWrite(); nil != lostWrite {
            return instance.lostReport(command, lostWrite)
        }

        return nil
    }

    meta := output.NewMeta(command, instance.arguments, instance.option, startedAt, time.Since(startedAt), output.Version{})
    envelope := output.NewEnvelope(meta)

    data := map[string]any{}
    if 0 < len(instance.messages) {
        data["messages"] = instance.messages
    }
    if nil != instance.database {
        /* the absent database is json null, so a consumer never confuses it with a database named literally <null> */
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

    /* the document renders through the tracking writer too, which swallows the write it lost; the renderer's own answer is read first and the remembered write failure second, so a json report cut short is refused the way the text report is */
    renderErr := output.Render(instance.writer, envelope, instance.option)
    if lostWrite := instance.writer.lostWrite(); nil == renderErr && nil != lostWrite {
        renderErr = instance.lostReport(command, lostWrite)
    }

    if nil != runErr {
        return runErr
    }

    return renderErr
}

/* errorDetailsOf and errorCauseOf fill the envelope's details and cause from what the error carries, its context and its cause chain. The details object is empty rather than null when the error carries no context, so the field keeps its json type. */
func errorDetailsOf(runErr error) map[string]any {
    details := map[string]any{}

    var provider exceptioncontract.ContextProvider
    /* the As target is read through the typed-nil door: bun's migrator wraps a migration error with %w after a plain nil test, so a typed-nil link can satisfy As and would panic in Context(), unwinding finish before the document is written */
    if true == errors.As(runErr, &provider) && false == isNilInterface(provider) {
        for key, value := range provider.Context() {
            details[key] = value
        }
    }

    return details
}

/* errorCauseOf answers the first link under the failure with the whole chain beneath it, and nothing when the failure has no cause. */
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

/* every text door of the output escapes what it did not write itself, the warning included, while the json branch hands the value to the document, whose printer escapes the C1 block */
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

/* printMigrationsBlock keys the json document apart from the display title: the key is the contract and the title is rendering, so a heading renamed for readability breaks no consumer. */
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
