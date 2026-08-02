package container

import (
    "testing"

    containercontract "github.com/precision-soft/melody/container/contract"
)

type providerContractProbe struct {
    value string
}

type providerConcreteError struct {
    detail string
}

func (instance *providerConcreteError) Error() string {
    return instance.detail
}

/* @info a typed-nil provider function passes the callers' untyped-nil checks, validates as a perfectly shaped signature and panics on its first call — boot used to report success for a service every resolution of which failed. It is refused at the one gate all three registration paths go through. */
func TestReflectedProvider_TypedNilProviderRefused(t *testing.T) {
    serviceContainer := NewContainer()

    var typedNilProvider containercontract.Provider[*providerContractProbe]

    registerErr := serviceContainer.Register("app.typed.nil.provider", typedNilProvider)
    if nil == registerErr {
        t.Fatalf("expected the typed-nil provider to be refused at registration")
    }

    scopedRegisterErr := serviceContainer.RegisterScoped("app.typed.nil.scoped", typedNilProvider)
    if nil == scopedRegisterErr {
        t.Fatalf("expected the typed-nil provider to be refused at scoped registration")
    }
}

/* @info a provider declared with a concrete error type boxes its nil error into a NON-nil interface: taken at face value, a healthy service failed every resolution, and the typed-nil cause was a delayed panic for the first Error() walk. The typed nil reads as "no error". */
func TestReflectedProvider_TypedNilConcreteErrorMeansSuccess(t *testing.T) {
    serviceContainer := NewContainer()

    registerErr := serviceContainer.Register(
        "app.concrete.error",
        func(resolver containercontract.Resolver) (*providerContractProbe, *providerConcreteError) {
            return &providerContractProbe{value: "healthy"}, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    value, getErr := serviceContainer.Get("app.concrete.error")
    if nil != getErr {
        t.Fatalf("expected the healthy service to resolve, got: %v", getErr)
    }

    typedValue, isTyped := value.(*providerContractProbe)
    if false == isTyped || "healthy" != typedValue.value {
        t.Fatalf("unexpected resolved value: %v", value)
    }
}

/* @info the provider contract is one gate with six shape refusals behind it, and only one of them — the argument count — had ever been entered by a test: the other five could each have been deleted without the suite noticing, and a provider that does not honour the contract would then have been registered at boot and called on the request path, where the reflection panics instead of returning the error the registration promised. Every refusal is asserted on its own message, because they all reach the caller through the same return and a shared assertion would let any one of them stand in for the rest. */
func TestValidateRegistrarProviderSignature_ANonFunctionIsRefused(t *testing.T) {
    serviceContainer := NewContainer()

    registerErr := serviceContainer.Register("app.not.a.function", 42)
    if nil == registerErr {
        t.Fatalf("expected a provider that is not a function to be refused")
    }

    if "provider must be a function" != registerErr.Error() {
        t.Fatalf("unexpected refusal message: %q", registerErr.Error())
    }
}

/* @info a provider taking something other than the resolver would be called with a resolver anyway, through reflection, which panics on the request path rather than at the boot line that declared it. */
func TestValidateRegistrarProviderSignature_AFirstArgumentThatIsNotTheResolverIsRefused(t *testing.T) {
    serviceContainer := NewContainer()

    registerErr := serviceContainer.Register(
        "app.wrong.argument",
        func(unrelated int) (*providerContractProbe, error) {
            return &providerContractProbe{value: "wrong"}, nil
        },
    )
    if nil == registerErr {
        t.Fatalf("expected a provider whose first argument is not the resolver to be refused")
    }

    if "provider first argument must be exactly resolver" != registerErr.Error() {
        t.Fatalf("unexpected refusal message: %q", registerErr.Error())
    }
}

/* @info the wrapper reads results[0] and results[1] by index, so a provider returning one value would index past the end of the result slice — inside the creation guard, per resolution. */
func TestValidateRegistrarProviderSignature_AProviderNotReturningTwoValuesIsRefused(t *testing.T) {
    serviceContainer := NewContainer()

    registerErr := serviceContainer.Register(
        "app.one.output",
        func(resolver containercontract.Resolver) *providerContractProbe {
            return &providerContractProbe{value: "single"}
        },
    )
    if nil == registerErr {
        t.Fatalf("expected a provider returning a single value to be refused")
    }

    if "provider must return exactly two values" != registerErr.Error() {
        t.Fatalf("unexpected refusal message: %q", registerErr.Error())
    }
}

/* @info a provider declared to return any files the service under the empty interface, which every value satisfies: the type-keyed resolution would then answer whichever registration reached the map first, for any type asked. */
func TestValidateRegistrarProviderSignature_AProviderReturningAnyIsRefused(t *testing.T) {
    serviceContainer := NewContainer()

    registerErr := serviceContainer.Register(
        "app.any.output",
        func(resolver containercontract.Resolver) (any, error) {
            return &providerContractProbe{value: "any"}, nil
        },
    )
    if nil == registerErr {
        t.Fatalf("expected a provider returning any to be refused")
    }

    if "provider must not return any" != registerErr.Error() {
        t.Fatalf("unexpected refusal message: %q", registerErr.Error())
    }
}

/* @info the wrapper asserts the second result to error, and this refusal is what makes that assertion safe — without it a provider whose second value is a plain string would reach the assertion at resolution time instead of at the line that declared it. */
func TestValidateRegistrarProviderSignature_ASecondReturnValueThatIsNotAnErrorIsRefused(t *testing.T) {
    serviceContainer := NewContainer()

    registerErr := serviceContainer.Register(
        "app.wrong.second.output",
        func(resolver containercontract.Resolver) (*providerContractProbe, string) {
            return &providerContractProbe{value: "wrong"}, ""
        },
    )
    if nil == registerErr {
        t.Fatalf("expected a provider whose second return value is not an error to be refused")
    }

    if "provider second return value must be error" != registerErr.Error() {
        t.Fatalf("unexpected refusal message: %q", registerErr.Error())
    }
}

/* @info the argument-count refusal had only ever been proven through the scoped door; the container one goes through the same gate and has to answer the same way, or the two lifetimes could drift apart on what they accept. */
func TestValidateRegistrarProviderSignature_TheArgumentCountRefusalHoldsOnTheContainerPathToo(t *testing.T) {
    serviceContainer := NewContainer()

    registerErr := serviceContainer.Register(
        "app.no.argument",
        func() (*providerContractProbe, error) {
            return &providerContractProbe{value: "none"}, nil
        },
    )
    if nil == registerErr {
        t.Fatalf("expected a provider taking no resolver to be refused")
    }

    if "provider must accept exactly one argument" != registerErr.Error() {
        t.Fatalf("unexpected refusal message: %q", registerErr.Error())
    }
}

/* @info the same concrete-error shape still fails the resolution when the provider actually errors — the normalization removes only the typed NIL, not the error channel. */
func TestReflectedProvider_ConcreteErrorStillFails(t *testing.T) {
    serviceContainer := NewContainer()

    registerErr := serviceContainer.Register(
        "app.concrete.error.failing",
        func(resolver containercontract.Resolver) (*providerContractProbe, *providerConcreteError) {
            return nil, &providerConcreteError{detail: "the provider refused"}
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    _, getErr := serviceContainer.Get("app.concrete.error.failing")
    if nil == getErr {
        t.Fatalf("expected the failing provider to fail the resolution")
    }
}
