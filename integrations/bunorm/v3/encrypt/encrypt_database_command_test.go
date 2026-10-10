package encrypt

import (
    "context"
    "io"
    "strings"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    "github.com/uptrace/bun"

    melodycli "github.com/precision-soft/melody/v3/cli"
    "github.com/precision-soft/melody/v3/exception"
)

/* the in-process cron runner dispatches overlapping executions of one command, so concurrent first runs must share one memoized migrator and run the database resolver exactly once — an unsynchronized memo races, and every loser opens a connection pool nothing ever closes. */
func TestEncryptDatabaseCommand_ConcurrentRunsShareOneResolvedMigrator(t *testing.T) {
    var resolveCount atomic.Int32

    command := NewEncryptDatabaseCommandFromResolver(
        func() (*bun.DB, error) {
            resolveCount.Add(1)
            time.Sleep(50 * time.Millisecond)

            return newMysqlDatabase(), nil
        },
        NewFakeCipher(),
    )

    migrators := make(chan *Migrator, 8)

    var waitGroup sync.WaitGroup
    for index := 0; index < 8; index++ {
        waitGroup.Add(1)
        go func() {
            defer waitGroup.Done()

            migrator, resolveErr := command.resolveMigrator()
            if nil != resolveErr {
                t.Errorf("unexpected resolve error: %v", resolveErr)

                return
            }

            migrators <- migrator
        }()
    }

    waitGroup.Wait()
    close(migrators)

    first := <-migrators
    if nil == first {
        t.Fatal("expected a resolved migrator")
    }

    for migrator := range migrators {
        if first != migrator {
            t.Fatal("expected every concurrent run to share the one memoized migrator")
        }
    }

    if 1 != resolveCount.Load() {
        t.Fatalf("expected the database resolver to run exactly once, ran %d times", resolveCount.Load())
    }
}

func runEncryptDatabaseCommand(t *testing.T, command *EncryptDatabaseCommand, extraArgs []string) error {
    t.Helper()

    return melodycli.DispatchCommand(
        context.Background(),
        command,
        fakeRuntime{},
        append([]string{command.Name()}, extraArgs...),
        io.Discard,
    )
}

/* a negative batch silently became the default of 500, so the operator who believed they had throttled the run had not */
func TestEncryptDatabaseCommand_RefusesANegativeBatch(t *testing.T) {
    command := NewEncryptDatabaseCommand(newMysqlDatabase(), NewFakeCipher())

    runErr := runEncryptDatabaseCommand(t, command, []string{"--table", "accounts", "--column", "iban", "--batch", "-1"})
    if nil == runErr {
        t.Fatalf("expected the negative batch to be refused")
    }

    if false == strings.Contains(runErr.Error(), "--batch must not be negative") {
        t.Fatalf("expected the refusal to name the flag, got: %v", runErr)
    }
}

/* a run that stops halfway leaves the column mixed; the rows already converted are the one number that says what a re-run costs */
func TestEncryptDatabaseCommand_CarriesTheProcessedCountOnFailure(t *testing.T) {
    command := NewEncryptDatabaseCommand(newMysqlDatabase(), NewFakeCipher())

    runErr := runEncryptDatabaseCommand(t, command, []string{"--table", "accounts", "--column", "iban"})
    if nil == runErr {
        t.Fatalf("expected the offline database to fail the run")
    }

    if false == strings.Contains(runErr.Error(), "encrypt database migration failed") {
        t.Fatalf("expected the failure wrap, got: %v", runErr)
    }

    logContext := exception.LogContext(runErr)
    if _, hasProcessed := logContext["processedRows"]; false == hasProcessed {
        t.Fatalf("expected the failure to carry the processed count, got context: %v", logContext)
    }
}
