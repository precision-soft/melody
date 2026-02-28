package migrate

import (
    "fmt"
    "io"
    "strconv"

    "github.com/precision-soft/melody/cli"
    "github.com/precision-soft/melody/cli/output"
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

func (o *commandOutput) printSuccess(message string) {
    if false == o.option.NoColor {
        _, _ = fmt.Fprintf(o.writer, "%s%s%s\n", cli.AnsiGreen, message, cli.AnsiReset)
    } else {
        _, _ = fmt.Fprintln(o.writer, message)
    }
}

func (o *commandOutput) printWarning(message string) {
    if false == o.option.NoColor {
        _, _ = fmt.Fprintf(o.writer, "%s%sWARNING: %s%s\n", cli.AnsiYellow, cli.AnsiBold, message, cli.AnsiReset)
    } else {
        _, _ = fmt.Fprintf(o.writer, "WARNING: %s\n", message)
    }
}

func (o *commandOutput) printError(err error) {
    if nil == err {
        return
    }

    if false == o.option.NoColor {
        _, _ = fmt.Fprintf(o.writer, "%s%sERROR: %s%s\n", cli.AnsiRed, cli.AnsiBold, err.Error(), cli.AnsiReset)
    } else {
        _, _ = fmt.Fprintf(o.writer, "ERROR: %s\n", err.Error())
    }
}

func (o *commandOutput) printDatabaseBlock(identity *databaseIdentity) {
    currentDatabaseString := "<null>"
    if nil != identity.CurrentDatabase {
        currentDatabaseString = *identity.CurrentDatabase
    }

    _, _ = fmt.Fprintln(o.writer, "DATABASE")
    _, _ = fmt.Fprintln(o.writer, "| key      | value              |")
    _, _ = fmt.Fprintln(o.writer, "| -------- | ------------------ |")
    _, _ = fmt.Fprintf(o.writer, "| database | %-18s |\n", truncateString(currentDatabaseString, 18))
    _, _ = fmt.Fprintf(o.writer, "| host     | %-18s |\n", truncateString(identity.Hostname, 18))
    _, _ = fmt.Fprintf(o.writer, "| port     | %-18s |\n", strconv.FormatUint(uint64(identity.Port), 10))
    _, _ = fmt.Fprintf(o.writer, "| user     | %-18s |\n", truncateString(identity.CurrentUser, 18))
    _, _ = fmt.Fprintf(o.writer, "| version  | %-18s |\n", truncateString(identity.Version, 18))
}

func (o *commandOutput) printDetailsBlock(fields map[string]string) {
    _, _ = fmt.Fprintln(o.writer, "DETAILS")
    _, _ = fmt.Fprintln(o.writer, "| key     | value                                             |")
    _, _ = fmt.Fprintln(o.writer, "| ------- | ------------------------------------------------- |")

    keys := []string{"manager", "group", "applied", "status", "name"}
    for _, key := range keys {
        if value, exists := fields[key]; exists {
            _, _ = fmt.Fprintf(o.writer, "| %-7s | %-49s |\n", key, truncateString(value, 49))
        }
    }
}

func (o *commandOutput) printMigrationsBlock(title string, names []string) {
    if 0 == len(names) {
        return
    }

    _, _ = fmt.Fprintln(o.writer, title)
    _, _ = fmt.Fprintln(o.writer, "| name                                              |")
    _, _ = fmt.Fprintln(o.writer, "| ------------------------------------------------- |")
    for _, name := range names {
        _, _ = fmt.Fprintf(o.writer, "| %-49s |\n", truncateString(name, 49))
    }
}

func (o *commandOutput) printFilesBlock(files []string) {
    if 0 == len(files) {
        return
    }

    _, _ = fmt.Fprintln(o.writer, "FILES")
    for _, file := range files {
        _, _ = fmt.Fprintf(o.writer, "  %s\n", file)
    }
}

func (o *commandOutput) newline() {
    _, _ = fmt.Fprintln(o.writer)
}

func truncateString(s string, maxLen int) string {
    if len(s) <= maxLen {
        return s
    }

    return s[:maxLen-3] + "..."
}
