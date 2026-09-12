package container

import (
    "errors"
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

/* TestContainer_Register_RefusesAnEmptyTeardownDependencyName pins the refusal an empty name gets: it cannot be an edge, and dropping it silently would report a teardown order that was never installed. */
func TestContainer_Register_RefusesAnEmptyTeardownDependencyName(t *testing.T) {
    serviceContainer := NewContainer()

    registerErr := serviceContainer.Register(
        "service.dependent",
        func(resolver containercontract.Resolver) (*registerProbeService, error) {
            return &registerProbeService{}, nil
        },
        WithTeardownDependency(""),
    )

    if false == errors.Is(registerErr, ErrTeardownDependencyNameIsRequired) {
        t.Fatalf("expected ErrTeardownDependencyNameIsRequired, got %v", registerErr)
    }

    /* the refusal leaves nothing behind: the registration is not half-installed */
    if true == serviceContainer.Has("service.dependent") {
        t.Fatalf("expected the refused registration to leave no provider behind")
    }
}

/* TestContainer_Register_RefusesATeardownDependencyOnItself pins the refusal a self-declaration gets. The teardown walk ignores a self-edge, so the declaration would be inert; it is refused where it is written rather than dropped where it is read. */
func TestContainer_Register_RefusesATeardownDependencyOnItself(t *testing.T) {
    serviceContainer := NewContainer()

    registerErr := serviceContainer.Register(
        "service.dependent",
        func(resolver containercontract.Resolver) (*registerProbeService, error) {
            return &registerProbeService{}, nil
        },
        WithTeardownDependency("service.dependent"),
    )

    if false == errors.Is(registerErr, ErrTeardownDependencyIsSelf) {
        t.Fatalf("expected ErrTeardownDependencyIsSelf, got %v", registerErr)
    }

    if true == serviceContainer.Has("service.dependent") {
        t.Fatalf("expected the refused registration to leave no provider behind")
    }
}

type registerProbeService struct{}
