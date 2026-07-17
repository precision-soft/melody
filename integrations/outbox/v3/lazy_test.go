package outbox

import (
    "context"
    "errors"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
)

func TestLazyRepository_DoesNotResolveAtConstruction(t *testing.T) {
    serviceContainer := container.NewContainer()

    container.MustRegister[*Store](
        serviceContainer,
        ServiceStore,
        func(resolver containercontract.Resolver) (*Store, error) {
            t.Fatalf("outbox store resolved during lazy repository construction")

            return nil, nil
        },
    )

    repository := NewLazyRepository(serviceContainer)
    if nil == repository {
        t.Fatalf("expected a repository handle")
    }
}

/** @info the relay drives the Repository from an error-driven backoff-and-retry loop, so a store that cannot be resolved must surface as the call's error — a panic would take the relay process down on a transient resolution outage — and a later successful resolution must proceed. */
func TestLazyRepository_ResolutionFailureSurfacesAsAnErrorAndIsRetried(t *testing.T) {
    serviceContainer := container.NewContainer()

    resolveCount := 0
    container.MustRegister[*Store](
        serviceContainer,
        ServiceStore,
        func(resolver containercontract.Resolver) (*Store, error) {
            resolveCount++

            return nil, errors.New("store database not reachable yet")
        },
    )

    repository := NewLazyRepository(serviceContainer)

    _, claimErr := repository.ClaimDueMessages(context.Background(), 1, time.Minute)
    if nil == claimErr {
        t.Fatal("expected the resolution failure to surface as the call's error")
    }

    if markErr := repository.MarkSent(context.Background(), 1, "claim-token"); nil == markErr {
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
