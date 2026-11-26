package config

import (
    "bytes"
    "context"
    "database/sql"
    "errors"
    "fmt"
    "os"
    "reflect"
    "slices"
    "strings"
    "sync"
    "testing"
    "time"

    melodyrueidis "github.com/precision-soft/melody/integrations/rueidis/v4"
    "github.com/precision-soft/melody/v4/.example/generated"
    "github.com/precision-soft/melody/v4/.example/persistence"
    "github.com/precision-soft/melody/v4/.example/reporting"
    "github.com/precision-soft/melody/v4/.example/repository"
    "github.com/precision-soft/melody/v4/.example/subscriber"
    melodyclock "github.com/precision-soft/melody/v4/clock"
    melodyclockcontract "github.com/precision-soft/melody/v4/clock/contract"
    melodyconfig "github.com/precision-soft/melody/v4/config"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    melodylogging "github.com/precision-soft/melody/v4/logging"
    melodymessagebus "github.com/precision-soft/melody/v4/messagebus"
    melodyloggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    bun "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/mysqldialect"
)

/* The catalog storage is the one door an ordinary http process passes through on its way to a repository: the generated wiring resolves it by type for every one of them. A provider that captures the handle the composition root already built resolves nothing, so the container records no dependency, the registry's SetLogger never runs and at SIGTERM neither the pool nor the registry is closed. Resolving is what writes the edge. */
func TestRegisterCatalogStorageService_ResolvesTheHandleRatherThanCapturingIt(t *testing.T) {
    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})

    resolutions := 0
    containerInstance.MustRegister(
        serviceDatabase,
        func(resolver melodycontainercontract.Resolver) (*bun.DB, error) {
            resolutions++

            return newUndialedDatabase(), nil
        },
        melodycontainer.RegisterOptions{Closer: registryOwnedDatabaseCloser},
    )

    moduleInstance := moduleWithEnvironment(t, map[string]string{})
    moduleInstance.catalogueWired = true
    moduleInstance.registerCatalogStorageService(containerRegistrar{Container: containerInstance})

    storage, storageErr := melodycontainer.FromResolver[*persistence.CatalogStorage](t.Context(), containerInstance, persistence.ServiceCatalogStorage)
    if nil != storageErr {
        t.Fatalf("resolve catalog storage: %v", storageErr)
    }

    if 1 != resolutions {
        t.Fatalf("expected the storage provider to resolve the handle service exactly once, it resolved it %d times", resolutions)
    }

    if false == storage.IsPersistent() {
        t.Fatal("expected the resolved storage to carry a handle")
    }
}

/* The sister case the gate exists for: without a configured database the handle service is never registered,
   so a provider that resolved unconditionally would fail here and take the whole nomenclature with it. The
   storage is still published, carrying nothing. */
func TestRegisterCatalogStorageService_PublishesAHandlelessStorageWithoutADatabase(t *testing.T) {
    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})

    moduleInstance := moduleWithEnvironment(t, map[string]string{})
    moduleInstance.registerCatalogStorageService(containerRegistrar{Container: containerInstance})

    storage, storageErr := melodycontainer.FromResolver[*persistence.CatalogStorage](t.Context(), containerInstance, persistence.ServiceCatalogStorage)
    if nil != storageErr {
        t.Fatalf("resolve catalog storage: %v", storageErr)
    }

    if true == storage.IsPersistent() {
        t.Fatal("expected a storage without a handle when no database is configured")
    }
}

