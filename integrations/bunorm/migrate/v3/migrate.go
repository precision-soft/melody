package migrate

import (
    "context"
    "fmt"
    "io"
    "os"
    "strings"
    "sync"
    "sync/atomic"

    "github.com/precision-soft/melody/v3/cli"
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

type runnerOptionContextKey struct{}

func withRunnerOption(ctx context.Context, option RunnerOption) context.Context {
    return context.WithValue(ctx, runnerOptionContextKey{}, option)
}

func runnerOptionFromContext(ctx context.Context) (RunnerOption, bool) {
    if nil == ctx {
        return RunnerOption{}, false
    }

    option, present := ctx.Value(runnerOptionContextKey{}).(RunnerOption)

    return option, present
}

var processRunnerOption atomic.Pointer[RunnerOption]
var processRunnerOptionMutex sync.Mutex
var previousRunnerOptionByInstalled map[*RunnerOption]*RunnerOption

/* SetDefaultRunnerOption replaces the process fallback, including any temporary command override. Context-carried options take precedence. */
func SetDefaultRunnerOption(option RunnerOption) {
    processRunnerOptionMutex.Lock()
    defer processRunnerOptionMutex.Unlock()

    previousRunnerOptionByInstalled = nil
    processRunnerOption.Store(&option)
}

func swapDefaultRunnerOption(option RunnerOption) (installed *RunnerOption, previous *RunnerOption) {
    processRunnerOptionMutex.Lock()
    defer processRunnerOptionMutex.Unlock()

    installed = &option
    previous = processRunnerOption.Swap(installed)
    if nil == previousRunnerOptionByInstalled {
        previousRunnerOptionByInstalled = make(map[*RunnerOption]*RunnerOption)
    }
    previousRunnerOptionByInstalled[installed] = previous

    return installed, previous
}

func restoreDefaultRunnerOption(installed *RunnerOption, _ *RunnerOption) {
    processRunnerOptionMutex.Lock()
    defer processRunnerOptionMutex.Unlock()

    previous, exists := previousRunnerOptionByInstalled[installed]
    if false == exists {
        return
    }
    delete(previousRunnerOptionByInstalled, installed)
    for successor, predecessor := range previousRunnerOptionByInstalled {
        if installed == predecessor {
            previousRunnerOptionByInstalled[successor] = previous
        }
    }
    processRunnerOption.CompareAndSwap(installed, previous)
}

func resolveDefaultRunnerOption() RunnerOption {
    if stored := processRunnerOption.Load(); nil != stored {
        return *stored
    }

    return DefaultRunnerOption()
}

func resolveRunnerOption(ctx context.Context) RunnerOption {
    if option, present := runnerOptionFromContext(ctx); true == present {
        return option
    }

    return resolveDefaultRunnerOption()
}

func RunQueries(ctx context.Context, db *bun.DB, direction string, migrationName string, queries []Query) error {
    return RunQueriesWithOption(ctx, db, direction, migrationName, queries, resolveRunnerOption(ctx))
}

func RunQueriesWithOption(ctx context.Context, db *bun.DB, direction string, migrationName string, queries []Query, option RunnerOption) error {
    writer := option.Writer
    if nil == writer {
        writer = os.Stdout
    }

    total := len(queries)
    printer := &migrationPrinter{writer: writer, noColor: option.NoColor}

    if 0 == total {
        printer.printEmpty(direction, migrationName)

        return nil
    }

    for index, query := range queries {
        step := index + 1
        prefix := fmt.Sprintf("[migration:%s] %s [%d/%d]", escapeControlCharacters(direction, false), escapeControlCharacters(migrationName, false), step, total)

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
    message := fmt.Sprintf("[migration:%s] %s: WARNING no queries to execute; the migration is marked applied without running anything", escapeControlCharacters(direction, false), escapeControlCharacters(migrationName, false))

    if instance.noColor {
        _, _ = fmt.Fprintf(instance.writer, "%s\n", message)
        return
    }

    _, _ = fmt.Fprintf(instance.writer, "%s%s%s%s\n", cli.AnsiYellow, cli.AnsiBold, message, cli.AnsiReset)
}

func (instance *migrationPrinter) printSuccess(direction string, migrationName string, total int) {
    message := fmt.Sprintf("[migration:%s] %s: all %d queries executed successfully", escapeControlCharacters(direction, false), escapeControlCharacters(migrationName, false), total)

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
