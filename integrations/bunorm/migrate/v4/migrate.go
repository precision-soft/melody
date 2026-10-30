package migrate

import (
    "context"
    "fmt"
    "io"
    "os"
    "strings"

    "github.com/precision-soft/melody/v4/cli"
    "github.com/uptrace/bun"
)

type Query struct {
    Name string
    SQL  string
}

/* RunnerOption shapes the per-query lines a migration prints: Writer, the run's own writer when left out — the posture the migrate command put on the context, else standard output; NoColor, true to print without colour, false to follow the run's posture. The zero value prints as the command prints. */
type RunnerOption struct {
    Writer  io.Writer
    NoColor bool
}

/* runnerOptionContextKey is the key under which a migrate command hands its parsed posture to the migrations it runs. A generated migration's signature is fixed by bun as (ctx, db) and cannot receive the parsed flags any other way, and bun passes the command's context into every migration unchanged, so the context is the one channel that reaches the migration and belongs to that run alone. */
type runnerOptionContextKey struct{}

/* emptyMigrationWarnerContextKey is the key under which a migrate command hands RunQueries the door an empty migration's warning goes through when the command's writer is discarded, under --format=json, so the machine document still carries it. It rides the context beside the option rather than inside it, since a function field would make RunnerOption incomparable. */
type emptyMigrationWarnerContextKey struct{}

func withEmptyMigrationWarner(ctx context.Context, warn func(message string)) context.Context {
    return context.WithValue(ctx, emptyMigrationWarnerContextKey{}, warn)
}

func emptyMigrationWarnerFromContext(ctx context.Context) func(message string) {
    if nil == ctx {
        return nil
    }

    warn, _ := ctx.Value(emptyMigrationWarnerContextKey{}).(func(message string))

    return warn
}

/* withRunnerOption returns a context carrying the option RunQueries reads first; the migrate commands derive it from their parsed flags and run the migrator under it. */
func withRunnerOption(ctx context.Context, option RunnerOption) context.Context {
    return context.WithValue(ctx, runnerOptionContextKey{}, option)
}

/* runnerOptionFromContext answers the option a migrate command put on the context, and false when the context carries none — a migration invoked outside any command, or one that dropped the context it receives. */
func runnerOptionFromContext(ctx context.Context) (RunnerOption, bool) {
    if nil == ctx {
        return RunnerOption{}, false
    }

    option, present := ctx.Value(runnerOptionContextKey{}).(RunnerOption)

    return option, present
}

/* RunQueries executes the queries in order, printing each under the direction and the migration's name, and stops at the first failure naming the step. The posture is the option's where set, the command's otherwise: a migration passes on the context it is given, which carries the command's writer and colour, and one that drops it prints to standard output. */
func RunQueries(ctx context.Context, db *bun.DB, direction string, migrationName string, queries []Query, option RunnerOption) error {
    commandOption, _ := runnerOptionFromContext(ctx)

    /* an option that names no writer takes the one the command prints under, so a migration that passes its own option to set the colour alone does not print its per-query lines past the writer its command chose, into the middle of a --format=json document */
    writer := option.Writer
    if nil == writer {
        writer = commandOption.Writer
    }
    if nil == writer {
        writer = os.Stdout
    }
    total := len(queries)
    printer := &migrationPrinter{writer: writer, noColor: option.NoColor || commandOption.NoColor, warn: emptyMigrationWarnerFromContext(ctx)}

    /* an empty set is almost always a builder that produced nothing rather than a migration with nothing to do, and the migrator marks the migration applied on success — burying it, since an applied migration never runs again. The run still succeeds, so the caller decides; what it must not do is read like the queries ran. */
    if 0 == total {
        printer.printEmpty(direction, migrationName)

        return nil
    }

    /* the prefix carries the migration name on every per-query line, and the name is the author's — it is escaped here once, the way the empty and success lines escape it, so the three lines that print the prefix do not repaint the terminal through a name whose query names were escaped beside it */
    escapedMigrationName := escapeControlCharacters(migrationName, false)

    for index, query := range queries {
        step := index + 1
        prefix := fmt.Sprintf("[migration:%s] %s [%d/%d]", escapeControlCharacters(direction, false), escapedMigrationName, step, total)

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

func Up(ctx context.Context, db *bun.DB, migrationName string, queries []Query, option RunnerOption) error {
    return RunQueries(ctx, db, "up", migrationName, queries, option)
}

func Down(ctx context.Context, db *bun.DB, migrationName string, queries []Query, option RunnerOption) error {
    return RunQueries(ctx, db, "down", migrationName, queries, option)
}

type migrationPrinter struct {
    writer  io.Writer
    noColor bool
    /* warn receives the empty migration's warning in place of the writer, set by a command whose writer is discarded under --format=json */
    warn func(message string)
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
    message := fmt.Sprintf("[migration:%s] %s: WARNING no queries to execute; the migration is marked applied without running anything", escapeControlCharacters(direction, false), escapeControlCharacters(migrationName, false))

    if nil != instance.warn {
        instance.warn(message)

        return
    }

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
