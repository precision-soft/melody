package config

import (
    "bytes"
    "context"
    "errors"
    "fmt"
    "reflect"
    "strings"
    "sync"
    "testing"
    "time"

    melodyrueidis "github.com/precision-soft/melody/integrations/rueidis/v3"
    "github.com/precision-soft/melody/v3/.example/generated"
    "github.com/precision-soft/melody/v3/.example/persistence"
    "github.com/precision-soft/melody/v3/.example/reporting"
    "github.com/precision-soft/melody/v3/.example/repository"
    "github.com/precision-soft/melody/v3/.example/subscriber"
    melodyclock "github.com/precision-soft/melody/v3/clock"
    melodyclockcontract "github.com/precision-soft/melody/v3/clock/contract"
    melodyconfig "github.com/precision-soft/melody/v3/config"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodylogging "github.com/precision-soft/melody/v3/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    rueidis "github.com/redis/rueidis"
    bun "github.com/uptrace/bun"
)

/* The catalog storage is the one door an ordinary http process passes through on its way to a repository: the generated wiring resolves it by type for every one of them. A provider that captures the handle the composition root already built resolves nothing, so the container records no dependency, the registry's SetLogger never runs and at SIGTERM neither the pool nor the registry is closed. Resolving is what writes the edge. */
func TestRegisterCatalogStorageService_ResolvesTheHandleRatherThanCapturingIt(t *testing.T) {
    containerInstance := melodycontainer.NewContainer()

    resolutions := 0
    containerInstance.MustRegister(
        serviceDatabase,
        func(resolver melodycontainercontract.Resolver) (*bun.DB, error) {
            resolutions++

            return newUndialedDatabase(), nil
        },
    )

    moduleInstance := moduleWithEnvironment(t, map[string]string{})
    moduleInstance.database = newUndialedDatabase()
    moduleInstance.registerCatalogStorageService(containerRegistrar{Container: containerInstance})

    storage, storageErr := melodycontainer.FromResolver[*persistence.CatalogStorage](containerInstance, persistence.ServiceCatalogStorage)
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
    containerInstance := melodycontainer.NewContainer()

    moduleInstance := moduleWithEnvironment(t, map[string]string{})
    moduleInstance.registerCatalogStorageService(containerRegistrar{Container: containerInstance})

    storage, storageErr := melodycontainer.FromResolver[*persistence.CatalogStorage](containerInstance, persistence.ServiceCatalogStorage)
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
        {melodyconfig.EnvDevelopment, true},
        {melodyconfig.EnvProduction, false},
    } {
        for _, database := range []*bun.DB{nil, newUndialedDatabase()} {
            containerInstance := melodycontainer.NewContainer()
            if nil != database {
                containerInstance.MustRegister(serviceDatabase, func(resolver melodycontainercontract.Resolver) (*bun.DB, error) {
                    return database, nil
                })
            }

            moduleInstance := moduleWithEnvironment(t, map[string]string{melodyconfig.EnvKey: environmentCase.environment})
            moduleInstance.database = database
            moduleInstance.registerCatalogStorageService(containerRegistrar{Container: containerInstance})

            storage, storageErr := melodycontainer.FromResolver[*persistence.CatalogStorage](containerInstance, persistence.ServiceCatalogStorage)
            if nil != storageErr {
                t.Fatalf("resolve catalog storage: %v", storageErr)
            }

            if environmentCase.seedsAccounts != storage.SeedsAccounts() {
                t.Fatalf("%s, database %v: expected SeedsAccounts %v, got %v", environmentCase.environment, nil != database, environmentCase.seedsAccounts, storage.SeedsAccounts())
            }
        }
    }
}

/* refusingBackplane refuses every publish, the failure the hub files */
type refusingBackplane struct{}

func (instance refusingBackplane) Publish(topic string, event melodyhttp.ServerSentEvent) error {
    return errors.New("the backplane is down")
}

func (instance refusingBackplane) Close() error {
    return nil
}

/* the hub files its own failures, and its provider is where it is handed the application's journal; without the swap a redis outage would silence cross-node delivery into a counter nobody reads. A publish the backplane refuses after the hub is resolved reaches the logger the container publishes. */
func TestRegisterServerSentEventHubService_HandsTheHubTheContainersJournal(t *testing.T) {
    /* read after Shutdown, which waits for the publishes in flight: the record is written before it returns */
    journal := &bytes.Buffer{}

    containerInstance := melodycontainer.NewContainer()
    containerInstance.MustRegister(
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return melodylogging.NewJsonLogger(journal, melodyloggingcontract.LevelDebug), nil
        },
    )

    moduleInstance := &Module{}
    moduleInstance.buildServerSentEvent()
    moduleInstance.registerServerSentEventHubService(containerRegistrar{Container: containerInstance})

    hub, hubErr := melodycontainer.FromResolver[*melodyhttp.ServerSentEventHub](containerInstance, subscriber.ServiceCatalogNotificationHub)
    if nil != hubErr || moduleInstance.serverSentEventHub != hub {
        t.Fatalf("expected the module's hub published, got %v, %v", hub, hubErr)
    }

    hub.SetBackplane(refusingBackplane{})
    hub.Broadcast("catalog", melodyhttp.ServerSentEvent{Event: "notification", Data: "changed"})
    hub.Shutdown()

    if false == strings.Contains(journal.String(), "server sent event backplane publish failed") {
        t.Fatalf("expected the refused publish in the container's journal, got %q", journal.String())
    }
}