/* the example's accounts sign in with their names as passwords, so only development seeds them: the handle the composition root publishes is marked in development, with or without a database, and unmarked anywhere else */
func TestRegisterCatalogStorageService_SeedsTheAccountsInDevelopmentOnly(t *testing.T) {
    for _, environmentCase := range []struct {
        environment   string
        seedsAccounts bool
    }{
        {melodyconfig.EnvironmentDevelopment, true},
        {melodyconfig.EnvironmentProduction, false},
    } {
        for _, database := range []*bun.DB{nil, newUndialedDatabase()} {
            containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
            if nil != database {
                containerInstance.MustRegister(serviceDatabase, func(resolver melodycontainercontract.Resolver) (*bun.DB, error) {
                    return database, nil
                }, melodycontainer.RegisterOptions{Closer: registryOwnedDatabaseCloser})
            }

            moduleInstance := moduleWithEnvironment(t, map[string]string{melodyconfig.EnvironmentNameKey: environmentCase.environment})
            moduleInstance.catalogueWired = nil != database
            moduleInstance.registerCatalogStorageService(containerRegistrar{Container: containerInstance})

            storage, storageErr := melodycontainer.FromResolver[*persistence.CatalogStorage](t.Context(), containerInstance, persistence.ServiceCatalogStorage)
            if nil != storageErr {
                t.Fatalf("resolve catalog storage: %v", storageErr)
            }

            if environmentCase.seedsAccounts != storage.SeedsAccounts() {
                t.Fatalf("%s, database %v: expected SeedsAccounts %v, got %v", environmentCase.environment, nil != database, environmentCase.seedsAccounts, storage.SeedsAccounts())
            }
        }
    }
}

/* the catalogue schema is built at boot with the warning mode, and a database that does not answer answers the schema's failure, so an http boot goes on with a warning */
func TestRegisterCatalogSchemaService_IsBuiltAtBootWarnsAndAnswersACatalogueItCannotPrepare(t *testing.T) {
    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    containerInstance.MustRegister(serviceDatabase, func(resolver melodycontainercontract.Resolver) (*bun.DB, error) {
        return newUndialedDatabase(), nil
    }, melodycontainer.RegisterOptions{Closer: registryOwnedDatabaseCloser})

    moduleInstance := moduleWithEnvironment(t, map[string]string{})
    moduleInstance.catalogueWired = true
    moduleInstance.registerCatalogStorageService(containerRegistrar{Container: containerInstance})
    moduleInstance.registerCatalogSchemaService(containerRegistrar{Container: containerInstance})

    builtAtBoot, modes := builtAtBootNames(containerInstance)
    if false == slices.Contains(builtAtBoot, persistence.ServiceCatalogSchema) || melodycontainer.BootFailureWarns != modes[persistence.ServiceCatalogSchema] {
        t.Fatalf("expected the catalogue schema built at boot with the warning mode, got %v %v", builtAtBoot, modes)
    }

    if _, schemaErr := melodycontainer.FromResolver[*persistence.CatalogSchema](t.Context(), containerInstance, persistence.ServiceCatalogSchema); nil == schemaErr {
        t.Fatal("expected the schema's failure answered")
    }
}

type callerContextMarker struct{}

/* seedContextHook records every seed insert that ran on a context other than the caller's */
type seedContextHook struct {
    mutex   sync.Mutex
    foreign []string
}

func (instance *seedContextHook) BeforeQuery(ctx context.Context, event *bun.QueryEvent) context.Context {
    if true == strings.HasPrefix(event.Query, "INSERT INTO `melody_example_v4_") && nil == ctx.Value(callerContextMarker{}) {
        instance.mutex.Lock()
        instance.foreign = append(instance.foreign, event.Query)
        instance.mutex.Unlock()
    }

    return ctx
}

func (instance *seedContextHook) AfterQuery(ctx context.Context, event *bun.QueryEvent) {}

/* seededCatalogueTables are the nomenclatures the catalogue schema seeds whatever the environment; the accounts are seeded in development alone */
var seededCatalogueTables = []string{"melody_example_v4_category", "melody_example_v4_currency", "melody_example_v4_product"}

