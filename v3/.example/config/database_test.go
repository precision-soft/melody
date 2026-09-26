package config

import (
    "context"
    "database/sql"
    "errors"
    "io"
    "strings"
    "testing"
    "time"

    melodypgsql "github.com/precision-soft/melody/integrations/bunorm/pgsql/v3"
    melodybunorm "github.com/precision-soft/melody/integrations/bunorm/v3"
    "github.com/precision-soft/melody/v3/.example/generated"
    "github.com/precision-soft/melody/v3/.example/persistence"
    "github.com/precision-soft/melody/v3/.example/repository"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
    melodylogging "github.com/precision-soft/melody/v3/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    bun "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/pgdialect"
)

func TestDialIsInsecureOnlyOnTheExactSpelling(t *testing.T) {
    if false == dialIsInsecure("true") {
        t.Fatal("expected the exact spelling to arm the insecure dial")
    }

    for _, value := range []string{"", "false", "TRUE", "1", "yes", " true"} {
        if true == dialIsInsecure(value) {
            t.Fatalf("expected %q to keep the verified handshake", value)
        }
    }
}

/* the registry's refusal reads the same for either database — "database connection failed" over a host — so the handle providers name the manager and where it is: the operator reading a console knows WHICH database refused before knowing why, and the registry's refusal stays the cause */
func TestDatabaseOpenedBy_NamesTheManagerAndItsLocationWhenTheOpenRefuses(t *testing.T) {
    registry, registryErr := melodybunorm.NewManagerRegistryWithContext(
        context.Background(),
        melodylogging.NewNopLogger(),
        melodybunorm.ProviderDefinition{
            Name:      "declared",
            Provider:  melodypgsql.NewProvider(),
            Params:    melodybunorm.ConnectionParameters{Host: "127.0.0.1", Port: "1", Database: "never", User: "nobody"},
            IsDefault: true,
        },
    )
    if nil != registryErr {
        t.Fatalf("build the registry: %v", registryErr)
    }

    database, openErr := databaseOpenedBy(registry, "archive", "postgres:5432/melody_example_v3_archive")
    if nil == openErr || nil != database {
        t.Fatalf("expected a manager the registry does not declare to be refused, got db=%v err=%v", database, openErr)
    }

    if false == strings.Contains(openErr.Error(), "the archive database at postgres:5432/melody_example_v3_archive could not be opened") {
        t.Fatalf("expected the refusal to name the manager and its location, got %q", openErr.Error())
    }

    if "archive" != exception.LogContext(openErr)["manager"] {
        t.Fatalf("expected the context to carry the manager, got %v", exception.LogContext(openErr))
    }

    if nil == errors.Unwrap(openErr) {
        t.Fatal("expected the registry's own refusal to stay the cause")
    }
}

/* the reading repository is resolved by type through the generated wiring, and a refusal of the archive's database on that first resolution reaches the console naming the archive set and its step, not as the container's "service not registered in resolver", the relabelling it applies to any error that is not this application's own. */
func TestArchiveReadingRepositoryResolvedByTypeNamesTheArchiveRatherThanTheWiring(t *testing.T) {
    serviceContainer := melodycontainer.NewContainer()

    melodycontainer.MustRegister(
        serviceContainer,
        persistence.ServiceArchiveStorage,
        func(resolver melodycontainercontract.Resolver) (*persistence.ArchiveStorage, error) {
            return persistence.NewArchiveStorageAt(bun.NewDB(sql.OpenDB(&refusingConnector{}), pgdialect.New()), "postgres:5432/melody_example_v3_archive"), nil
        },
    )

    generated.RegisterGeneratedServices(serviceContainer)

    _, resolveErr := melodycontainer.FromResolverByType[repository.CatalogReadingRepository](serviceContainer)
    if nil == resolveErr {
        t.Fatal("expected the refusing archive to fail the resolution")
    }

    if true == strings.Contains(resolveErr.Error(), "service not registered") {
        t.Fatalf("expected the refusal to name the archive, not the wiring, got %q", resolveErr.Error())
    }

    if false == strings.Contains(resolveErr.Error(), "on the archive set") {
        t.Fatalf("expected the refusal to name the archive set, got %q", resolveErr.Error())
    }
}

