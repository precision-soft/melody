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
