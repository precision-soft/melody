package config

import (
    "context"
    "fmt"
    "sync"
    "time"
    "testing"
    "github.com/precision-soft/melody/v3/.example/generated"
    "github.com/precision-soft/melody/v3/.example/reporting"
    "github.com/precision-soft/melody/v3/.example/repository"
    melodyclock "github.com/precision-soft/melody/v3/clock"
    melodyclockcontract "github.com/precision-soft/melody/v3/clock/contract"
    "github.com/precision-soft/melody/v3/.example/persistence"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    bun "github.com/uptrace/bun"
)

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

    moduleInstance := &Module{database: newUndialedDatabase()}
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

func TestRegisterCatalogStorageService_PublishesAHandlelessStorageWithoutADatabase(t *testing.T) {
    containerInstance := melodycontainer.NewContainer()

    moduleInstance := &Module{}
    moduleInstance.registerCatalogStorageService(containerRegistrar{Container: containerInstance})

    storage, storageErr := melodycontainer.FromResolver[*persistence.CatalogStorage](containerInstance, persistence.ServiceCatalogStorage)
    if nil != storageErr {
        t.Fatalf("resolve catalog storage: %v", storageErr)
    }

    if true == storage.IsPersistent() {
        t.Fatal("expected a storage without a handle when no database is configured")
    }
}

func TestServerSentEventHubShutdown_ClosesTheBackplaneItOwnsExactlyOnce(t *testing.T) {
    hub := melodyhttp.NewServerSentEventHub()
    backplane := &countingBackplane{hub: hub}
    hub.SetBackplane(backplane)

    hub.Shutdown()

    if 1 != backplane.closes {
        t.Fatalf("expected the hub to close its backplane exactly once, got %d", backplane.closes)
    }
}

func TestServerSentEventHubShutdown_ASecondCloserBesideItClosesAnAlreadyClosedBackplane(t *testing.T) {
    hub := melodyhttp.NewServerSentEventHub()
    backplane := &countingBackplane{hub: hub}
    hub.SetBackplane(backplane)

    hub.Shutdown()

    _ = backplane.Close()

    if 2 != backplane.closes {
        t.Fatalf("expected the second closer to close an already-closed backplane, got %d calls", backplane.closes)
    }
}

func TestGeneratedServices_ConcurrentScopesKeepRequestStateSeparate(t *testing.T) {
    serviceContainer := melodycontainer.NewContainer()
    defer serviceContainer.Close()
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
                if err := requestScope.Close(); nil != err {
                    t.Error(err)
                }
            }()
            <-start
            trail, err := melodycontainer.FromResolver[*reporting.RequestReportTrail](requestScope, reporting.ServiceRequestReportTrail)
            if nil != err {
                t.Error(err)
                return
            }
            if requestId != trail.RequestId() {
                t.Errorf("request identity leaked: want %s, got %s", requestId, trail.RequestId())
            }
            if formatter != melodycontainer.MustFromResolverByType[*reporting.ReportFormatter](requestScope) {
                t.Error("stateless formatter was not shared")
            }
            if journal != repository.MustGetCatalogJournalRepository(requestScope) {
                t.Error("process journal was not shared")
            }
            for entryIndex := 0; entryIndex < 20; entryIndex++ {
                trail.Record(requestId, repository.CatalogJournalActionCreated, "product", fmt.Sprintf("%s-%d", requestId, entryIndex))
            }
        }(requestId, requestScope)
    }
    close(start)
    workers.Wait()

    entries, err := journal.Latest(context.Background(), 100)
    if nil != err {
        t.Fatal(err)
    }
    counts := map[string]int{}
    for _, entry := range entries {
        if entry.Actor != entry.RequestId {
            t.Errorf("actor %s was attributed to request %s", entry.Actor, entry.RequestId)
        }
        counts[entry.RequestId]++
    }
    if 20 != counts["request-alpha"] || 20 != counts["request-beta"] || 40 != len(entries) {
        t.Fatalf("request journals leaked or lost entries: %v, total=%d", counts, len(entries))
    }
}
