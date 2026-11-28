package outbox

import (
    "context"
    "errors"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
)

func TestLazyRepository_DoesNotResolveAtConstruction(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    container.MustRegister[*Store](
        serviceContainer,
        ServiceStore,
        func(resolver containercontract.Resolver) (*Store, error) {
            t.Fatalf("outbox store resolved during lazy repository construction")

            return nil, nil
        },
        container.RegisterOptions{},
    )

    repository := NewLazyRepository(serviceContainer)
    if nil == repository {
        t.Fatalf("expected a repository handle")
    }
}

func TestLazyRepository_ResolutionFailureSurfacesAsAnErrorAndIsRetried(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    resolveCount := 0
    container.MustRegister[*Store](
        serviceContainer,
        ServiceStore,
        func(resolver containercontract.Resolver) (*Store, error) {
            resolveCount++

            return nil, errors.New("store database not reachable yet")
        },
        container.RegisterOptions{},
    )

    repository := NewLazyRepository(serviceContainer)

    _, claimErr := repository.ClaimDueMessages(outboxTestRuntime(context.Background()), 1, time.Minute, ClaimOptions{})
    if nil == claimErr {
        t.Fatal("expected the resolution failure to surface as the call's error")
    }

    if markErr := repository.MarkSent(outboxTestRuntime(context.Background()), 1, "claim-token"); nil == markErr {
        t.Fatal("expected the retried resolution failure to surface as the call's error")
    }

    if 2 != resolveCount {
        t.Fatalf("expected each call to retry the resolution, got %d resolutions", resolveCount)
    }
}

func TestLazyRepository_NilResolverPanicsAtConstruction(t *testing.T) {
    defer func() {
        if recovered := recover(); nil == recovered {
            t.Fatal("expected a panic for a nil resolver")
        }
    }()

    NewLazyRepository(nil)
}

type lazyRepositoryContextKey struct{}

func TestLazyRepository_ResolvesTheStoreUnderTheDoorsContext(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    var seen any
    container.MustRegister[*Store](serviceContainer, ServiceStore, func(resolver containercontract.Resolver) (*Store, error) {
        seen = resolver.Runtime().Context().Value(lazyRepositoryContextKey{})

        return nil, errors.New("no database in this test")
    }, container.RegisterOptions{})

    repository := NewLazyRepository(serviceContainer)
    doorContext := context.WithValue(t.Context(), lazyRepositoryContextKey{}, "door")
    if _, claimErr := repository.ClaimDueMessages(outboxTestRuntime(doorContext), 1, time.Second, ClaimOptions{}); nil == claimErr {
        t.Fatalf("expected the failing store to fail the door")
    }

    if "door" != seen {
        t.Fatalf("the store's provider read %v, wanted the door's context", seen)
    }

    if _, nilErr := repository.ClaimDueMessages(nil, 1, time.Second, ClaimOptions{}); nil == nilErr || false == strings.Contains(nilErr.Error(), "the runtime is nil or carries no context") {
        t.Fatalf("expected a nil runtime refused as an error, got %v", nilErr)
    }
}

func TestLazyRepository_ForwardsTheMaintenanceDoorsAndAnswersTheResolutionError(t *testing.T) {
    storeDown := errors.New("store database not reachable yet")
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    container.MustRegister[*Store](serviceContainer, ServiceStore, func(resolver containercontract.Resolver) (*Store, error) {
        return nil, storeDown
    }, container.RegisterOptions{})

    repository := NewLazyRepository(serviceContainer)
    runtimeInstance := outboxTestRuntime(t.Context())

    if _, releaseErr := repository.ReleaseClaims(runtimeInstance, []int64{1}, "claim-token"); false == exception.ChainIs(releaseErr, storeDown) {
        t.Fatalf("expected ReleaseClaims to answer the resolution error, got %v", releaseErr)
    }

    if _, requeueErr := repository.RequeueDead(runtimeInstance, RequeueDeadOptions{}); false == exception.ChainIs(requeueErr, storeDown) {
        t.Fatalf("expected RequeueDead to answer the resolution error, got %v", requeueErr)
    }

    if _, pruneErr := repository.PruneSent(runtimeInstance, PruneSentOptions{SentBefore: time.Now()}); false == exception.ChainIs(pruneErr, storeDown) {
        t.Fatalf("expected PruneSent to answer the resolution error, got %v", pruneErr)
    }
}

func TestLazyRepository_ForwardsEachMaintenanceDoorToTheStoresOwn(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    container.MustRegister[*Store](serviceContainer, ServiceStore, func(resolver containercontract.Resolver) (*Store, error) {
        return &Store{}, nil
    }, container.RegisterOptions{})

    repository := NewLazyRepository(serviceContainer)
    runtimeInstance := outboxTestRuntime(t.Context())

    if _, pruneErr := repository.PruneSent(runtimeInstance, PruneSentOptions{}); nil == pruneErr {
        t.Fatal("expected the store's zero cut-off refusal through the lazy door")
    } else {
        expectErrorNamingField(t, pruneErr, "PruneSentOptions.SentBefore")
    }

    if _, requeueErr := repository.RequeueDead(runtimeInstance, RequeueDeadOptions{Limit: -1}); nil == requeueErr || false == strings.Contains(requeueErr.Error(), "limit must be positive") {
        t.Fatalf("expected the store's limit refusal through the lazy door, got %v", requeueErr)
    }
}

func TestLazyRepository_ReleaseClaimsReachesTheStore(t *testing.T) {
    store := outboxTestStore(t)
    enqueueOutboxRows(t, store, 1)

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    container.MustRegister[*Store](serviceContainer, ServiceStore, func(resolver containercontract.Resolver) (*Store, error) {
        return store, nil
    }, container.RegisterOptions{})

    runtimeInstance := outboxTestRuntime(t.Context())
    claimed, claimErr := store.ClaimDueMessages(runtimeInstance, 1, time.Hour, ClaimOptions{})
    if nil != claimErr || 1 != len(claimed) {
        t.Fatalf("claim: %v (got %d)", claimErr, len(claimed))
    }

    released, releaseErr := NewLazyRepository(serviceContainer).ReleaseClaims(runtimeInstance, []int64{claimed[0].Id}, claimed[0].ClaimToken)
    if nil != releaseErr || 1 != released {
        t.Fatalf("expected the lazy door to release the claimed row, got %d: %v", released, releaseErr)
    }
}
