package outbox

import (
    "testing"

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