/* the heal: a database that did not answer at the first resolution is reached by the next one, which prepares the catalogue, the storage built anew since a failed creation leaves nothing behind */
func TestRegisterCatalogSchemaService_TheNextResolutionAfterTheDatabaseAnswersPreparesTheCatalogue(t *testing.T) {
    dsn := os.Getenv("MYSQL_DSN")
    if "" == dsn {
        t.Skip("MYSQL_DSN not set; skipping the catalogue schema heal on MySQL")
    }

    sqlDatabase, openErr := sql.Open("mysql", dsn+"?parseTime=true")
    if nil != openErr {
        t.Fatalf("open: %v", openErr)
    }
    t.Cleanup(func() { _ = sqlDatabase.Close() })

    database := bun.NewDB(sqlDatabase, mysqldialect.New())
    hook := &seedContextHook{}
    database.AddQueryHook(hook)

    /* the fourth major's own trail table is dropped and its seeded nomenclatures emptied, so the heal has to create and seed them again */
    if _, dropErr := database.NewRaw("DROP TABLE IF EXISTS " + persistence.AuditTable).Exec(t.Context()); nil != dropErr {
        t.Fatalf("drop the trail table: %v", dropErr)
    }

    /* emptied in the reverse of the seeding order, since a product refers to its category and its currency */
    for index := len(seededCatalogueTables) - 1; 0 <= index; index-- {
        table := seededCatalogueTables[index]
        if _, deleteErr := database.NewRaw("DELETE FROM " + table).Exec(t.Context()); nil != deleteErr && false == strings.Contains(deleteErr.Error(), "doesn't exist") {
            t.Fatalf("empty %s: %v", table, deleteErr)
        }
    }

    answers := false
    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    containerInstance.MustRegister(serviceDatabase, func(resolver melodycontainercontract.Resolver) (*bun.DB, error) {
        if false == answers {
            return nil, errors.New("the database did not answer")
        }

        return database, nil
    }, melodycontainer.RegisterOptions{Closer: registryOwnedDatabaseCloser})

    moduleInstance := moduleWithEnvironment(t, map[string]string{})
    moduleInstance.catalogueWired = true
    moduleInstance.registerCatalogStorageService(containerRegistrar{Container: containerInstance})
    moduleInstance.registerCatalogSchemaService(containerRegistrar{Container: containerInstance})

    if _, schemaErr := melodycontainer.FromResolver[*persistence.CatalogSchema](t.Context(), containerInstance, persistence.ServiceCatalogSchema); nil == schemaErr {
        t.Fatal("expected the schema refused while the database does not answer")
    }

    answers = true

    callerContext := context.WithValue(t.Context(), callerContextMarker{}, true)
    if _, schemaErr := melodycontainer.FromResolver[*persistence.CatalogSchema](callerContext, containerInstance, persistence.ServiceCatalogSchema); nil != schemaErr {
        t.Fatalf("expected the next resolution to prepare the catalogue, got %v", schemaErr)
    }

    if 0 != len(hook.foreign) {
        t.Fatalf("expected every seed written on the resolving caller's context, got %v", hook.foreign)
    }

    for _, table := range seededCatalogueTables {
        rowCount, countErr := database.NewSelect().Table(table).Count(t.Context())
        if nil != countErr || 0 == rowCount {
            t.Fatalf("expected %s seeded, got %d rows: %v", table, rowCount, countErr)
        }
    }

    trailTables := 0
    if scanErr := database.NewRaw("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ?", persistence.AuditTable).Scan(t.Context(), &trailTables); nil != scanErr || 1 != trailTables {
        t.Fatalf("expected the trail table created again, got %d: %v", trailTables, scanErr)
    }
}

