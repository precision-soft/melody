package container

import (
    "reflect"
    "testing"

    containercontract "github.com/precision-soft/melody/container/contract"
)

type registerScopedProbe struct {
    value string
}

type registerScopedOtherProbe struct {
    value string
}

func TestRegisterScopedGeneric_RegistersAServiceEveryScopeBuildsOnItsOwn(t *testing.T) {
    serviceContainer := NewContainer()

    registerScopedErr := RegisterScoped[*registerScopedProbe](
        serviceContainer,
        "app.scoped.typed",
        func(resolver containercontract.Resolver) (*registerScopedProbe, error) {
            return &registerScopedProbe{value: "scoped"}, nil
        },
    )
    if nil != registerScopedErr {
        t.Fatalf("unexpected scoped register error: %v", registerScopedErr)
    }

    firstScope := serviceContainer.NewScope()
    secondScope := serviceContainer.NewScope()

    firstValue, firstErr := firstScope.Get("app.scoped.typed")
    if nil != firstErr {
        t.Fatalf("unexpected get error on the first scope: %v", firstErr)
    }

    secondValue, secondErr := secondScope.Get("app.scoped.typed")
    if nil != secondErr {
        t.Fatalf("unexpected get error on the second scope: %v", secondErr)
    }

    if firstValue == secondValue {
        t.Fatalf("expected each scope to build its own instance of a scoped service")
    }
}

func TestRegisterScopedGeneric_NilRegistrarIsRefusedByName(t *testing.T) {
    registerScopedErr := RegisterScoped[*registerScopedProbe](
        nil,
        "app.scoped.typed",
        func(resolver containercontract.Resolver) (*registerScopedProbe, error) {
            return &registerScopedProbe{value: "scoped"}, nil
        },
    )
    if nil == registerScopedErr {
        t.Fatalf("expected a nil scoped registrar to be refused")
    }

    if "scoped registrar is nil" != registerScopedErr.Error() {
        t.Fatalf("unexpected refusal message: %q", registerScopedErr.Error())
    }
}

/* the refusal belongs to the type registration and has to be told apart from the provider contract's own refusal of an any-returning provider: opting the type registration out is what reaches that second one. */
func TestRegisterScopedGeneric_AnyServiceTypeIsRefusedForTheTypeRegistration(t *testing.T) {
    serviceContainer := NewContainer()

    registerScopedErr := RegisterScoped[any](
        serviceContainer,
        "app.scoped.any",
        func(resolver containercontract.Resolver) (any, error) {
            return &registerScopedProbe{value: "scoped"}, nil
        },
    )
    if nil == registerScopedErr {
        t.Fatalf("expected an any service type to be refused for the type registration")
    }

    if "type registration requires a concrete type" != registerScopedErr.Error() {
        t.Fatalf("unexpected refusal message: %q", registerScopedErr.Error())
    }

    withoutTypeErr := RegisterScoped[any](
        serviceContainer,
        "app.scoped.any",
        func(resolver containercontract.Resolver) (any, error) {
            return &registerScopedProbe{value: "scoped"}, nil
        },
        WithoutTypeRegistration(),
    )
    if nil == withoutTypeErr {
        t.Fatalf("expected the provider contract to refuse an any-returning provider even without the type registration")
    }

    if "provider must not return any" != withoutTypeErr.Error() {
        t.Fatalf("expected the second refusal to come from the provider contract, got %q", withoutTypeErr.Error())
    }
}

func TestMustRegisterScopedGeneric_PanicNamesTheDeclarationThatFailed(t *testing.T) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatalf("expected the failed scoped registration to panic")
        }

        recoveredErr, isError := recoveredValue.(error)
        if false == isError {
            t.Fatalf("expected an error panic value, got %#v", recoveredValue)
        }

        if "failed to register scoped service" != recoveredErr.Error() {
            t.Fatalf("unexpected panic message: %q", recoveredErr.Error())
        }
    }()

    MustRegisterScoped[*registerScopedProbe](
        nil,
        "app.scoped.typed",
        func(resolver containercontract.Resolver) (*registerScopedProbe, error) {
            return &registerScopedProbe{value: "scoped"}, nil
        },
    )
}

