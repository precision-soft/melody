package config

import (
    "context"
    "database/sql"
    "errors"
    "strings"
    "sync"
    "testing"
    "time"

    melodymysql "github.com/precision-soft/melody/integrations/bunorm/mysql/v4"
    melodypgsql "github.com/precision-soft/melody/integrations/bunorm/pgsql/v4"
    melodybunorm "github.com/precision-soft/melody/integrations/bunorm/v4"
    "github.com/precision-soft/melody/v4/.example/generated"
    "github.com/precision-soft/melody/v4/.example/persistence"
    "github.com/precision-soft/melody/v4/.example/repository"
    "github.com/precision-soft/melody/v4/bound"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
    melodylock "github.com/precision-soft/melody/v4/lock"
    melodylockcontract "github.com/precision-soft/melody/v4/lock/contract"
    melodylogging "github.com/precision-soft/melody/v4/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    bun "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/pgdialect"
)

func TestInsecureSwitch_ArmsOnTheExactSpellingAndRefusesAnyOtherValueByName(t *testing.T) {
    for _, switchKey := range []struct{ environmentKey, parameter string }{
        {environmentKeyMysqlInsecure, parameterMysqlInsecure},
        {environmentKeyPgsqlInsecure, parameterPgsqlInsecure},
        {environmentKeyS3Insecure, parameterS3Insecure},
    } {
        for value, insecure := range map[string]bool{"": false, "false": false, "true": true} {
            if insecure != moduleWithEnvironment(t, map[string]string{switchKey.environmentKey: value}).booleanSwitch(switchKey.environmentKey, switchKey.parameter) {
                t.Fatalf("expected %s=%q to dial insecure=%v", switchKey.environmentKey, value, insecure)
            }
        }

        for _, value := range []string{"TRUE", "1", "yes", " true"} {
            moduleInstance := moduleWithEnvironment(t, map[string]string{switchKey.environmentKey: value})

            refusal := recoveredRefusal(t, func() { moduleInstance.booleanSwitch(switchKey.environmentKey, switchKey.parameter) })
            if nil == refusal || false == strings.Contains(refusal.Error(), "the switch is neither true nor false") {
                t.Fatalf("expected %s=%q refused by name, got %v", switchKey.environmentKey, value, refusal)
            }
        }
    }
}

/* the registry's refusal reads the same for either database — "database connection failed" over a host — so the handle providers name the manager and where it is: the operator reading a console knows WHICH database refused before knowing why, and the registry's refusal stays the cause */
func TestDatabaseOpenedBy_NamesTheManagerAndItsLocationWhenTheOpenRefuses(t *testing.T) {
    registry, registryErr := melodybunorm.NewManagerRegistry(
        melodylogging.NewNopLogger(),
        []melodybunorm.ProviderDefinition{
            melodybunorm.ProviderDefinition{
                Name:       "declared",
                Provider:   melodypgsql.NewProvider(melodypgsql.ProviderConfig{Retry: melodypgsql.RetryConfig{MaxAttempts: bound.Of(1)}}),
                Parameters: melodybunorm.ConnectionParameters{Host: "127.0.0.1", Port: "1", Database: "never", User: "nobody"},
                IsDefault:  true,
            },
        },
    )
    if nil != registryErr {
        t.Fatalf("build the registry: %v", registryErr)
    }

    database, openErr := databaseOpenedBy(databaseTestRuntime(context.Background()), registry, "archive", "postgres:5432/melody_example_v4_archive")
    if nil == openErr || nil != database {
        t.Fatalf("expected a manager the registry does not declare to be refused, got db=%v err=%v", database, openErr)
    }

    if false == strings.Contains(openErr.Error(), "the archive database at postgres:5432/melody_example_v4_archive could not be opened") {
        t.Fatalf("expected the refusal to name the manager and its location, got %q", openErr.Error())
    }

    if "archive" != exception.LogContext(openErr, exception.LogContextConfig{})["manager"] {
        t.Fatalf("expected the context to carry the manager, got %v", exception.LogContext(openErr, exception.LogContextConfig{}))
    }

    if nil == errors.Unwrap(openErr) {
        t.Fatal("expected the registry's own refusal to stay the cause")
    }
}

