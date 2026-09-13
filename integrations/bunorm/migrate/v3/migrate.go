package migrate

import (
    "context"
    "fmt"
    "io"
    "os"
    "strings"
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

/* runnerOptionContextKey is the key under which a migrate command hands its parsed posture to the migrations it runs. A generated migration's signature is fixed by bun as (ctx, db) and cannot receive the parsed flags any other way, and bun passes the command's context into every migration unchanged, so the context is the one channel that reaches the migration and belongs to that run alone. */
type runnerOptionContextKey struct{}

/* withRunnerOption returns a context carrying the option RunQueries reads first; the migrate commands derive it from their parsed flags and run the migrator under it. */
func withRunnerOption(ctx context.Context, option RunnerOption) context.Context {
    return context.WithValue(ctx, runnerOptionContextKey{}, option)
}

/* runnerOptionFromContext answers the option a migrate command put on the context, and false when the context carries none — a migration invoked outside any command, or one that dropped the context it was handed. */
func runnerOptionFromContext(ctx context.Context) (RunnerOption, bool) {
    if nil == ctx {
        return RunnerOption{}, false
    }

    option, present := ctx.Value(runnerOptionContextKey{}).(RunnerOption)

    return option, present
}

/* processRunnerOption is the process-wide fallback RunQueries reads when the context carries no option. It exists for the migration that drops the context it was handed and for a host process that runs migrations outside melody's commands; it is not how a command reaches its own migrations — that is the context — because a process default is one value for the whole process, so two commands dispatched concurrently overwrote each other's writer and a --format=json run sent a text run's per-query lines into its own discarded writer. */
var processRunnerOption atomic.Pointer[RunnerOption]

/* SetDefaultRunnerOption installs the process-wide fallback RunQueries uses when the context carries no option. It is the door of a host process that runs migrations on its own; the migrate commands do not leave anything behind in it — each installs its posture for the length of its run and puts back what was there. */
func SetDefaultRunnerOption(option RunnerOption) {
    processRunnerOption.Store(&option)
}

/* swapDefaultRunnerOption installs the fallback for the length of a command and answers the pointer that was installed before, for restoreDefaultRunnerOption. The fallback is what a migration that drops its context sees, and under --format=json that migration would otherwise print its per-query lines into the document; the pointer is what makes the restore exact — only the command that installed a value puts it back. */
func swapDefaultRunnerOption(option RunnerOption) (installed *RunnerOption, previous *RunnerOption) {
    installed = &option

    return installed, processRunnerOption.Swap(installed)
}

/* restoreDefaultRunnerOption puts the previous fallback back, and only when the one this command installed is still the live one: a command that finishes while another dispatched after it is still running leaves that command's value where it is. Two commands with migrations that drop their context share the one fallback for as long as they overlap — the context is the channel that keeps them apart, and a migration that drops it has opted out of that. */
func restoreDefaultRunnerOption(installed *RunnerOption, previous *RunnerOption) {
    processRunnerOption.CompareAndSwap(installed, previous)
}

func resolveDefaultRunnerOption() RunnerOption {
    if stored := processRunnerOption.Load(); nil != stored {
        return *stored
    }

    return DefaultRunnerOption()
}

/* resolveRunnerOption answers the posture a run prints under, in the order the doors are trusted: the option the command put on the context, then the process-wide fallback, then the package default. */
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
    message := fmt.Sprintf("[migration:%s] %s: WARNING no queries to execute; the migration is marked applied without running anything", direction, escapeControlCharacters(migrationName, false))

    if instance.noColor {
        _, _ = fmt.Fprintf(instance.writer, "%s\n", message)
        return
    }

    _, _ = fmt.Fprintf(instance.writer, "%s%s%s%s\n", cli.AnsiYellow, cli.AnsiBold, message, cli.AnsiReset)
}

func (instance *migrationPrinter) printSuccess(direction string, migrationName string, total int) {
    message := fmt.Sprintf("[migration:%s] %s: all %d queries executed successfully", direction, escapeControlCharacters(migrationName, false), total)

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