/* the registry's lazy opens are bound to the process's context: the archive is opened on a request or a scheduled command, and an open in flight when the process is asked to stop ends with the signal rather than running its whole retry budget while the teardown waits. Cancelled, the open ends at once naming the cancellation, and the registry closes with nothing pending. */
func TestBuildDatabase_BindsTheArchiveOpenToTheProcessContext(t *testing.T) {
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    moduleInstance := moduleWithEnvironment(t, map[string]string{
        environmentKeyPgsqlHost:     "127.0.0.1",
        environmentKeyPgsqlPort:     "1",
        environmentKeyPgsqlDatabase: "never",
        environmentKeyPgsqlUser:     "nobody",
        environmentKeyPgsqlPassword: "nothing",
    })
    moduleInstance.processContext = ctx
    moduleInstance.buildDatabase()

    if nil == moduleInstance.databaseRegistry || false == moduleInstance.archiveWired {
        t.Fatal("expected the archive to be declared on the registry")
    }

    openDone := make(chan error, 1)
    go func() {
        _, openErr := moduleInstance.databaseRegistry.Database(databaseArchiveManagerName)
        openDone <- openErr
    }()

    time.Sleep(50 * time.Millisecond)
    cancel()

    select {
    case openErr := <-openDone:
        if nil == openErr || false == strings.Contains(openErr.Error(), "cancelled by the caller's context") {
            t.Fatalf("expected the open to end with the cancellation, got %v", openErr)
        }
    case <-time.After(2 * time.Second):
        t.Fatal("expected the cancelled open to end at once; it is still retrying")
    }

    closeContext, cancelClose := context.WithTimeout(context.Background(), 2*time.Second)
    defer cancelClose()

    if closeErr := moduleInstance.databaseRegistry.CloseWithContext(closeContext); nil != closeErr {
        t.Fatalf("expected the registry to close with nothing in flight, got %v", closeErr)
    }
}

/* the archive's open budget is its own and request-sized: against a postgres that refuses, the open gives up in under three seconds, not after the catalogue's ten attempts one to five seconds apart. */
func TestBuildDatabase_GivesTheArchiveARequestSizedOpenBudget(t *testing.T) {
    moduleInstance := moduleWithEnvironment(t, map[string]string{
        environmentKeyPgsqlHost:     "127.0.0.1",
        environmentKeyPgsqlPort:     "1",
        environmentKeyPgsqlDatabase: "never",
        environmentKeyPgsqlUser:     "nobody",
        environmentKeyPgsqlPassword: "nothing",
    })
    moduleInstance.buildDatabase()

    startedAt := time.Now()
    _, openErr := moduleInstance.databaseRegistry.Database(databaseArchiveManagerName)
    elapsed := time.Since(startedAt)

    /* the registry's own headline is "database connection failed"; the attempts spent are in its cause */
    if nil == openErr || false == strings.Contains(openErr.Error(), "database connection failed") {
        t.Fatalf("expected the open to give up after its attempts, got %v", openErr)
    }

    if 3*time.Second < elapsed {
        t.Fatalf("expected the archive's budget to be spent in under three seconds, took %s", elapsed)
    }
}

/* the first resolution of the archive repository applies the migration set after the dial, a wait of up to the lock window on a held migration lock; that wait runs under the process context the storage carries, so a cancelled process ends it at once rather than running the whole window while the teardown waits. Over a refusing connector the dial refuses first either way, so the arm that separates the two is the context's refusal being what the migration set hands back. */
func TestArchiveStorageCarriesTheProcessContextIntoTheMigrationSet(t *testing.T) {
    ctx, cancel := context.WithCancel(context.Background())
    cancel()

    storage := persistence.NewArchiveStorageAt(bun.NewDB(sql.OpenDB(&refusingConnector{}), pgdialect.New()), "postgres:5432/melody_example_v3_archive").WithContext(ctx)

    if ctx != storage.Context() {
        t.Fatal("expected the storage to hand back the context it was bound to")
    }

    if context.Background() != persistence.NewArchiveStorage(nil).Context() {
        t.Fatal("expected a storage nobody bound to answer a background context")
    }

    _, resolveErr := repository.NewCatalogReadingRepository(storage)
    if nil == resolveErr {
        t.Fatal("expected the cancelled context to refuse the first resolution")
    }

    if false == errors.Is(resolveErr, context.Canceled) {
        t.Fatalf("expected the migration set to hand back the context's cancellation, got %v", resolveErr)
    }
}

/* the composition root binds the process context onto the handle it publishes */
func TestRegisterArchiveStorageService_BindsTheProcessContext(t *testing.T) {
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    moduleInstance := &Module{processContext: ctx, archiveWired: true, archiveLocation: "postgres:5432/melody_example_v3_archive"}
    serviceContainer := melodycontainer.NewContainer()
    melodycontainer.MustRegister(
        serviceContainer,
        serviceArchiveDatabase,
        func(resolver melodycontainercontract.Resolver) (*bun.DB, error) {
            return newUndialedDatabase(), nil
        },
    )
    moduleInstance.registerArchiveStorageService(&containerRegistrar{Container: serviceContainer})

    storage, resolveErr := melodycontainer.FromResolver[*persistence.ArchiveStorage](serviceContainer, persistence.ServiceArchiveStorage)
    if nil != resolveErr {
        t.Fatalf("resolving the archive storage failed: %v", resolveErr)
    }

    if ctx != storage.Context() {
        t.Fatal("expected the published storage to carry the process context")
    }
}

/* recordingProvider opens a handle over a connector that is never dialed and records the logger the registry
   handed it: the journal an open reports through is the registry's current logger at that moment */
type recordingProvider struct {
    openedWith []melodyloggingcontract.Logger
}

