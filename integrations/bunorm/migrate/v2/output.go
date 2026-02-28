package migrate

import (
    "fmt"
    "io"
    "strconv"

    "github.com/precision-soft/melody/v2/cli"
    "github.com/precision-soft/melody/v2/cli/output"
)

type commandOutput struct {
    writer io.Writer
    option output.Option
}

func newCommandOutput(writer io.Writer, option output.Option) *commandOutput {
    return &commandOutput{
        writer: writer,
        option: option,
    }
}

func (instance *commandOutput) printSuccess(message string) {
    if false == instance.option.NoColor {
        _, _ = fmt.Fprintf(instance.writer, "%s%s%s\n", cli.AnsiGreen, message, cli.AnsiReset)
    } else {
        _, _ = fmt.Fprintln(instance.writer, message)
    }
}

func (instance *commandOutput) printWarning(message string) {
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

    if false == instance.option.NoColor {
        _, _ = fmt.Fprintf(instance.writer, "%s%sERROR: %s%s\n", cli.AnsiRed, cli.AnsiBold, err.Error(), cli.AnsiReset)
    } else {
        _, _ = fmt.Fprintf(instance.writer, "ERROR: %s\n", err.Error())
    }
}

func (instance *commandOutput) printDatabaseBlock(identity *databaseIdentity) {
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

    _, _ = fmt.Fprintln(instance.writer, "FILES")
    for _, file := range files {
        _, _ = fmt.Fprintf(instance.writer, "  %s\n", file)
    }
}

func (instance *commandOutput) newline() {
    _, _ = fmt.Fprintln(instance.writer)
}

func truncateString(s string, maxLen int) string {
    if len(s) <= maxLen {
        return s
    }

    return s[:maxLen-3] + "..."
}