func TestMustRegisterScopedGeneric_RegistersOnTheHappyPath(t *testing.T) {
    serviceContainer := NewContainer()

    MustRegisterScoped[*registerScopedProbe](
        serviceContainer,
        "app.scoped.typed",
        func(resolver containercontract.Resolver) (*registerScopedProbe, error) {
            return &registerScopedProbe{value: "scoped"}, nil
        },
    )

    scopeInstance := serviceContainer.NewScope()

    value, getErr := scopeInstance.Get("app.scoped.typed")
    if nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    probe, isProbe := value.(*registerScopedProbe)
    if false == isProbe || "scoped" != probe.value {
        t.Fatalf("expected the registration to build its own service, got %#v", value)
    }
}

func TestRegisterScopedTypeGeneric_DerivesTheNameAndAnswersByType(t *testing.T) {
    serviceContainer := NewContainer()

    registerScopedTypeErr := RegisterScopedType[*registerScopedProbe](
        serviceContainer,
        func(resolver containercontract.Resolver) (*registerScopedProbe, error) {
            return &registerScopedProbe{value: "by type"}, nil
        },
    )
    if nil != registerScopedTypeErr {
        t.Fatalf("unexpected scoped register error: %v", registerScopedTypeErr)
    }

    scopeInstance := serviceContainer.NewScope()

    value, getByTypeErr := FromResolverByType[*registerScopedProbe](scopeInstance)
    if nil != getByTypeErr {
        t.Fatalf("unexpected get by type error: %v", getByTypeErr)
    }

    if "by type" != value.value {
        t.Fatalf("unexpected resolved value: %#v", value)
    }

    derivedName := defaultServiceNameForType(reflect.TypeOf((*registerScopedProbe)(nil)))
    if false == scopeInstance.Has(derivedName) {
        t.Fatalf("expected the scope to hold the derived name %q", derivedName)
    }
}

func TestRegisterScopedTypeGeneric_CallerOptionsCannotSilentlyDisarmTheTypeRegistration(t *testing.T) {
    serviceContainer := NewContainer()

    firstErr := RegisterScopedType[*registerScopedProbe](
        serviceContainer,
        func(resolver containercontract.Resolver) (*registerScopedProbe, error) {
            return &registerScopedProbe{value: "first"}, nil
        },
    )
    if nil != firstErr {
        t.Fatalf("unexpected scoped register error: %v", firstErr)
    }

    secondErr := RegisterScoped[*registerScopedProbe](
        serviceContainer,
        "app.scoped.second",
        func(resolver containercontract.Resolver) (*registerScopedProbe, error) {
            return &registerScopedProbe{value: "second"}, nil
        },
    )
    if nil == secondErr {
        t.Fatalf("expected the second registration of the same type to be refused, which proves the first one registered the type")
    }
}

func TestMustRegisterScopedTypeGeneric_PanicNamesTheDeclarationThatFailed(t *testing.T) {
    serviceContainer := NewContainer()

    MustRegisterScopedType[*registerScopedProbe](
        serviceContainer,
        func(resolver containercontract.Resolver) (*registerScopedProbe, error) {
            return &registerScopedProbe{value: "first"}, nil
        },
    )

    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatalf("expected the duplicate scoped type registration to panic")
        }

        recoveredErr, isError := recoveredValue.(error)
        if false == isError {
            t.Fatalf("expected an error panic value, got %#v", recoveredValue)
        }

        if "failed to register scoped service by type" != recoveredErr.Error() {
            t.Fatalf("unexpected panic message: %q", recoveredErr.Error())
        }
    }()

    MustRegisterScopedType[*registerScopedProbe](
        serviceContainer,
        func(resolver containercontract.Resolver) (*registerScopedProbe, error) {
            return &registerScopedProbe{value: "duplicate"}, nil
        },
    )
}

func TestMustRegisterScopedTypeGeneric_RegistersOnTheHappyPath(t *testing.T) {
    serviceContainer := NewContainer()

    MustRegisterScopedType[*registerScopedOtherProbe](
        serviceContainer,
        func(resolver containercontract.Resolver) (*registerScopedOtherProbe, error) {
            return &registerScopedOtherProbe{value: "by type"}, nil
        },
    )

    scopeInstance := serviceContainer.NewScope()

    value, getByTypeErr := FromResolverByType[*registerScopedOtherProbe](scopeInstance)
    if nil != getByTypeErr {
        t.Fatalf("unexpected get by type error: %v", getByTypeErr)
    }

    if "by type" != value.value {
        t.Fatalf("unexpected resolved value: %#v", value)
    }
}
