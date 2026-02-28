package migrate

import (
    "context"
    "fmt"
    "io"
    "os"
    "strings"

    "github.com/precision-soft/melody/v2/cli"
    "github.com/uptrace/bun"
)

type Query struct {
    Name string
    SQL  string
}

type RunnerOption struct {
    Writer  io.Writer
    NoColor bool
}

func DefaultRunnerOption() RunnerOption {
    return RunnerOption{
        Writer:  os.Stdout,
        NoColor: false,
    }
}

func RunQueries(ctx context.Context, db *bun.DB, direction string, migrationName string, queries []Query) error {
    return RunQueriesWithOption(ctx, db, direction, migrationName, queries, DefaultRunnerOption())
}

func RunQueriesWithOption(ctx context.Context, db *bun.DB, direction string, migrationName string, queries []Query, option RunnerOption) error {
    writer := option.Writer
    if nil == writer {
        writer = os.Stdout
    }

    total := len(queries)
    printer := &migrationPrinter{writer: writer, noColor: option.NoColor}

    for index, query := range queries {
        step := index + 1
        prefix := fmt.Sprintf("[migration:%s] %s [%d/%d]", direction, migrationName, step, total)

        printer.printExecuting(prefix, query.Name)

        if _, err := db.ExecContext(ctx, query.SQL); nil != err {
            printer.printFailed(prefix, query.Name, err, query.SQL)

            return fmt.Errorf("migration %s failed at step %d/%d (%s): %w", migrationName, step, total, query.Name, err)
        }

        printer.printCompleted(prefix, query.Name)
    }

    printer.printSuccess(direction, migrationName, total)

    return nil
}

func Up(ctx context.Context, db *bun.DB, migrationName string, queries []Query) error {
    return RunQueries(ctx, db, "up", migrationName, queries)
}

func UpWithOption(ctx context.Context, db *bun.DB, migrationName string, queries []Query, option RunnerOption) error {
    return RunQueriesWithOption(ctx, db, "up", migrationName, queries, option)
}

func Down(ctx context.Context, db *bun.DB, migrationName string, queries []Query) error {
    return RunQueries(ctx, db, "down", migrationName, queries)
}

func DownWithOption(ctx context.Context, db *bun.DB, migrationName string, queries []Query, option RunnerOption) error {
    return RunQueriesWithOption(ctx, db, "down", migrationName, queries, option)
}

type migrationPrinter struct {
    writer  io.Writer
    noColor bool
}

func (instance *migrationPrinter) printExecuting(prefix string, queryName string) {
    if instance.noColor {
        _, _ = fmt.Fprintf(instance.writer, "%s executing: %s\n", prefix, queryName)
        return
    }

    _, _ = fmt.Fprintf(instance.writer, "%s%s%s executing: %s\n", cli.AnsiCyan, prefix, cli.AnsiReset, queryName)
}

func (instance *migrationPrinter) printCompleted(prefix string, queryName string) {
    if instance.noColor {
        _, _ = fmt.Fprintf(instance.writer, "%s completed: %s\n", prefix, queryName)
        return
    }

    _, _ = fmt.Fprintf(instance.writer, "%s%s%s completed: %s%s%s\n", cli.AnsiCyan, prefix, cli.AnsiReset, cli.AnsiGreen, queryName, cli.AnsiReset)
}

func (instance *migrationPrinter) printFailed(prefix string, queryName string, err error, sql string) {
    if instance.noColor {
        _, _ = fmt.Fprintf(instance.writer, "%s FAILED: %s\n", prefix, queryName)
        _, _ = fmt.Fprintf(instance.writer, "%s ERROR: %s\n", prefix, err.Error())
        _, _ = fmt.Fprintf(instance.writer, "%s QUERY:\n%s\n", prefix, formatQueryForLog(sql))
        return
    }

    _, _ = fmt.Fprintf(instance.writer, "%s%s%s %sFAILED:%s %s\n",
        cli.AnsiCyan, prefix, cli.AnsiReset,
        cli.AnsiRed, cli.AnsiReset,
        queryName,
    )
    _, _ = fmt.Fprintf(instance.writer, "%s%s ERROR: %s%s\n",
        cli.AnsiRed, cli.AnsiBold, err.Error(), cli.AnsiReset,
    )
    _, _ = fmt.Fprintf(instance.writer, "%s QUERY:%s\n%s%s%s\n",
        cli.AnsiYellow, cli.AnsiReset,
        cli.AnsiYellow, formatQueryForLog(sql), cli.AnsiReset,
    )
}

func (instance *migrationPrinter) printSuccess(direction string, migrationName string, total int) {
    message := fmt.Sprintf("[migration:%s] %s: all %d queries executed successfully", direction, migrationName, total)

    if instance.noColor {
        _, _ = fmt.Fprintf(instance.writer, "%s\n", message)
        return
    }

    _, _ = fmt.Fprintf(instance.writer, "%s%s%s%s\n", cli.AnsiGreen, cli.AnsiBold, message, cli.AnsiReset)
}

func formatQueryForLog(sql string) string {
    lines := strings.Split(strings.TrimSpace(sql), "\n")
    result := make([]string, 0, len(lines))

    for _, line := range lines {
        result = append(result, "       "+strings.TrimSpace(line))
    }

    return strings.Join(result, "\n")
}
