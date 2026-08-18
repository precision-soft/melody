package cli

/* The shared test material of the package: the runtime and command harness every command mirror drives its command through, and the journal stub two of them feed. This is the one test file the layout rule exempts from having a source of its own. */

import (
    "bytes"
    "context"
    "testing"
    "time"

    examplecache "github.com/precision-soft/melody/v2/.example/cache"
    "github.com/precision-soft/melody/v2/.example/repository"
    melodycache "github.com/precision-soft/melody/v2/cache"
    melodycachecontract "github.com/precision-soft/melody/v2/cache/contract"
    melodyclicontract "github.com/precision-soft/melody/v2/cli/contract"
    melodyclock "github.com/precision-soft/melody/v2/clock"
    melodycontainer "github.com/precision-soft/melody/v2/container"
    melodycontainercontract "github.com/precision-soft/melody/v2/container/contract"
    melodyruntime "github.com/precision-soft/melody/v2/runtime"
    melodyruntimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

/* fixtureCliTime is a fixed stamp, so a rendered value never changes between runs. */
var fixtureCliTime = time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)

/* newCliTestCache builds a real in-memory cache and ties its cleanup goroutine to the test, because the services refuse a nil cache. */
func newCliTestCache(t *testing.T) melodycachecontract.Cache {
    t.Helper()

    manager := melodycache.NewManagerOwningBackend(
        melodycache.NewInMemoryBackend(0, time.Minute, melodyclock.NewFrozenClock(fixtureCliTime)),
        examplecache.NewGobSerializer(),
    )
    t.Cleanup(func() {
        _ = manager.Close()
    })

    return manager
}

func newCliTestRuntime(serviceContainer melodycontainercontract.Container) melodyruntimecontract.Runtime {
    return melodyruntime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)
}

func registerCliTestService[T any](serviceContainer melodycontainercontract.Container, serviceName string, serviceInstance T) {
    melodycontainer.MustRegister[T](
        serviceContainer,
        serviceName,
        func(resolver melodycontainercontract.Resolver) (T, error) {
            return serviceInstance, nil
        },
    )
}

/* runCliCommand drives the command through a real CommandContext, so the standard flags are parsed by the same layer that parses them in production. */
func runCliCommand(
    command melodyclicontract.Command,
    runtimeInstance melodyruntimecontract.Runtime,
    arguments []string,
) (string, error) {
    buffer := &bytes.Buffer{}

    commandContext := &melodyclicontract.CommandContext{
        Name:      command.Name(),
        Flags:     command.Flags(),
        Writer:    buffer,
        ErrWriter: buffer,
        ExitErrHandler: func(
            handlerContext context.Context,
            handlerCommandContext *melodyclicontract.CommandContext,
            handlerErr error,
        ) {
        },
        Action: func(
            actionContext context.Context,
            actionCommandContext *melodyclicontract.CommandContext,
        ) error {
            return command.Run(runtimeInstance, actionCommandContext)
        },
    }

    commandArguments := make([]string, 0, len(arguments)+1)
    commandArguments = append(commandArguments, command.Name())
    commandArguments = append(commandArguments, arguments...)

    runErr := commandContext.Run(context.Background(), commandArguments)

    return buffer.String(), runErr
}

type stubJournalRepository struct {
    entries []*repository.CatalogJournalEntry
}

func (instance *stubJournalRepository) Append(ctx context.Context, entry *repository.CatalogJournalEntry) (*repository.CatalogJournalEntry, error) {
    instance.entries = append(instance.entries, entry)

    return entry, nil
}

/* Latest answers newest first the way both real implementations do, honouring the zero-means-all contract the command relies on. */
func (instance *stubJournalRepository) Latest(ctx context.Context, limit int) ([]*repository.CatalogJournalEntry, error) {
    newest := make([]*repository.CatalogJournalEntry, 0, len(instance.entries))
    for index := len(instance.entries) - 1; index >= 0; index-- {
        newest = append(newest, instance.entries[index])
    }

    if 0 < limit && limit < len(newest) {
        newest = newest[:limit]
    }

    return newest, nil
}

func (instance *stubJournalRepository) Count(ctx context.Context) (int, error) {
    return len(instance.entries), nil
}

var _ repository.CatalogJournalRepository = (*stubJournalRepository)(nil)