/* the reading repository is resolved by type through the generated wiring, and a refusal of the archive's database on that first resolution reaches the console naming the archive set and its step, not as the container's "service not registered in resolver", the relabelling it applies to any error that is not this application's own. */
func TestArchiveReadingRepositoryResolvedByTypeNamesTheArchiveRatherThanTheWiring(t *testing.T) {
    serviceContainer := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})

    melodycontainer.MustRegister(
        serviceContainer,
        persistence.ServiceArchiveStorage,
        func(resolver melodycontainercontract.Resolver) (*persistence.ArchiveStorage, error) {
            return persistence.NewArchiveStorageAt(bun.NewDB(sql.OpenDB(&refusingConnector{}), pgdialect.New()), "postgres:5432/melody_example_v4_archive"), nil
        },
        melodycontainer.RegisterOptions{},
    )

    generated.RegisterGeneratedServices(serviceContainer)

    _, resolveErr := melodycontainer.FromResolverByType[repository.CatalogReadingRepository](t.Context(), serviceContainer)
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

/* databaseTestRuntime is a run over a scope of an empty container, under ctx */
func databaseTestRuntime(ctx context.Context) melodycontainercontract.Runtime {
    return melodyruntime.NewRun(ctx, melodycontainer.NewContainer(melodycontainer.ContainerConfig{}).NewScope())
}

/* registryOver resolves the registry the module registers, through its own provider, beside a logger service */
func registryOver(t *testing.T, moduleInstance *Module) *melodybunorm.ManagerRegistry {
    t.Helper()

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    t.Cleanup(func() { _ = containerInstance.CloseWithContext(context.Background()) })
    containerInstance.MustRegister(
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return melodylogging.NewNopLogger(), nil
        },
        melodycontainer.RegisterOptions{},
    )

    moduleInstance.registerDatabaseServices(containerRegistrar{Container: containerInstance})

    registry, registryErr := melodycontainer.FromResolver[*melodybunorm.ManagerRegistry](t.Context(), containerInstance, serviceDatabaseRegistry)
    if nil != registryErr {
        t.Fatalf("resolve the registry: %v", registryErr)
    }

    return registry
}