/* the module's own registration builds the catalogue schema at boot with the warning mode */
func TestModuleRegisterServices_BuildsTheCatalogueSchemaAtBootAndWarns(t *testing.T) {
    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    moduleInstance := moduleWithEnvironment(t, map[string]string{})
    moduleInstance.messageBusTransport = melodymessagebus.NewInMemoryTransport(melodymessagebus.InMemoryTransportConfig{})
    moduleInstance.RegisterServices(containerRegistrar{Container: containerInstance})

    builtAtBoot, modes := builtAtBootNames(containerInstance)
    if false == slices.Contains(builtAtBoot, persistence.ServiceCatalogSchema) || melodycontainer.BootFailureWarns != modes[persistence.ServiceCatalogSchema] {
        t.Fatalf("expected the catalogue schema built at boot with the warning mode, got %v %v", builtAtBoot, modes)
    }
}

/* without a database there is nothing to prepare: the schema is published without a statement */
func TestRegisterCatalogSchemaService_PublishesTheSchemaWithoutADatabase(t *testing.T) {
    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})

    moduleInstance := moduleWithEnvironment(t, map[string]string{})
    moduleInstance.registerCatalogStorageService(containerRegistrar{Container: containerInstance})
    moduleInstance.registerCatalogSchemaService(containerRegistrar{Container: containerInstance})

    schema, schemaErr := melodycontainer.FromResolver[*persistence.CatalogSchema](t.Context(), containerInstance, persistence.ServiceCatalogSchema)
    if nil != schemaErr || nil == schema {
        t.Fatalf("expected the schema published without a database, got %v", schemaErr)
    }
}

/* refusingBackplane refuses every publish, the failure the hub files */
type refusingBackplane struct{}

func (instance refusingBackplane) Publish(topic string, event melodyhttp.ServerSentEvent) error {
    return errors.New("the backplane is down")
}

func (instance refusingBackplane) CloseWithContext(closeContext context.Context) error {
    return nil
}

/* the hub files its own failures, and its provider builds it with the application's journal; without it a redis outage would silence cross-node delivery into a counter nobody reads. A publish the backplane refuses after the hub is resolved reaches the logger the container publishes. */
func TestHub_IsBuiltInItsProviderWithTheApplicationsLogger(t *testing.T) {
    /* read after the close, which waits for the publishes in flight: the record is written before it returns */
    journal := &bytes.Buffer{}

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    containerInstance.MustRegister(
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return melodylogging.NewJsonLogger(journal, melodylogging.JsonLoggerConfig{MinLevel: melodyloggingcontract.LevelDebug}), nil
        },
        melodycontainer.RegisterOptions{},
    )

    moduleInstance := &Module{}
    moduleInstance.registerServerSentEventHubService(containerRegistrar{Container: containerInstance})

    if nil != moduleInstance.builtServerSentEventHub.Load() {
        t.Fatal("expected no hub before its provider ran")
    }

    hub, hubErr := melodycontainer.FromResolver[*melodyhttp.ServerSentEventHub](t.Context(), containerInstance, subscriber.ServiceCatalogNotificationHub)
    if nil != hubErr || nil == hub || moduleInstance.builtServerSentEventHub.Load() != hub {
        t.Fatalf("expected the provider's hub published and handed to the shutdown hook, got %v, %v", hub, hubErr)
    }

    hub.SetBackplane(refusingBackplane{})
    hub.Broadcast("catalog", melodyhttp.ServerSentEvent{Event: "notification", Data: "changed"})
    _ = hub.CloseWithContext(t.Context())

    if false == strings.Contains(journal.String(), "server sent event backplane publish failed") {
        t.Fatalf("expected the refused publish in the container's journal, got %q", journal.String())
    }
}

/* without an archive the archive's storage is still published, carrying nothing — the generated wiring fills
   the archive repository by type — and it does not reach for the archive's handle, which is not registered */
func TestRegisterArchiveStorageService_PublishesAHandlelessStorageWithoutAnArchive(t *testing.T) {
    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})

    moduleInstance := &Module{}
    moduleInstance.registerArchiveStorageService(containerRegistrar{Container: containerInstance})

    storage, storageErr := melodycontainer.FromResolver[*persistence.ArchiveStorage](t.Context(), containerInstance, persistence.ServiceArchiveStorage)
    if nil != storageErr {
        t.Fatalf("resolve the archive storage: %v", storageErr)
    }

    if true == storage.IsPersistent() {
        t.Fatal("expected an archive storage without a handle when no archive is wired")
    }
}