func (instance *recordingProvider) Open(params melodybunorm.ConnectionParameters, logger melodyloggingcontract.Logger) (*bun.DB, error) {
    instance.openedWith = append(instance.openedWith, logger)

    return newUndialedDatabase(), nil
}

/* databaseServicesOver registers the database services of a module whose registry declares the catalogue and the archive over recording providers, beside a logger service that counts its resolutions; the logger it publishes is returned, so a test compares the logger an open receives with it */
func databaseServicesOver(t *testing.T, archiveWired bool) (melodycontainercontract.Container, *recordingProvider, *recordingProvider, melodyloggingcontract.Logger, *int) {
    t.Helper()

    catalogProvider := &recordingProvider{}
    archiveProvider := &recordingProvider{}

    registry, registryErr := melodybunorm.NewManagerRegistryWithContext(
        context.Background(),
        melodylogging.NewNopLogger(),
        melodybunorm.ProviderDefinition{
            Name:      databaseManagerName,
            Provider:  catalogProvider,
            Params:    melodybunorm.ConnectionParameters{Host: "mysql", Port: "3306", Database: "catalogue", User: "melody"},
            IsDefault: true,
        },
        melodybunorm.ProviderDefinition{
            Name:     databaseArchiveManagerName,
            Provider: archiveProvider,
            Params:   melodybunorm.ConnectionParameters{Host: "postgres", Port: "5432", Database: "archive", User: "melody"},
        },
    )
    if nil != registryErr {
        t.Fatalf("build the registry: %v", registryErr)
    }

    /* a logger of its own, so the open handed THIS one and not the one the registry was built on */
    journal := melodylogging.NewJsonLogger(io.Discard, melodyloggingcontract.LevelDebug)
    loggerResolutions := 0

    containerInstance := melodycontainer.NewContainer()
    t.Cleanup(func() { _ = containerInstance.Close() })
    containerInstance.MustRegister(
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            loggerResolutions++

            return journal, nil
        },
    )

    moduleInstance := moduleWithEnvironment(t, map[string]string{environmentKeyMysqlHost: "mysql"})
    moduleInstance.databaseRegistry = registry
    moduleInstance.archiveWired = archiveWired
    moduleInstance.catalogLocation = "mysql:3306/catalogue"
    moduleInstance.archiveLocation = "postgres:5432/archive"
    moduleInstance.registerDatabaseServices(containerRegistrar{Container: containerInstance})

    return containerInstance, catalogProvider, archiveProvider, journal, &loggerResolutions
}

/* the chain the catalogue storage starts (storage, handle, registry, journal) continues here: the handle resolves the registry, and the registry's provider hands it the container's journal before the handle is opened, so the open reports through the application's logger rather than the emergency one the registry is built on. A handle that captured the registry would resolve nothing, so the swap would not run and the teardown would have no edge to order the two closes by. */
func TestRegisterDatabaseServices_EachHandleResolvesTheRegistryWhichTakesTheJournalFirst(t *testing.T) {
    for _, handle := range []struct {
        serviceName string
        archive     bool
    }{
        {serviceName: serviceDatabase},
        {serviceName: serviceArchiveDatabase, archive: true},
    } {
        containerInstance, catalogProvider, archiveProvider, journal, loggerResolutions := databaseServicesOver(t, true)

        database, openErr := melodycontainer.FromResolver[*bun.DB](containerInstance, handle.serviceName)
        if nil != openErr || nil == database {
            t.Fatalf("%s: expected the handle opened, got %v, %v", handle.serviceName, database, openErr)
        }

        opened := catalogProvider
        if true == handle.archive {
            opened = archiveProvider
        }

        if 1 != *loggerResolutions {
            t.Fatalf("%s: expected the registry's provider to run and resolve the journal once, got %d", handle.serviceName, *loggerResolutions)
        }

        if 1 != len(opened.openedWith) || journal != opened.openedWith[0] {
            t.Fatalf("%s: expected the open handed the container's journal, got %v", handle.serviceName, opened.openedWith)
        }
    }
}

/* both handles are asked for by NAME: a resolution by type between two handles of one concrete type could only
   answer whichever landed first, so the question is not askable at all. And without an archive the archive's
   handle is not registered, the catalogue's still is. */
func TestRegisterDatabaseServices_KeepsBothHandlesOffTheTypeIndexAndTheArchiveBehindItsSwitch(t *testing.T) {
    containerInstance, _, _, _, _ := databaseServicesOver(t, true)
    if false == containerInstance.Has(serviceDatabase) || false == containerInstance.Has(serviceArchiveDatabase) {
        t.Fatal("expected both handles registered with both databases declared")
    }

    if _, byTypeErr := melodycontainer.FromResolverByType[*bun.DB](containerInstance); nil == byTypeErr {
        t.Fatal("expected no handle on the by-type index")
    }

    withoutArchive, _, _, _, _ := databaseServicesOver(t, false)
    if false == withoutArchive.Has(serviceDatabase) || true == withoutArchive.Has(serviceArchiveDatabase) {
        t.Fatal("expected the catalogue's handle alone without the archive")
    }
}