/* the registry's lazy opens run under the runtime of the caller that asked: the archive is opened on a request or a scheduled command, and an open in flight when that caller goes away ends with its context rather than running its whole retry budget. Cancelled, the open ends at once naming the cancellation, and the registry closes with nothing pending. */
func TestBuildDatabase_RunsTheArchiveOpenUnderTheCallersRuntime(t *testing.T) {
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    moduleInstance := moduleWithEnvironment(t, map[string]string{
        environmentKeyPgsqlHost:     "127.0.0.1",
        environmentKeyPgsqlPort:     "1",
        environmentKeyPgsqlDatabase: "never",
        environmentKeyPgsqlUser:     "nobody",
        environmentKeyPgsqlPassword: "nothing",
    })
    moduleInstance.buildDatabase()

    if 0 == len(moduleInstance.databaseDefinitions) || false == moduleInstance.archiveWired {
        t.Fatal("expected the archive to be declared for the registry")
    }

    registry := registryOver(t, moduleInstance)

    openDone := make(chan error, 1)
    go func() {
        _, openErr := registry.Database(databaseTestRuntime(ctx), databaseArchiveManagerName)
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

    if closeErr := registry.CloseWithContext(closeContext); nil != closeErr {
        t.Fatalf("expected the registry to close with nothing in flight, got %v", closeErr)
    }
}

/* the catalogue is opened by the first request that reads it, so its budget is request-sized too: against a mysql that refuses, the open gives up in under three seconds, not after ten attempts one to five seconds apart. */
func TestBuildDatabase_GivesTheCatalogueARequestSizedOpenBudget(t *testing.T) {
    moduleInstance := moduleWithEnvironment(t, map[string]string{
        environmentKeyMysqlHost:     "127.0.0.1",
        environmentKeyMysqlPort:     "1",
        environmentKeyMysqlDatabase: "never",
        environmentKeyMysqlUser:     "nobody",
        environmentKeyMysqlPassword: "nothing",
    })
    moduleInstance.buildDatabase()

    startedAt := time.Now()
    _, openErr := registryOver(t, moduleInstance).Database(databaseTestRuntime(context.Background()), databaseManagerName)
    elapsed := time.Since(startedAt)

    if nil == openErr || false == strings.Contains(openErr.Error(), "database connection failed") {
        t.Fatalf("expected the open to give up after its attempts, got %v", openErr)
    }

    if 3*time.Second < elapsed {
        t.Fatalf("expected the catalogue's budget to be spent in under three seconds, took %s", elapsed)
    }
}

/* the archive's open budget is request-sized: against a postgres that refuses, the open gives up in under three seconds. */
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
    _, openErr := registryOver(t, moduleInstance).Database(databaseTestRuntime(context.Background()), databaseArchiveManagerName)
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

    storage := persistence.NewArchiveStorageAt(bun.NewDB(sql.OpenDB(&refusingConnector{}), pgdialect.New()), "postgres:5432/melody_example_v4_archive").WithContext(ctx)

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

    moduleInstance := &Module{processContext: ctx, archiveWired: true, archiveLocation: "postgres:5432/melody_example_v4_archive"}
    serviceContainer := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    melodycontainer.MustRegister(
        serviceContainer,
        serviceArchiveDatabase,
        func(resolver melodycontainercontract.Resolver) (*bun.DB, error) {
            return newUndialedDatabase(), nil
        },
        melodycontainer.RegisterOptions{Closer: registryOwnedDatabaseCloser},
    )
    moduleInstance.registerArchiveStorageService(&containerRegistrar{Container: serviceContainer})

    storage, resolveErr := melodycontainer.FromResolver[*persistence.ArchiveStorage](ctx, serviceContainer, persistence.ServiceArchiveStorage)
    if nil != resolveErr {
        t.Fatalf("resolving the archive storage failed: %v", resolveErr)
    }

    if ctx != storage.Context() {
        t.Fatal("expected the published storage to carry the process context")
    }
}

/* recordingProvider opens a handle over a connector that is never dialed and records the logger the registry
   handed it: the journal an open reports through is the logger the registry was built on */
type recordingProvider struct {
    openedWith []melodyloggingcontract.Logger
}

func (instance *recordingProvider) Open(runtimeInstance melodycontainercontract.Runtime, parameters melodybunorm.ConnectionParameters, logger melodyloggingcontract.Logger) (*bun.DB, error) {
    instance.openedWith = append(instance.openedWith, logger)

    return newUndialedDatabase(), nil
}

func (instance *recordingProvider) OpenForMigration(runtimeInstance melodycontainercontract.Runtime, parameters melodybunorm.ConnectionParameters, logger melodyloggingcontract.Logger) (*bun.DB, error) {
    return instance.Open(runtimeInstance, parameters, logger)
}

