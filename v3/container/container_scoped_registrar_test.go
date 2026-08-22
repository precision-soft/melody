package container

import (
    "errors"
    "testing"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
    collisionalpha "github.com/precision-soft/melody/v3/container/internal/collisionalpha/contract"
    collisionbeta "github.com/precision-soft/melody/v3/container/internal/collisionbeta/contract"
)

type scopedRegistrarProbe struct {
    value string
}

func TestRegisterScoped_RefusesANameTheContainerAlreadyHolds(t *testing.T) {
    serviceContainer := NewContainer()

    registerErr := serviceContainer.Register(
        "app.thing",
        func(resolver containercontract.Resolver) (*scopedRegistrarProbe, error) {
            return &scopedRegistrarProbe{value: "container"}, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    registerScopedErr := serviceContainer.RegisterScoped(
        "app.thing",
        func(resolver containercontract.Resolver) (*scopedRegistrarProbe, error) {
            return &scopedRegistrarProbe{value: "scoped"}, nil
        },
    )
    if nil == registerScopedErr {
        t.Fatalf("expected the scoped registration to be refused")
    }

    if false == errors.Is(registerScopedErr, ErrServiceIdAlreadyRegistered) {
        t.Fatalf("expected the refusal to be classifiable as a duplicate name, got %v", registerScopedErr)
    }
}

/* both registrations opt out of the type registration on purpose: with it, the cross-level TYPE check refuses first and the name check underneath is never reached. */
func TestRegister_RefusesANameAScopedRegistrationAlreadyHolds(t *testing.T) {
    serviceContainer := NewContainer()

    registerScopedErr := serviceContainer.RegisterScoped(
        "app.thing",
        func(resolver containercontract.Resolver) (*scopedRegistrarProbe, error) {
            return &scopedRegistrarProbe{value: "scoped"}, nil
        },
        WithoutTypeRegistration(),
    )
    if nil != registerScopedErr {
        t.Fatalf("unexpected scoped register error: %v", registerScopedErr)
    }

    registerErr := serviceContainer.Register(
        "app.thing",
        func(resolver containercontract.Resolver) (*scopedRegistrarProbe, error) {
            return &scopedRegistrarProbe{value: "container"}, nil
        },
        WithoutTypeRegistration(),
    )
    if nil == registerErr {
        t.Fatalf("expected the container registration to be refused")
    }

    if false == errors.Is(registerErr, ErrScopedServiceIdAlreadyRegistered) {
        t.Fatalf("expected the refusal to name the scoped registration, got %v", registerErr)
    }
}

func TestRegister_RefusesATypeAScopedRegistrationAlreadyHolds(t *testing.T) {
    serviceContainer := NewContainer()

    registerScopedErr := serviceContainer.RegisterScoped(
        "app.scoped.thing",
        func(resolver containercontract.Resolver) (*scopedRegistrarProbe, error) {
            return &scopedRegistrarProbe{value: "scoped"}, nil
        },
    )
    if nil != registerScopedErr {
        t.Fatalf("unexpected scoped register error: %v", registerScopedErr)
    }

    registerErr := serviceContainer.Register(
        "app.container.thing",
        func(resolver containercontract.Resolver) (*scopedRegistrarProbe, error) {
            return &scopedRegistrarProbe{value: "container"}, nil
        },
    )
    if nil == registerErr {
        t.Fatalf("expected the container type registration to be refused")
    }

    if false == errors.Is(registerErr, ErrScopedServiceTypeAlreadyRegistered) {
        t.Fatalf("expected the refusal to name the scoped type registration, got %v", registerErr)
    }
}

func TestRegisterScoped_ReplacingAdmitsTheCollisionInEitherOrder(t *testing.T) {
    containerFirst := NewContainer()

    registerErr := containerFirst.Register(
        "app.thing",
        func(resolver containercontract.Resolver) (*scopedRegistrarProbe, error) {
            return &scopedRegistrarProbe{value: "container"}, nil
        },
        WithoutTypeRegistration(),
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    registerScopedErr := containerFirst.RegisterScoped(
        "app.thing",
        func(resolver containercontract.Resolver) (*scopedRegistrarProbe, error) {
            return &scopedRegistrarProbe{value: "scoped"}, nil
        },
        WithoutTypeRegistration(),
        Replacing(),
    )
    if nil != registerScopedErr {
        t.Fatalf("expected Replacing to admit the collision, got %v", registerScopedErr)
    }

    scopedFirst := NewContainer()

    registerScopedErr = scopedFirst.RegisterScoped(
        "app.thing",
        func(resolver containercontract.Resolver) (*scopedRegistrarProbe, error) {
            return &scopedRegistrarProbe{value: "scoped"}, nil
        },
        WithoutTypeRegistration(),
        Replacing(),
    )
    if nil != registerScopedErr {
        t.Fatalf("unexpected scoped register error: %v", registerScopedErr)
    }

    registerErr = scopedFirst.Register(
        "app.thing",
        func(resolver containercontract.Resolver) (*scopedRegistrarProbe, error) {
            return &scopedRegistrarProbe{value: "container"}, nil
        },
        WithoutTypeRegistration(),
    )
    if nil != registerErr {
        t.Fatalf("expected the earlier Replacing to admit the later container registration, got %v", registerErr)
    }
}

func TestRegisterScoped_RefusesADuplicateScopedName(t *testing.T) {
    serviceContainer := NewContainer()

    registerScopedErr := serviceContainer.RegisterScoped(
        "app.thing",
        func(resolver containercontract.Resolver) (*scopedRegistrarProbe, error) {
            return &scopedRegistrarProbe{value: "first"}, nil
        },
    )
    if nil != registerScopedErr {
        t.Fatalf("unexpected scoped register error: %v", registerScopedErr)
    }

    registerScopedErr = serviceContainer.RegisterScoped(
        "app.other",
        func(resolver containercontract.Resolver) (*scopedRegistrarProbe, error) {
            return &scopedRegistrarProbe{value: "second"}, nil
        },
        WithoutTypeRegistration(),
    )
    if nil != registerScopedErr {
        t.Fatalf("unexpected second scoped register error: %v", registerScopedErr)
    }

    registerScopedErr = serviceContainer.RegisterScoped(
        "app.thing",
        func(resolver containercontract.Resolver) (*scopedRegistrarProbe, error) {
            return &scopedRegistrarProbe{value: "duplicate"}, nil
        },
        WithoutTypeRegistration(),
    )
    if nil == registerScopedErr {
        t.Fatalf("expected the duplicate scoped registration to be refused")
    }

    if false == errors.Is(registerScopedErr, ErrScopedServiceIdAlreadyRegistered) {
        t.Fatalf("expected a scoped duplicate cause, got %v", registerScopedErr)
    }
}

func TestRegisterScoped_RefusesATypeTheContainerAlreadyRegistered(t *testing.T) {
    serviceContainer := NewContainer()

    registerErr := serviceContainer.Register(
        "app.container.thing",
        func(resolver containercontract.Resolver) (*scopedRegistrarProbe, error) {
            return &scopedRegistrarProbe{value: "container"}, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    registerScopedErr := serviceContainer.RegisterScoped(
        "app.scoped.thing",
        func(resolver containercontract.Resolver) (*scopedRegistrarProbe, error) {
            return &scopedRegistrarProbe{value: "scoped"}, nil
        },
        WithTypeRegistration(false),
    )
    if nil == registerScopedErr {
        t.Fatalf("expected the scoped type registration to be refused")
    }

    if false == errors.Is(registerScopedErr, ErrServiceTypeAlreadyRegistered) {
        t.Fatalf("expected a container type collision cause, got %v", registerScopedErr)
    }
}

func TestRegisterScoped_RollsBackTheNameWhenTheTypeRegistrationFails(t *testing.T) {
    serviceContainer := NewContainer().(*container)

    registerErr := serviceContainer.Register(
        "app.container.thing",
        func(resolver containercontract.Resolver) (*scopedRegistrarProbe, error) {
            return &scopedRegistrarProbe{value: "container"}, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    registerScopedErr := serviceContainer.RegisterScoped(
        "app.scoped.thing",
        func(resolver containercontract.Resolver) (*scopedRegistrarProbe, error) {
            return &scopedRegistrarProbe{value: "scoped"}, nil
        },
    )
    if nil == registerScopedErr {
        t.Fatalf("expected the scoped registration to fail on the type")
    }

    serviceContainer.mutex.RLock()
    _, nameKept := serviceContainer.scopedProviders["app.scoped.thing"]
    serviceContainer.mutex.RUnlock()

    if true == nameKept {
        t.Fatalf("expected the scoped name to be rolled back after the type registration failed")
    }
}

func TestRegisterScoped_RefusesAProviderWithTheWrongSignature(t *testing.T) {
    serviceContainer := NewContainer()

    registerScopedErr := serviceContainer.RegisterScoped(
        "app.thing",
        func() (*scopedRegistrarProbe, error) {
            return &scopedRegistrarProbe{value: "scoped"}, nil
        },
    )
    if nil == registerScopedErr {
        t.Fatalf("expected a provider taking no resolver to be refused")
    }

    if "provider must accept exactly one argument" != registerScopedErr.Error() {
        t.Fatalf("unexpected refusal message: %q", registerScopedErr.Error())
    }
}

func TestRegisterScoped_InvalidatesThePlanSoTheNextScopeSeesIt(t *testing.T) {
    serviceContainer := NewContainer()

    firstScope := serviceContainer.NewScope()

    registerScopedErr := serviceContainer.RegisterScoped(
        "app.late",
        func(resolver containercontract.Resolver) (*scopedRegistrarProbe, error) {
            return &scopedRegistrarProbe{value: "late"}, nil
        },
    )
    if nil != registerScopedErr {
        t.Fatalf("unexpected scoped register error: %v", registerScopedErr)
    }

    if true == firstScope.Has("app.late") {
        t.Fatalf("expected a running scope to keep the plan it was created with")
    }

    secondScope := serviceContainer.NewScope()

    if false == secondScope.Has("app.late") {
        t.Fatalf("expected a scope created after the registration to see it")
    }

    value, getErr := secondScope.Get("app.late")
    if nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    probe, isProbe := value.(*scopedRegistrarProbe)
    if false == isProbe || "late" != probe.value {
        t.Fatalf("expected the late registration to build its own service, got %#v", value)
    }
}

func TestNewScope_SharesTheSamePlanPointer(t *testing.T) {
    serviceContainer := NewContainer().(*container)

    registerScopedErr := serviceContainer.RegisterScoped(
        "app.thing",
        func(resolver containercontract.Resolver) (*scopedRegistrarProbe, error) {
            return &scopedRegistrarProbe{value: "scoped"}, nil
        },
    )
    if nil != registerScopedErr {
        t.Fatalf("unexpected scoped register error: %v", registerScopedErr)
    }

    firstScope := serviceContainer.NewScope().(*scope)
    secondScope := serviceContainer.NewScope().(*scope)

    if firstScope.plan != secondScope.plan {
        t.Fatalf("expected both scopes to hold the same plan pointer rather than a copy each")
    }
}

func TestRegisterScoped_ProtectedNameRefused(t *testing.T) {
    serviceContainer := NewContainer()

    plainErr := serviceContainer.RegisterScoped(
        "service.protected.probe",
        func(resolver containercontract.Resolver) (*scopedRegistrarProbe, error) {
            return &scopedRegistrarProbe{value: "scoped"}, nil
        },
    )
    if nil == plainErr {
        t.Fatalf("expected the protected name to be refused for a scoped registration")
    }

    replacingErr := serviceContainer.RegisterScoped(
        "service.protected.probe",
        func(resolver containercontract.Resolver) (*scopedRegistrarProbe, error) {
            return &scopedRegistrarProbe{value: "scoped"}, nil
        },
        Replacing(),
    )
    if nil == replacingErr {
        t.Fatalf("expected the protected name to be refused even with Replacing declared")
    }
}

/* unlike its two siblings — the generic front door and the one on a live scope, which both wrap the failure with a message naming the declaration — this door re-panics the registration's own refusal unchanged, matching MustRegister beside it; the assertion pins that spelling. */
func TestContainer_MustRegisterScoped_RegistersAndRePanicsTheRefusalUnchanged(t *testing.T) {
    serviceContainer := NewContainer()

    serviceContainer.MustRegisterScoped(
        "app.scoped.must",
        func(resolver containercontract.Resolver) (*scopedRegistrarProbe, error) {
            return &scopedRegistrarProbe{value: "scoped"}, nil
        },
    )

    scopeInstance := serviceContainer.NewScope()

    value, getErr := scopeInstance.Get("app.scoped.must")
    if nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    probe, isProbe := value.(*scopedRegistrarProbe)
    if false == isProbe || "scoped" != probe.value {
        t.Fatalf("expected the registration to build its own service, got %#v", value)
    }

    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatalf("expected the duplicate scoped registration to panic")
        }

        recoveredErr, isError := recoveredValue.(error)
        if false == isError {
            t.Fatalf("expected an error panic value, got %#v", recoveredValue)
        }

        if "scoped service already registered" != recoveredErr.Error() {
            t.Fatalf("unexpected panic message: %q", recoveredErr.Error())
        }

        if false == errors.Is(recoveredErr, ErrScopedServiceIdAlreadyRegistered) {
            t.Fatalf("expected the re-panicked refusal to keep its cause, got %v", recoveredErr)
        }
    }()

    serviceContainer.MustRegisterScoped(
        "app.scoped.must",
        func(resolver containercontract.Resolver) (*scopedRegistrarProbe, error) {
            return &scopedRegistrarProbe{value: "duplicate"}, nil
        },
        WithoutTypeRegistration(),
    )
}

func TestContainerScopedRegistrar_UntypedNilProviderIsRefusedByName(t *testing.T) {
    serviceContainer := NewContainer()

    registerScopedErr := serviceContainer.RegisterScoped("app.nil.scoped.provider", nil)
    if nil == registerScopedErr {
        t.Fatalf("expected an untyped nil scoped provider to be refused")
    }

    if "the provider is required to register a scoped service" != registerScopedErr.Error() {
        t.Fatalf("unexpected refusal message: %q", registerScopedErr.Error())
    }
}

func TestContainerScopedRegistrar_EmptyNameIsRefusedByName(t *testing.T) {
    serviceContainer := NewContainer()

    registerScopedErr := serviceContainer.RegisterScoped("", scopedNameProbeProvider())
    if nil == registerScopedErr {
        t.Fatalf("expected an empty scoped service name to be refused")
    }

    if "service name is required to register a scoped service" != registerScopedErr.Error() {
        t.Fatalf("unexpected refusal message: %q", registerScopedErr.Error())
    }
}

func scopedNameProbeProvider() containercontract.Provider[*providerContractProbe] {
    return func(resolver containercontract.Resolver) (*providerContractProbe, error) {
        return &providerContractProbe{value: "scoped"}, nil
    }
}

func TestContainer_RegisterScopedRefusedAfterClose(t *testing.T) {
    serviceContainer := NewContainer()

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    registerScopedErr := serviceContainer.RegisterScoped(
        "app.post.close.scoped",
        func(resolver containercontract.Resolver) (*testService, error) {
            return &testService{Value: "late"}, nil
        },
    )
    if nil == registerScopedErr {
        t.Fatalf("expected the scoped registration on a closed container to be refused")
    }

    if "container is closed" != registerScopedErr.Error() {
        t.Fatalf("unexpected refusal message: %q", registerScopedErr.Error())
    }
}

func TestContainer_RegisterScoped_StrictDuplicateTypeRefused(t *testing.T) {
    serviceContainer := NewContainer()

    firstErr := serviceContainer.RegisterScoped(
        "app.scoped.first",
        func(resolver containercontract.Resolver) (*testService, error) {
            return &testService{Value: "first"}, nil
        },
    )
    if nil != firstErr {
        t.Fatalf("unexpected scoped register error: %v", firstErr)
    }

    strictErr := serviceContainer.RegisterScoped(
        "app.scoped.second",
        func(resolver containercontract.Resolver) (*testService, error) {
            return &testService{Value: "second"}, nil
        },
    )
    if nil == strictErr {
        t.Fatalf("expected the strict duplicate scoped type to be refused")
    }

    if "scoped service type already registered" != strictErr.Error() {
        t.Fatalf("unexpected refusal message: %q", strictErr.Error())
    }

    if false == errors.Is(strictErr, ErrScopedServiceTypeAlreadyRegistered) {
        t.Fatalf("expected a scoped duplicate type cause, got %v", strictErr)
    }
}

/* the identity registry guards the scoped type door with the same refusal the container door carries: two pointer-to-unnamed-composite types of same-short-named packages share one identity key, and a scoped registration taking the key of an already-registered DIFFERENT type would hand the creation guard and the teardown one node for two types. The second declaration is refused at its own boot line. */
func TestRegisterScoped_TypeIdentityKeyCollisionRefused(t *testing.T) {
    serviceContainer := NewContainer()

    firstRegisterErr := serviceContainer.RegisterScoped(
        "app.scoped.collision.alpha",
        func(resolver containercontract.Resolver) (*struct{ Bus collisionalpha.Bus }, error) {
            return &struct{ Bus collisionalpha.Bus }{}, nil
        },
    )
    if nil != firstRegisterErr {
        t.Fatalf("unexpected register error: %v", firstRegisterErr)
    }

    secondRegisterErr := serviceContainer.RegisterScoped(
        "app.scoped.collision.beta",
        func(resolver containercontract.Resolver) (*struct{ Bus collisionbeta.Bus }, error) {
            return &struct{ Bus collisionbeta.Bus }{}, nil
        },
    )
    if nil == secondRegisterErr {
        t.Fatalf("expected the colliding identity key to be refused at the scoped door")
    }
}
