package container

import (
    "testing"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
    alpha "github.com/precision-soft/melody/v3/container/internal/collisionalpha/contract"
    beta "github.com/precision-soft/melody/v3/container/internal/collisionbeta/contract"
)

func TestContainer_RegisterType_AndResolveByType(t *testing.T) {
    serviceContainer := NewContainer()

    err := RegisterType[*testService](
        serviceContainer,
        func(resolver containercontract.Resolver) (*testService, error) {
            return &testService{Value: "typed"}, nil
        },
    )
    if nil != err {
        t.Fatalf("unexpected error")
    }

    service := MustFromResolverByType[*testService](serviceContainer)
    if "typed" != service.Value {
        t.Fatalf("unexpected value")
    }
}

func TestContainer_RegisterType_Interface_AndResolveByType(t *testing.T) {
    serviceContainer := NewContainer()

    err := RegisterType[testInterface](
        serviceContainer,
        func(resolver containercontract.Resolver) (testInterface, error) {
            return &testImplementation{name: "impl"}, nil
        },
    )
    if nil != err {
        t.Fatalf("unexpected error")
    }

    value := MustFromResolverByType[testInterface](serviceContainer)
    if "impl" != value.Name() {
        t.Fatalf("unexpected name")
    }
}

/* both same-string types can now be type-registered under their auto-derived names: the name is import-path-qualified, so "contract.Bus" from two packages no longer collides at registration */
func TestRegisterType_SameStringTypesFromDifferentPackagesGetDistinctNames(t *testing.T) {
    serviceContainer := NewContainer()

    MustRegisterType(serviceContainer, func(resolver containercontract.Resolver) (*alpha.Bus, error) {
        return &alpha.Bus{Region: "alpha"}, nil
    })

    MustRegisterType(serviceContainer, func(resolver containercontract.Resolver) (*beta.Bus, error) {
        return &beta.Bus{Region: "beta"}, nil
    })

    alphaBus, alphaErr := FromResolverByType[*alpha.Bus](serviceContainer)
    if nil != alphaErr {
        t.Fatalf("expected the alpha bus to resolve, got %v", alphaErr)
    }

    betaBus, betaErr := FromResolverByType[*beta.Bus](serviceContainer)
    if nil != betaErr {
        t.Fatalf("expected the beta bus to resolve, got %v", betaErr)
    }

    if "alpha" != alphaBus.Region || "beta" != betaBus.Region {
        t.Fatalf("expected each auto-named registration to keep its own value, got %q and %q", alphaBus.Region, betaBus.Region)
    }
}