func TestGeneratedServices_ConcurrentScopesKeepRequestStateSeparate(t *testing.T) {
    serviceContainer := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    defer func() {
        if closeErr := serviceContainer.CloseWithContext(context.Background()); nil != closeErr {
            t.Errorf("close: %v", closeErr)
        }
    }()

    generated.RegisterGeneratedServices(serviceContainer)
    generated.RegisterGeneratedServicesScoped(serviceContainer)
    registerCatalogJournalSourceService(serviceContainer)
    melodycontainer.MustRegisterType(serviceContainer, func(melodycontainercontract.Resolver) (*persistence.CatalogStorage, error) {
        return persistence.NewCatalogStorage(nil), nil
    }, melodycontainer.RegisterOptions{})
    melodycontainer.MustRegisterType(serviceContainer, func(melodycontainercontract.Resolver) (*persistence.CatalogSchema, error) {
        return &persistence.CatalogSchema{}, nil
    }, melodycontainer.RegisterOptions{})
    melodycontainer.MustRegisterType(serviceContainer, func(melodycontainercontract.Resolver) (melodyclockcontract.Clock, error) {
        return melodyclock.NewSystemClock(), nil
    }, melodycontainer.RegisterOptions{})
    melodycontainer.MustRegister(serviceContainer, "request.context", func(melodycontainercontract.Resolver) (*melodyhttp.RequestContext, error) {
        return melodyhttp.NewRequestContext("outside-request", time.Now()), nil
    }, melodycontainer.RegisterOptions{})

    journal := repository.MustGetCatalogJournalRepository(t.Context(), serviceContainer)
    formatter := melodycontainer.MustFromResolverByType[*reporting.ReportFormatter](t.Context(), serviceContainer)

    start := make(chan struct{})
    var workers sync.WaitGroup
    for _, requestId := range []string{"request-alpha", "request-beta"} {
        requestScope := serviceContainer.NewScope()
        requestScope.MustOverrideProtectedInstance("request.context", melodyhttp.NewRequestContext(requestId, time.Now()), melodycontainer.OverrideOptions{})

        workers.Add(1)
        go func(requestId string, requestScope melodycontainercontract.Scope) {
            defer workers.Done()
            defer func() {
                if closeErr := requestScope.CloseWithContext(context.Background()); nil != closeErr {
                    t.Errorf("close the %s scope: %v", requestId, closeErr)
                }
            }()

            <-start

            trail, resolveErr := melodycontainer.FromResolver[*reporting.RequestReportTrail](t.Context(), requestScope, reporting.ServiceRequestReportTrail)
            if nil != resolveErr {
                t.Errorf("resolve the %s trail: %v", requestId, resolveErr)

                return
            }
            if requestId != trail.RequestId() {
                t.Errorf("expected the trail of %s, got %s", requestId, trail.RequestId())
            }
            if formatter != melodycontainer.MustFromResolverByType[*reporting.ReportFormatter](t.Context(), requestScope) {
                t.Errorf("expected the stateless formatter shared by %s", requestId)
            }
            if journal != repository.MustGetCatalogJournalRepository(t.Context(), requestScope) {
                t.Errorf("expected the process journal shared by %s", requestId)
            }

            for entryIndex := 0; entryIndex < 20; entryIndex++ {
                trail.Record(requestId, repository.CatalogJournalActionCreated, "product", fmt.Sprintf("%s-%d", requestId, entryIndex))
            }
        }(requestId, requestScope)
    }
    close(start)
    workers.Wait()

    entries, latestErr := journal.Latest(context.Background(), 100)
    if nil != latestErr {
        t.Fatalf("read the journal: %v", latestErr)
    }

    counts := map[string]int{}
    for _, entry := range entries {
        if entry.Actor != entry.RequestId {
            t.Errorf("expected the entry of %s filed under its own request, got %s", entry.Actor, entry.RequestId)
        }
        counts[entry.RequestId]++
    }

    if 20 != counts["request-alpha"] || 20 != counts["request-beta"] || 40 != len(entries) {
        t.Fatalf("expected twenty entries per request and forty in all, got %v and %d", counts, len(entries))
    }
}