/* databaseServicesOver registers the database services of a module whose registry declares the catalogue and the archive over recording providers, beside a logger service that counts its resolutions; the buffer that logger writes into is returned, so a test reads where a record written through the logger an open receives lands */
func databaseServicesOver(t *testing.T, archiveWired bool) (melodycontainercontract.Container, *recordingProvider, *recordingProvider, *lockedJournalBuffer, *int) {
    t.Helper()

    catalogProvider := &recordingProvider{}
    archiveProvider := &recordingProvider{}

    definitions := []melodybunorm.ProviderDefinition{
        melodybunorm.ProviderDefinition{
            Name:       databaseManagerName,
            Provider:   catalogProvider,
            Parameters: melodybunorm.ConnectionParameters{Host: "mysql", Port: "3306", Database: "catalogue", User: "melody"},
            IsDefault:  true,
        },
        melodybunorm.ProviderDefinition{
            Name:       databaseArchiveManagerName,
            Provider:   archiveProvider,
            Parameters: melodybunorm.ConnectionParameters{Host: "postgres", Port: "5432", Database: "archive", User: "melody"},
        },
    }

    /* a logger of its own, so a record reaching THIS one proves the registry's provider built the registry on the container's journal */
    journalBuffer := &lockedJournalBuffer{}
    journal := melodylogging.NewJsonLogger(journalBuffer, melodylogging.JsonLoggerConfig{MinLevel: melodyloggingcontract.LevelDebug})
    loggerResolutions := 0

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    t.Cleanup(func() { _ = containerInstance.CloseWithContext(context.Background()) })
    containerInstance.MustRegister(
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            loggerResolutions++

            return journal, nil
        },
        melodycontainer.RegisterOptions{},
    )

    moduleInstance := moduleWithEnvironment(t, map[string]string{environmentKeyMysqlHost: "mysql"})
    moduleInstance.databaseDefinitions = definitions
    moduleInstance.archiveWired = archiveWired
    moduleInstance.catalogLocation = "mysql:3306/catalogue"
    moduleInstance.archiveLocation = "postgres:5432/archive"
    moduleInstance.registerDatabaseServices(containerRegistrar{Container: containerInstance})

    return containerInstance, catalogProvider, archiveProvider, journalBuffer, &loggerResolutions
}

/* lockedJournalBuffer is the journal's destination, read by the test while the application may still write */
type lockedJournalBuffer struct {
    mutex   sync.Mutex
    content strings.Builder
}

func (instance *lockedJournalBuffer) Write(payload []byte) (int, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return instance.content.Write(payload)
}

func (instance *lockedJournalBuffer) String() string {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return instance.content.String()
}

