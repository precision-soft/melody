package migrate

import (
    "errors"
    "fmt"
    "io"
    "reflect"
    "strconv"
    "sync"
    "time"

    "github.com/precision-soft/melody/v3/cli"
    "github.com/precision-soft/melody/v3/cli/output"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
)

type commandOutputWriter struct {
    mutex sync.Mutex
    writer io.Writer
    err error
}

func (instance *commandOutputWriter) Write(payload []byte) (int, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()
    if nil != instance.err {
        return 0, instance.err
    }
    written, err := instance.writer.Write(payload)
    if nil == err && written != len(payload) {
        err = io.ErrShortWrite
    }
    instance.err = err
    return written, err
}

func (instance *commandOutputWriter) failure() error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()
    return instance.err
}

type commandOutput struct {
    writer    *commandOutputWriter
    arguments []string
    option    output.Option

    messages   []string
    warnings   []string
    unlockFailure error
    database   *databaseIdentity
    details    map[string]string
    migrations map[string][]string
    files      []string
}

func newCommandOutput(writer io.Writer, arguments []string, option output.Option) *commandOutput {
    return &commandOutput{
        writer:    &commandOutputWriter{writer: writer},
        arguments: append([]string{}, arguments...),
        option:    option,
    }
}

func runnerOptionForCommand(writer io.Writer, option output.Option) RunnerOption {
    if true == output.IsJsonFormat(option.Format) {
        return RunnerOption{Writer: io.Discard, NoColor: true}
    }

    return RunnerOption{Writer: writer, NoColor: option.NoColor}
}

func (instance *commandOutput) isJson() bool {
    return output.IsJsonFormat(instance.option.Format)
}

func (instance *commandOutput) wantsDetail() bool {
    if true == instance.isJson() {
        return true
    }

    return instance.option.Verbose
}

func (instance *commandOutput) finishRun(commandName string, startedAt time.Time, runErr error, recovered any) error {
    if nil == recovered {
        return instance.finish(commandName, startedAt, runErr)
    }

    if nil == runErr {
        runErr = exception.NewError(
            commandName+" panicked",

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

func (instance *commandOutput) finish(command string, startedAt time.Time, runErr error) error {
    if false == instance.isJson() {
        if writeErr := instance.writer.failure(); nil != writeErr {
            return errors.Join(runErr, writeErr)
        }
        return runErr
    }

    meta := output.NewMeta(command, instance.arguments, instance.option, startedAt, time.Since(startedAt), output.Version{})
    envelope := output.NewEnvelope(meta)

    data := map[string]any{}
    if 0 < len(instance.messages) {
        data["messages"] = instance.messages
    }
    if nil != instance.database {

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

    if nil != instance.unlockFailure {
        envelope.Warnings = append(envelope.Warnings, output.NewWarning(
            "migrate.unlock_failed", instance.unlockFailure.Error(),
            map[string]any{"action": "unlock", "requiresNoActiveMigration": true},
        ))
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

func errorDetailsOf(runErr error) map[string]any {
    details := map[string]any{}

    var provider exceptioncontract.ContextProvider

    if true == errors.As(runErr, &provider) && false == isNilInterface(provider) {
        for key, value := range provider.Context() {
            details[key] = value
        }
    }

    return details
}

func errorCauseOf(runErr error) *output.ErrorCause {
    causeChain := exception.BuildCauseChain(errors.Unwrap(runErr), 8)
    if 0 == len(causeChain) {
        return nil
    }

    return output.NewErrorCause(causeChain[0], map[string]any{"chain": causeChain})
}

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

func pluralizeMigrations(count int) string {
    if 1 == count {
        return "1 migration"
    }

    return strconv.Itoa(count) + " migrations"
}

func (instance *commandOutput) printTextSuccess(message string) {
    if true == instance.isJson() {
        return
    }

    instance.printSuccess(message)
}

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

    if true == instance.isJson() {
        instance.warnings = append(instance.warnings, err.Error())

        return
    }

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
