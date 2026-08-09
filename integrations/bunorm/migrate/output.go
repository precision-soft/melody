package migrate

import (
    "fmt"
    "io"
    "strconv"
    "time"

    "github.com/precision-soft/melody/cli"
    "github.com/precision-soft/melody/cli/output"
)

type commandOutput struct {
    writer io.Writer
    option output.Option

    /* the json accumulation: under --format=json every print records instead of writing, and finish renders the one machine-readable document the cli runner's silenced banner promises — the flag was accepted and validated long before this package honoured it */
    messages   []string
    warnings   []string
    database   *databaseIdentity
    details    map[string]string
    migrations map[string][]string
    files      []string
}

func newCommandOutput(writer io.Writer, option output.Option) *commandOutput {
    return &commandOutput{
        writer: writer,
        option: option,
    }
}

/* runnerOptionForCommand derives the per-query printer posture from the command's parsed flags: the command's writer and colour choice in text mode, a discarded writer under json — the document is the only byte the command may emit there. */
func runnerOptionForCommand(writer io.Writer, option output.Option) RunnerOption {
    if output.FormatJson == option.Format {
        return RunnerOption{Writer: io.Discard, NoColor: true}
    }

    return RunnerOption{Writer: writer, NoColor: option.NoColor}
}

func (instance *commandOutput) isJson() bool {
    return output.FormatJson == instance.option.Format
}

/* finish is the command's one exit door: under --format=json it renders the accumulated document — the failure included — and in every mode it answers the error the command should return. The command's own failure stays the verdict; a rendering failure becomes one only when the command itself succeeded. */
func (instance *commandOutput) finish(command string, startedAt time.Time, runErr error) error {
    if false == instance.isJson() {
        return runErr
    }

    meta := output.NewMeta(command, nil, instance.option, startedAt, time.Since(startedAt), output.Version{})
    envelope := output.NewEnvelope(meta)

    data := map[string]any{}
    if 0 < len(instance.messages) {
        data["messages"] = instance.messages
    }
    if nil != instance.database {
        currentDatabaseString := "<null>"
        if nil != instance.database.CurrentDatabase {
            currentDatabaseString = *instance.database.CurrentDatabase
        }

        data["database"] = map[string]any{
            "database": currentDatabaseString,
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
        envelope.Error = output.NewError("migrate.failed", runErr.Error(), nil, nil)
    }

    renderErr := output.Render(instance.writer, envelope, instance.option)
    if nil != runErr {
        return runErr
    }

    return renderErr
}

func (instance *commandOutput) printSuccess(message string) {
    if true == instance.isJson() {
        instance.messages = append(instance.messages, message)

        return
    }

    if false == instance.option.NoColor {
        _, _ = fmt.Fprintf(instance.writer, "%s%s%s\n", cli.AnsiGreen, message, cli.AnsiReset)
    } else {
        _, _ = fmt.Fprintln(instance.writer, message)
    }
}

func (instance *commandOutput) printWarning(message string) {
    if true == instance.isJson() {
        instance.warnings = append(instance.warnings, message)

        return
    }

    if false == instance.option.NoColor {
        _, _ = fmt.Fprintf(instance.writer, "%s%sWARNING: %s%s\n", cli.AnsiYellow, cli.AnsiBold, message, cli.AnsiReset)
    } else {
        _, _ = fmt.Fprintf(instance.writer, "WARNING: %s\n", message)
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

    if false == instance.option.NoColor {
        _, _ = fmt.Fprintf(instance.writer, "%s%sERROR: %s%s\n", cli.AnsiRed, cli.AnsiBold, err.Error(), cli.AnsiReset)
    } else {
        _, _ = fmt.Fprintf(instance.writer, "ERROR: %s\n", err.Error())
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
    _, _ = fmt.Fprintf(instance.writer, "| database | %-18s |\n", truncateString(currentDatabaseString, 18))
    _, _ = fmt.Fprintf(instance.writer, "| host     | %-18s |\n", truncateString(identity.Hostname, 18))
    _, _ = fmt.Fprintf(instance.writer, "| port     | %-18s |\n", strconv.FormatUint(uint64(identity.Port), 10))
    _, _ = fmt.Fprintf(instance.writer, "| user     | %-18s |\n", truncateString(identity.CurrentUser, 18))
    _, _ = fmt.Fprintf(instance.writer, "| version  | %-18s |\n", truncateString(identity.Version, 18))
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
            _, _ = fmt.Fprintf(instance.writer, "| %-7s | %-49s |\n", key, truncateString(value, 49))
        }
    }
}

func (instance *commandOutput) printMigrationsBlock(title string, names []string) {
    if 0 == len(names) {
        return
    }

    if true == instance.isJson() {
        if nil == instance.migrations {
            instance.migrations = make(map[string][]string)
        }
        instance.migrations[title] = names

        return
    }

    _, _ = fmt.Fprintln(instance.writer, title)
    _, _ = fmt.Fprintln(instance.writer, "| name                                              |")
    _, _ = fmt.Fprintln(instance.writer, "| ------------------------------------------------- |")
    for _, name := range names {
        _, _ = fmt.Fprintf(instance.writer, "| %-49s |\n", truncateString(name, 49))
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
        _, _ = fmt.Fprintf(instance.writer, "  %s\n", file)
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
