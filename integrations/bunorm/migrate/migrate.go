package migrate

import (
    "context"
    "fmt"
    "io"
    "os"
    "strings"
    "sync/atomic"

    "github.com/precision-soft/melody/cli"
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

/* processRunnerOption is the process default RunQueries reads when a migration does not pass its own option: a generated migration's signature is fixed by bun as (ctx, db) and cannot receive the parsed flags any other way, so without this door a --no-color run carried escape codes from exactly the per-query lines the flag was passed to clean. */
var processRunnerOption atomic.Pointer[RunnerOption]

/* SetDefaultRunnerOption installs the process default RunQueries uses when the migration does not pass its own option; the migrate commands call it from their parsed flags before running the migrator. */
func SetDefaultRunnerOption(option RunnerOption) {
    processRunnerOption.Store(&option)
}

func resolveDefaultRunnerOption() RunnerOption {
    if stored := processRunnerOption.Load(); nil != stored {
        return *stored
    }

    return DefaultRunnerOption()
}

func RunQueries(ctx context.Context, db *bun.DB, direction string, migrationName string, queries []Query) error {
    return RunQueriesWithOption(ctx, db, direction, migrationName, queries, resolveDefaultRunnerOption())
}

func RunQueriesWithOption(ctx context.Context, db *bun.DB, direction string, migrationName string, queries []Query, option RunnerOption) error {
    writer := option.Writer
    if nil == writer {
        writer = os.Stdout
    }

    total := len(queries)
    printer := &migrationPrinter{writer: writer, noColor: option.NoColor}

    /* an empty set is almost always a builder that produced nothing rather than a migration with nothing to do, and the migrator marks the migration applied on success — burying it, since an applied migration never runs again. The run still succeeds, so the caller decides; what it must not do is read like the queries ran. */
    if 0 == total {
        printer.printEmpty(direction, migrationName)

        return nil
    }

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
    escapedQueryName := escapeControlCharacters(queryName, false)

    if instance.noColor {
        _, _ = fmt.Fprintf(instance.writer, "%s executing: %s\n", prefix, escapedQueryName)
        return
    }

    _, _ = fmt.Fprintf(instance.writer, "%s%s%s executing: %s\n", cli.AnsiCyan, prefix, cli.AnsiReset, escapedQueryName)
}

func (instance *migrationPrinter) printCompleted(prefix string, queryName string) {
    escapedQueryName := escapeControlCharacters(queryName, false)

    if instance.noColor {
        _, _ = fmt.Fprintf(instance.writer, "%s completed: %s\n", prefix, escapedQueryName)
        return
    }

    _, _ = fmt.Fprintf(instance.writer, "%s%s%s completed: %s%s%s\n", cli.AnsiCyan, prefix, cli.AnsiReset, cli.AnsiGreen, escapedQueryName, cli.AnsiReset)
}

/* printFailed escapes what it did not write itself before the terminal sees it: the error text came off the wire, and the statement — kept multi-line on purpose, its line breaks are the readability — may carry any byte the migration author or the driver put there. */
func (instance *migrationPrinter) printFailed(prefix string, queryName string, err error, sql string) {
    escapedQueryName := escapeControlCharacters(queryName, false)
    escapedErrorMessage := escapeControlCharacters(err.Error(), false)
    escapedSql := escapeControlCharacters(sql, true)

    if instance.noColor {
        _, _ = fmt.Fprintf(instance.writer, "%s FAILED: %s\n", prefix, escapedQueryName)
        _, _ = fmt.Fprintf(instance.writer, "%s ERROR: %s\n", prefix, escapedErrorMessage)
        _, _ = fmt.Fprintf(instance.writer, "%s QUERY:\n%s\n", prefix, formatQueryForLog(escapedSql))
        return
    }

    _, _ = fmt.Fprintf(instance.writer, "%s%s%s %sFAILED:%s %s\n",
        cli.AnsiCyan, prefix, cli.AnsiReset,
        cli.AnsiRed, cli.AnsiReset,
        escapedQueryName,
    )
    _, _ = fmt.Fprintf(instance.writer, "%s%s ERROR: %s%s\n",
        cli.AnsiRed, cli.AnsiBold, escapedErrorMessage, cli.AnsiReset,
    )
    _, _ = fmt.Fprintf(instance.writer, "%s QUERY:%s\n%s%s%s\n",
        cli.AnsiYellow, cli.AnsiReset,
        cli.AnsiYellow, formatQueryForLog(escapedSql), cli.AnsiReset,
    )
}

func (instance *migrationPrinter) printEmpty(direction string, migrationName string) {
    message := fmt.Sprintf("[migration:%s] %s: WARNING no queries to execute; the migration is marked applied without running anything", direction, migrationName)

    if instance.noColor {
        _, _ = fmt.Fprintf(instance.writer, "%s\n", message)
        return
    }

    _, _ = fmt.Fprintf(instance.writer, "%s%s%s%s\n", cli.AnsiYellow, cli.AnsiBold, message, cli.AnsiReset)
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
