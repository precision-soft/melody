package container

import (
    "testing"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
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
