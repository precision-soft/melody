package container

import (
    "context"
    "errors"
    "reflect"
    "testing"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    alpha "github.com/precision-soft/melody/v4/container/internal/collisionalpha/contract"
    beta "github.com/precision-soft/melody/v4/container/internal/collisionbeta/contract"
)

func TestContainer_RegisterType_AndResolveByType(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    err := RegisterType[*testService](
        serviceContainer,
        func(resolver containercontract.Resolver) (*testService, error) {
            return &testService{Value: "typed"}, nil
        },
        RegisterOptions{},
    )
    if nil != err {
        t.Fatalf("unexpected error")
    }

    service := MustFromResolverByType[*testService](t.Context(), serviceContainer)
    if "typed" != service.Value {
        t.Fatalf("unexpected value")
    }
}

func TestContainer_RegisterType_Interface_AndResolveByType(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    err := RegisterType[testInterface](
        serviceContainer,
        func(resolver containercontract.Resolver) (testInterface, error) {
            return &testImplementation{name: "impl"}, nil
        },
        RegisterOptions{},
    )
    if nil != err {
        t.Fatalf("unexpected error")
    }

    value := MustFromResolverByType[testInterface](t.Context(), serviceContainer)
    if "impl" != value.Name() {
        t.Fatalf("unexpected name")
    }
}

func TestRegisterType_SameStringTypesFromDifferentPackagesGetDistinctNames(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    MustRegisterType(serviceContainer, func(resolver containercontract.Resolver) (*alpha.Bus, error) {
        return &alpha.Bus{Region: "alpha"}, nil
    }, RegisterOptions{})

    MustRegisterType(serviceContainer, func(resolver containercontract.Resolver) (*beta.Bus, error) {
        return &beta.Bus{Region: "beta"}, nil
    }, RegisterOptions{})

    alphaBus, alphaErr := FromResolverByType[*alpha.Bus](t.Context(), serviceContainer)
    if nil != alphaErr {
        t.Fatalf("expected the alpha bus to resolve, got %v", alphaErr)
    }

    betaBus, betaErr := FromResolverByType[*beta.Bus](t.Context(), serviceContainer)
    if nil != betaErr {
        t.Fatalf("expected the beta bus to resolve, got %v", betaErr)
    }

    if "alpha" != alphaBus.Region || "beta" != betaBus.Region {
        t.Fatalf("expected each auto-named registration to keep its own value, got %q and %q", alphaBus.Region, betaBus.Region)
    }
}

func TestContainer_Register_RefusesAnEmptyTeardownDependencyName(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    registerErr := serviceContainer.Register(
        "service.dependent",
        func(resolver containercontract.Resolver) (*registerProbeService, error) {
            return &registerProbeService{}, nil
        },
        RegisterOptions{TeardownDependencyNames: []string{""}},
    )

    if false == errors.Is(registerErr, ErrTeardownDependencyNameIsRequired) {
        t.Fatalf("expected ErrTeardownDependencyNameIsRequired, got %v", registerErr)
    }

    if true == serviceContainer.Has("service.dependent") {
        t.Fatalf("expected the refused registration to leave no provider behind")
    }
}

func TestContainer_Register_RefusesATeardownDependencyOnItself(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    registerErr := serviceContainer.Register(
        "service.dependent",
        func(resolver containercontract.Resolver) (*registerProbeService, error) {
            return &registerProbeService{}, nil
        },
        RegisterOptions{TeardownDependencyNames: []string{"service.dependent"}},
    )

    if false == errors.Is(registerErr, ErrTeardownDependencyIsSelf) {
        t.Fatalf("expected ErrTeardownDependencyIsSelf, got %v", registerErr)
    }

    if true == serviceContainer.Has("service.dependent") {
        t.Fatalf("expected the refused registration to leave no provider behind")
    }
}

func TestContainer_Register_AdmitsATypeDeclarationOnItsOwnGoTypeWhenTheRegistrationDoesNotFileIt(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    serviceContainer.MustRegister(
        "service.b",
        func(resolver containercontract.Resolver) (*typedNodeProbeService, error) {
            return &typedNodeProbeService{label: "b"}, nil
        },
        RegisterOptions{},
    )

    registerErr := serviceContainer.Register(
        "service.a",
        func(resolver containercontract.Resolver) (*typedNodeProbeService, error) {
            return &typedNodeProbeService{label: "a"}, nil
        },
        RegisterOptions{TypeRegistrationDisabled: true, TeardownDependencyTypes: []reflect.Type{reflect.TypeFor[*typedNodeProbeService]()}},
    )
    if nil != registerErr {
        t.Fatalf("expected the declaration on the sibling's type to be admitted, got %v", registerErr)
    }

    MustFromResolver[*typedNodeProbeService](t.Context(), serviceContainer, "service.b")
    MustFromResolver[*typedNodeProbeService](t.Context(), serviceContainer, "service.a")

    for _, entry := range serviceContainer.(teardownPlanner).TeardownPlan() {
        if "service:service.a" != entry.NodeKey {
            continue
        }

        if 1 != len(entry.Dependencies) || "service:service.b" != entry.Dependencies[0] {
            t.Fatalf("expected service.a closed before service.b through the declaration on its type, got %v", entry.Dependencies)
        }

        return
    }

    t.Fatalf("expected the plan to list service.a")
}

func TestContainer_Register_RefusesATypeDeclarationOnTheTypeTheRegistrationFilesItself(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    registerErr := serviceContainer.Register(
        "service.a",
        func(resolver containercontract.Resolver) (*typedNodeProbeService, error) {
            return &typedNodeProbeService{label: "a"}, nil
        },
        RegisterOptions{TeardownDependencyTypes: []reflect.Type{reflect.TypeFor[*typedNodeProbeService]()}},
    )

    if false == errors.Is(registerErr, ErrTeardownDependencyIsSelf) {
        t.Fatalf("expected ErrTeardownDependencyIsSelf, got %v", registerErr)
    }

    if true == serviceContainer.Has("service.a") {
        t.Fatalf("expected the refused registration to leave no provider behind")
    }
}

type registerProbeService struct{}

type typedNodeProbeService struct {
    label string
}

func (instance *typedNodeProbeService) CloseWithContext(closeContext context.Context) error {
    return nil
}

func TestRegisterGeneric_AnyServiceTypeIsRefusedForTheTypeRegistration(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    registerErr := Register[any](
        serviceContainer,
        "app.any",
        func(resolver containercontract.Resolver) (any, error) {
            return &testService{}, nil
        },
        RegisterOptions{},
    )
    if nil == registerErr || "type registration requires a concrete type" != registerErr.Error() {
        t.Fatalf("expected an any service type refused for the type registration, got %v", registerErr)
    }

    withoutTypeErr := Register[any](
        serviceContainer,
        "app.any",
        func(resolver containercontract.Resolver) (any, error) {
            return &testService{}, nil
        },
        RegisterOptions{TypeRegistrationDisabled: true},
    )
    if nil == withoutTypeErr || "provider must not return any" != withoutTypeErr.Error() {
        t.Fatalf("expected the second refusal to come from the provider contract, got %v", withoutTypeErr)
    }
}
