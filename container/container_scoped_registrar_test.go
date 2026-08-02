package container

import (
    "errors"
    "testing"

    containercontract "github.com/precision-soft/melody/container/contract"
)

type scopedRegistrarProbe struct {
    value string
}

/* @info A name that answers with a process singleton outside a scope and with a per-request service inside one is the ambiguity the two lifetimes exist to keep apart. It is refused where it is made, not where it is resolved. */
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

/* @info The refusal has to hold in the other order too. Without it, whether a collision is reported at all would depend on which module happened to register first, and a framework service registered after a module's scoped one would silently shadow it.

@info Both registrations opt out of the type registration on purpose: with it, the cross-level TYPE check refuses first and the name check underneath is never reached, so a test written without it stays green even when the name check is deleted. */
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

/* @info The cross-level type check is a separate guard from the name one and needs its own proof, or deleting either leaves the other covering for it. */
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

/* @info Declaring Replacing on the scoped registration admits the collision whichever order the two arrive in: the opt-in belongs to the registration that means to shadow, and it must not depend on module ordering to be heard. */
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

/* @info Two scoped registrations under one name would make which provider answers depend on map iteration, so the second is refused with a cause of its own — the report says which lifetime the name was already taken at. */
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

/* @info A type resolving to a singleton outside a scope and to a per-request service inside one is the same ambiguity as a name, so the cross-level type check refuses it whether or not the registration called itself strict — strictness only decides whether two names may share a type at the SAME lifetime. */
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

/* @info A failed type registration must leave nothing behind: a name kept without its type would answer by name and be absent by type, which is a half-registered service nobody declared. */
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

/* @info The provider contract is enforced in one place for both lifetimes, so a scoped registration cannot accept a shape a container registration would refuse. */
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

/* @info A registration made after a scope already exists must reach the scopes created next; the published plan is a cache, and leaving it stale would silently drop the registration for the rest of the process. */
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

/* @info Creating a scope has to stay O(1) whatever the plan holds: it happens once per request, and copying the registration maps into every scope would put the whole declared graph on the request path. Holding the plan by reference is what makes that true, so the two scopes must share one pointer. */
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

/* @info the override path refuses to substitute a protected "service." name, and a scoped registration with Replacing() used to perform exactly that substitution inside every scope — where the kernel resolves through. The protected namespace holds at both lifetimes, with or without Replacing. */
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

/* @info the container's own panicking scoped door had never been executed. Unlike its two siblings — the generic front door and the one on a live scope, which both wrap the failure with a message naming the declaration — this one re-panics the registration's own refusal unchanged, matching MustRegister beside it; the assertion pins that spelling, because a wrapper added here later would change what a boot log says without any test noticing. */
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

/* @info the same refusal on the scoped door, which spells it for its own lifetime: a shared message would let either guard be deleted while the other kept the suite green. */
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

/* @info an empty scoped name is refused at the door for the reason the container one is: a service filed under it can never be asked for again. */
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

/* @info a scoped registration landing after Close would be built into scopes the teardown has finished with — the plain registrar has refused a closed container since the container session, and the scoped one carries the same refusal at its own door, where the name it is given is the one the report has to carry. */
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

/* @info the strict duplicate-type refusal exists at the scoped lifetime too, with a message of its own: the container one and this one are separate guards, and a shared assertion would let either be deleted while the other kept the suite green. */
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