/* without an archive the archive's storage is still published, carrying nothing — the generated wiring fills
   the archive repository by type — and it does not reach for the archive's handle, which is not registered */
func TestRegisterArchiveStorageService_PublishesAHandlelessStorageWithoutAnArchive(t *testing.T) {
    containerInstance := melodycontainer.NewContainer()

    moduleInstance := &Module{}
    moduleInstance.registerArchiveStorageService(containerRegistrar{Container: containerInstance})

    storage, storageErr := melodycontainer.FromResolver[*persistence.ArchiveStorage](containerInstance, persistence.ServiceArchiveStorage)
    if nil != storageErr {
        t.Fatalf("resolve the archive storage: %v", storageErr)
    }

    if true == storage.IsPersistent() {
        t.Fatal("expected an archive storage without a handle when no archive is wired")
    }
}

func TestGeneratedServices_ConcurrentScopesKeepRequestStateSeparate(t *testing.T) {
    serviceContainer := melodycontainer.NewContainer()
    defer func() {
        if closeErr := serviceContainer.Close(); nil != closeErr {
            t.Errorf("close: %v", closeErr)
        }
    }()

    generated.RegisterGeneratedServices(serviceContainer)
    generated.RegisterGeneratedServicesScoped(serviceContainer)
    melodycontainer.MustRegisterType(serviceContainer, func(melodycontainercontract.Resolver) (*persistence.CatalogStorage, error) {
        return persistence.NewCatalogStorage(nil), nil
    })
    melodycontainer.MustRegisterType(serviceContainer, func(melodycontainercontract.Resolver) (melodyclockcontract.Clock, error) {
        return melodyclock.NewSystemClock(), nil
    })
    melodycontainer.MustRegister(serviceContainer, "request.context", func(melodycontainercontract.Resolver) (*melodyhttp.RequestContext, error) {
        return melodyhttp.NewRequestContext("outside-request", time.Now()), nil
    })

    journal := repository.MustGetCatalogJournalRepository(serviceContainer)
    formatter := melodycontainer.MustFromResolverByType[*reporting.ReportFormatter](serviceContainer)

    start := make(chan struct{})
    var workers sync.WaitGroup
    for _, requestId := range []string{"request-alpha", "request-beta"} {
        requestScope := serviceContainer.NewScope()
        requestScope.MustOverrideProtectedInstance("request.context", melodyhttp.NewRequestContext(requestId, time.Now()))

        workers.Add(1)
        go func(requestId string, requestScope melodycontainercontract.Scope) {
            defer workers.Done()
            defer func() {
                if closeErr := requestScope.Close(); nil != closeErr {
                    t.Errorf("close the %s scope: %v", requestId, closeErr)
                }
            }()

            <-start

            trail, resolveErr := melodycontainer.FromResolver[*reporting.RequestReportTrail](requestScope, reporting.ServiceRequestReportTrail)
            if nil != resolveErr {
                t.Errorf("resolve the %s trail: %v", requestId, resolveErr)

                return
            }
            if requestId != trail.RequestId() {
                t.Errorf("expected the trail of %s, got %s", requestId, trail.RequestId())
            }
            if formatter != melodycontainer.MustFromResolverByType[*reporting.ReportFormatter](requestScope) {
                t.Errorf("expected the stateless formatter shared by %s", requestId)
            }
            if journal != repository.MustGetCatalogJournalRepository(requestScope) {
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
    containerInstance := melodycontainer.NewContainer()
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

/* stubRedisClient stands for a wired redis client; the hub's registration reads only whether one is there */
type stubRedisClient struct {
    rueidis.Client
}

type closeableConnectionStub struct{}

func (instance *closeableConnectionStub) Close() error {
    return nil
}

func hubTeardownDependencies(t *testing.T, redisWired bool) []string {
    t.Helper()

    containerInstance := melodycontainer.NewContainer()
    containerInstance.MustRegister(
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return melodylogging.NewNopLogger(), nil
        },
    )

    moduleInstance := &Module{}
    moduleInstance.buildServerSentEvent()

    if true == redisWired {
        moduleInstance.redisClient = stubRedisClient{}
        containerInstance.MustRegister(
            melodyrueidis.ServiceConnection,
            func(resolver melodycontainercontract.Resolver) (*closeableConnectionStub, error) {
                return &closeableConnectionStub{}, nil
            },
        )
        _ = melodycontainer.MustFromResolver[*closeableConnectionStub](containerInstance, melodyrueidis.ServiceConnection)
    }

    moduleInstance.registerServerSentEventHubService(containerRegistrar{Container: containerInstance})
    _ = melodycontainer.MustFromResolver[*melodyhttp.ServerSentEventHub](containerInstance, subscriber.ServiceCatalogNotificationHub)

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
