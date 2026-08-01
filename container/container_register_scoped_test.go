package container

import (
    "reflect"
    "strings"
    "testing"

    containercontract "github.com/precision-soft/melody/container/contract"
)

type registerScopedProbe struct {
    value string
}

type registerScopedOtherProbe struct {
    value string
}

/* @info the typed front door is what an application actually calls to declare a scoped service, and until now nothing executed it at all: the whole of RegisterScoped[T] — its two guards and its delegation — could have been deleted and the suite would have stayed green. It has to reach the registrar and produce a service the scope builds. */
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

/* @info the registrar arrives from a hook signature, so a nil one is a wiring mistake rather than a caller error, and it has to be named at the declaration line instead of dereferenced there. */
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

/* @info a scoped service declared as any would be filed under the empty interface, which every value satisfies, so the first resolution by type would answer with whichever registration reached the map first. The refusal belongs to the type registration and has to be told apart from the provider contract's own refusal of an any-returning provider — opting the type registration out reaches that second one, and the two must not share a message. */
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

/* @info the panicking form is the one a boot hook uses, where an error return has nowhere to go; it has to carry its own message so the failure names the declaration rather than surfacing as whatever the registrar happened to answer. */
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

/* @info the happy path of the panicking form has to register, not merely not panic: a wrapper that swallowed its delegation would pass a panic-only assertion. */
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

/* @info the by-type door derives the name itself and forces the type registration on, which is the whole of what it adds over its named sibling: the service has to answer by type from inside a scope, and the derived name has to be the one the container filed it under. */
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

/* @info without WithTypeRegistration forced on, RegisterScopedType would declare a service reachable only under a name nobody spells by hand — the option it prepends is the point of the door, and it has to survive the caller's own options being appended after it. */
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

/* @info the panicking by-type form carries its own message too, and the two must differ: a boot failure has to say whether the declaration named the service or derived it. */
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

/* @info the happy path of the panicking by-type form has to register, for the reason its named sibling does. */
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

/* @info the deferred by-type handle is the counterpart of Lazy for a component assembled before the service is safe to resolve; nothing executed it, so the closure it builds — the one that reaches FromResolverByType rather than FromResolver — was never proven to resolve anything at all. */
func TestLazyByType_ResolvesTheServiceOnFirstUse(t *testing.T) {
    serviceContainer := NewContainer()

    resolutionCount := 0

    registerErr := RegisterType[*registerScopedProbe](
        serviceContainer,
        func(resolver containercontract.Resolver) (*registerScopedProbe, error) {
            resolutionCount++

            return &registerScopedProbe{value: "lazy by type"}, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    lazyService := LazyByType[*registerScopedProbe](serviceContainer)

    if 0 != resolutionCount {
        t.Fatalf("expected the handle to resolve nothing before its first use, got %d resolutions", resolutionCount)
    }

    value := lazyService.Get()
    if "lazy by type" != value.value {
        t.Fatalf("unexpected resolved value: %#v", value)
    }

    if value != lazyService.Get() {
        t.Fatalf("expected the second use to answer with the memoized value")
    }
}

/* @info a by-type handle over a type nothing registered has to fail through the by-type resolution, not through a name lookup: the two doors report differently, and a handle wired to the wrong one would name a service the caller never spelled. */
func TestLazyByType_MissingRegistrationFailsThroughTheByTypeResolution(t *testing.T) {
    serviceContainer := NewContainer()

    lazyService := LazyByType[*registerScopedOtherProbe](serviceContainer)

    _, resolveErr := lazyService.Resolve()
    if nil == resolveErr {
        t.Fatalf("expected the by-type resolution of an unregistered type to fail")
    }

    if false == strings.Contains(resolveErr.Error(), "service type is not registered") {
        t.Fatalf("expected the by-type refusal, got %q", resolveErr.Error())
    }
}