/* the chain the catalogue storage starts (storage, handle, registry, journal) continues here: the handle resolves the registry, and the registry's provider builds it on the container's journal before the handle is opened, so the open reports through the application's logger. A handle that captured the registry would resolve nothing, so the teardown would have no edge to order the two closes by. */
func TestRegisterDatabaseServices_EachHandleResolvesTheRegistryWhichTakesTheJournalFirst(t *testing.T) {
    for _, handle := range []struct {
        serviceName string
        archive     bool
    }{
        {serviceName: serviceDatabase},
        {serviceName: serviceArchiveDatabase, archive: true},
    } {
        containerInstance, catalogProvider, archiveProvider, journalBuffer, loggerResolutions := databaseServicesOver(t, true)

        database, openErr := melodycontainer.FromResolver[*bun.DB](t.Context(), containerInstance, handle.serviceName)
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

        if 1 != len(opened.openedWith) {
            t.Fatalf("%s: expected one open, got %d", handle.serviceName, len(opened.openedWith))
        }

        /* the registry hands its own logger, which carries the application's, so the journal is proven by where a record lands rather than by identity */
        opened.openedWith[0].Warning("open reported through the journal", nil)
        if false == strings.Contains(journalBuffer.String(), "open reported through the journal") {
            t.Fatalf("%s: expected the open handed a logger reporting into the container's journal, got %q", handle.serviceName, journalBuffer.String())
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

    if _, byTypeErr := melodycontainer.FromResolverByType[*bun.DB](t.Context(), containerInstance); nil == byTypeErr {
        t.Fatal("expected no handle on the by-type index")
    }

    withoutArchive, _, _, _, _ := databaseServicesOver(t, false)
    if false == withoutArchive.Has(serviceDatabase) || true == withoutArchive.Has(serviceArchiveDatabase) {
        t.Fatal("expected the catalogue's handle alone without the archive")
    }
}

/* a declared catalogue is opened by its service at the first resolution, never by the build: against a mysql that refuses, the build answers at once with the catalogue wired and nothing opened, so a process that never reads the catalogue holds no pool for the teardown to miss */
func TestBuildDatabase_DeclaresTheCatalogueWithoutOpeningIt(t *testing.T) {
    moduleInstance := moduleWithEnvironment(t, map[string]string{
        environmentKeyMysqlHost:     "127.0.0.1",
        environmentKeyMysqlPort:     "1",
        environmentKeyMysqlDatabase: "never",
        environmentKeyMysqlUser:     "nobody",
        environmentKeyMysqlPassword: "nothing",
    })

    built := make(chan struct{})
    go func() {
        moduleInstance.buildDatabase()
        close(built)
    }()

    select {
    case <-built:
    case <-time.After(2 * time.Second):
        t.Fatal("expected the build to open nothing; it is dialing the catalogue")
    }

    if 0 == len(moduleInstance.databaseDefinitions) || false == moduleInstance.catalogueWired {
        t.Fatalf("expected the catalogue declared for the registry and wired, got %d definitions wired %t", len(moduleInstance.databaseDefinitions), moduleInstance.catalogueWired)
    }

    unwired := moduleWithEnvironment(t, map[string]string{})
    unwired.buildDatabase()
    if true == unwired.catalogueWired {
        t.Fatal("expected no catalogue wired without MYSQL_HOST")
    }
}

/* the catalogue's locker resolves the handle at its first use, through the handle's own service, rather than capturing one at registration */
func TestRegisterLockerService_ResolvesTheCatalogueHandleAtFirstUse(t *testing.T) {
    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    t.Cleanup(func() { _ = containerInstance.CloseWithContext(context.Background()) })

    resolutions := 0
    containerInstance.MustRegister(serviceDatabase, func(resolver melodycontainercontract.Resolver) (*bun.DB, error) {
        resolutions++

        return newUndialedDatabase(), nil
    }, melodycontainer.RegisterOptions{Closer: registryOwnedDatabaseCloser})

    moduleInstance := moduleWithEnvironment(t, map[string]string{})
    moduleInstance.catalogueWired = true
    moduleInstance.registerLockerService(containerRegistrar{Container: containerInstance})

    if 0 != resolutions {
        t.Fatalf("expected the registration to resolve no handle, got %d", resolutions)
    }

    locker, lockerErr := melodycontainer.FromResolver[melodylockcontract.Locker](t.Context(), containerInstance, melodylock.ServiceLocker)
    if nil != lockerErr || 1 != resolutions {
        t.Fatalf("expected the locker built over the resolved handle, got %v after %d resolutions", lockerErr, resolutions)
    }

    if _, isMysql := locker.(*melodymysql.Locker); false == isMysql {
        t.Fatalf("expected the catalogue's mysql locker, got %T", locker)
    }
}

/* the registry is built at boot: a declared host whose database is left empty is a definition the registry refuses, so the boot fails naming it rather than the first request that reads the catalogue */
func TestRegisterDatabaseServices_BuildsTheRegistryAtBootSoARefusedDefinitionFailsTheBoot(t *testing.T) {
    moduleInstance := moduleWithEnvironment(t, map[string]string{
        environmentKeyMysqlHost: "127.0.0.1",
        environmentKeyMysqlUser: "nobody",
    })
    moduleInstance.buildDatabase()

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    t.Cleanup(func() { _ = containerInstance.CloseWithContext(context.Background()) })
    containerInstance.MustRegister(
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return melodylogging.NewNopLogger(), nil
        },
        melodycontainer.RegisterOptions{},
    )
    moduleInstance.registerDatabaseServices(containerRegistrar{Container: containerInstance})

    names := containerInstance.(interface{ BuiltAtBootServiceNames() []string }).BuiltAtBootServiceNames()
    if 1 != len(names) || serviceDatabaseRegistry != names[0] {
        t.Fatalf("expected the registry registered to be built at boot, got %v", names)
    }

    bootRuntime, finish := melodyruntime.NewBoot(t.Context(), containerInstance)
    defer func() {
        _ = finish()
    }()

    _, buildErr := bootRuntime.Scope().Get(bootRuntime.Context(), serviceDatabaseRegistry)
    if false == errors.Is(buildErr, melodybunorm.ErrConnectionDatabaseIsRequired) {
        t.Fatalf("expected the boot build refused for the empty database, got %v", buildErr)
    }
}

/* runtimeRecordingProvider opens a handle over a connector that is never dialed and records the context of the runtime the registry handed it */
type runtimeRecordingProvider struct {
    openedUnder []context.Context
}

func (instance *runtimeRecordingProvider) Open(runtimeInstance melodycontainercontract.Runtime, parameters melodybunorm.ConnectionParameters, logger melodyloggingcontract.Logger) (*bun.DB, error) {
    instance.openedUnder = append(instance.openedUnder, runtimeInstance.Context())

    return newUndialedDatabase(), nil
}

func (instance *runtimeRecordingProvider) OpenForMigration(runtimeInstance melodycontainercontract.Runtime, parameters melodybunorm.ConnectionParameters, logger melodyloggingcontract.Logger) (*bun.DB, error) {
    return instance.Open(runtimeInstance, parameters, logger)
}

/* each handle the module registers is opened under the runtime of the resolution that publishes it: the open's context carries that resolution's deadline */
func TestRegisterDatabaseServices_EachHandleOpensUnderTheResolvingRuntime(t *testing.T) {
    for _, serviceName := range []string{serviceDatabase, serviceArchiveDatabase} {
        provider := &runtimeRecordingProvider{}

        containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
        containerInstance.MustRegister(
            melodylogging.ServiceLogger,
            func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
                return melodylogging.NewNopLogger(), nil
            },
            melodycontainer.RegisterOptions{},
        )

        moduleInstance := moduleWithEnvironment(t, map[string]string{environmentKeyMysqlHost: "mysql"})
        moduleInstance.databaseDefinitions = []melodybunorm.ProviderDefinition{
            melodybunorm.ProviderDefinition{Name: databaseManagerName, Provider: provider, Parameters: melodybunorm.ConnectionParameters{Host: "mysql", Port: "3306", Database: "catalogue", User: "melody"}, IsDefault: true},
            melodybunorm.ProviderDefinition{Name: databaseArchiveManagerName, Provider: provider, Parameters: melodybunorm.ConnectionParameters{Host: "postgres", Port: "5432", Database: "archive", User: "melody"}},
        }
        moduleInstance.archiveWired = true
        moduleInstance.registerDatabaseServices(containerRegistrar{Container: containerInstance})

        resolutionContext, cancel := context.WithTimeout(context.Background(), time.Minute)
        expectedDeadline, _ := resolutionContext.Deadline()

        if _, resolveErr := melodycontainer.FromResolver[*bun.DB](resolutionContext, containerInstance, serviceName); nil != resolveErr {
            t.Fatalf("%s: resolve the handle: %v", serviceName, resolveErr)
        }

        cancel()
        _ = containerInstance.CloseWithContext(context.Background())

        if 1 != len(provider.openedUnder) {
            t.Fatalf("%s: expected one open, got %d", serviceName, len(provider.openedUnder))
        }

        if openDeadline, hasDeadline := provider.openedUnder[0].Deadline(); false == hasDeadline || false == expectedDeadline.Equal(openDeadline) {
            t.Fatalf("%s: expected the open under the resolution's deadline %v, got %v (%t)", serviceName, expectedDeadline, openDeadline, hasDeadline)
        }
    }
}