func TestRegisterSeeders_CollectsTheSeedersInTheOrderTheirReferencesNeed(t *testing.T) {
    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    repository.RegisterSeeders(containerInstance)

    typeLister, isTypeLister := any(containerInstance).(melodycontainercontract.TypeLister)
    if false == isTypeLister {
        t.Fatal("expected the container to enumerate its registered types")
    }

    references := typeLister.ReferencesImplementing(reflect.TypeOf((*repository.Seeder)(nil)).Elem())

    collected := make([]string, 0, len(references))
    for _, reference := range references {
        collected = append(collected, reference.ServiceName)
    }

    expected := []string{
        repository.ServiceCategorySeeder,
        repository.ServiceCurrencySeeder,
        repository.ServiceProductSeeder,
        repository.ServiceUserSeeder,
    }

    if strings.Join(expected, ",") != strings.Join(collected, ",") {
        t.Fatalf("expected the seeders collected as %v, got %v", expected, collected)
    }
}

func hubTeardownDependencies(t *testing.T, redisWired bool) []string {
    t.Helper()

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    containerInstance.MustRegister(
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return melodylogging.NewNopLogger(), nil
        },
        melodycontainer.RegisterOptions{},
    )

    t.Cleanup(func() { _ = containerInstance.CloseWithContext(context.Background()) })

    moduleInstance := &Module{}

    if true == redisWired {
        client := redisTestClient(t)
        moduleInstance.redisProvider = melodyrueidis.NewProvider("melody-example-v4", melodyrueidis.ProviderConfig{})
        containerInstance.MustRegister(
            melodyrueidis.ServiceConnection,
            func(resolver melodycontainercontract.Resolver) (*melodyrueidis.Connection, error) {
                return melodyrueidis.NewConnection(client, "melody-example-v4"), nil
            },
            melodycontainer.RegisterOptions{},
        )
    }

    moduleInstance.registerServerSentEventHubService(containerRegistrar{Container: containerInstance})
    _ = melodycontainer.MustFromResolver[*melodyhttp.ServerSentEventHub](t.Context(), containerInstance, subscriber.ServiceCatalogNotificationHub)

    planner, isPlanner := any(containerInstance).(interface {
        TeardownPlan() []melodycontainercontract.TeardownPlanEntry
    })
    if false == isPlanner {
        t.Fatal("expected the container to answer its teardown plan")
    }

    for _, entry := range planner.TeardownPlan() {
        if "service:"+subscriber.ServiceCatalogNotificationHub == entry.NodeKey {
            return entry.Dependencies
        }
    }

    t.Fatal("expected the hub in the teardown plan")

    return nil
}

func TestRegisterServerSentEventHubService_ClosesTheHubBeforeTheRedisConnectionItsBackplaneCaptured(t *testing.T) {
    dependencies := hubTeardownDependencies(t, true)

    for _, dependency := range dependencies {
        if "service:"+melodyrueidis.ServiceConnection == dependency {
            return
        }
    }

    t.Fatalf("expected the hub closed before the redis connection, got the dependencies %v", dependencies)
}

func TestRegisterServerSentEventHubService_DeclaresNoRedisEdgeWithoutRedis(t *testing.T) {
    for _, dependency := range hubTeardownDependencies(t, false) {
        if "service:"+melodyrueidis.ServiceConnection == dependency {
            t.Fatalf("expected no edge to a redis connection that is not wired, got %v", dependency)
        }
    }
}
