package container

import (
    "testing"

    containercontract "github.com/precision-soft/melody/container/contract"
)

func TestContainer_RegisterAndGetService(t *testing.T) {
    serviceContainer := NewContainer()

    err := serviceContainer.Register(
        "service.test",
        func(resolver containercontract.Resolver) (*testService, error) {
            return &testService{Value: "ok"}, nil
        },
    )
    if nil != err {
        t.Fatalf("unexpected error")
    }

    valueAny, err := serviceContainer.Get("service.test")
    if nil != err {
        t.Fatalf("unexpected get error: %v", err)
    }

    service := valueAny.(*testService)
    if "ok" != service.Value {
        t.Fatalf("unexpected value")
    }
}

func TestContainer_Register_ReturnsErrorOnInvalidArguments(t *testing.T) {
    serviceContainer := NewContainer()

    err := serviceContainer.Register(
        "",
        func(resolver containercontract.Resolver) (*testService, error) {
            return &testService{}, nil
        },
    )
    if nil == err {
        t.Fatalf("expected error")
    }
}

func TestContainer_MustRegister_PanicsOnInvalidArguments(t *testing.T) {
    serviceContainer := NewContainer()

    defer func() {
        if nil == recover() {
            t.Fatalf("expected panic")
        }
    }()

    serviceContainer.MustRegister(
        "",
        func(resolver containercontract.Resolver) (*testService, error) {
            return &testService{}, nil
        },
    )
}

/* @info an untyped nil provider is refused at the container registrar's own door, before the contract gate is reached. The refusal is SHADOWED: the gate underneath answers an untyped nil with the identical message and the identical context, so disarming the door changes nothing a caller can observe and this test pins the verdict rather than the position of the guard. Its scoped sibling below is not shadowed — that one spells "scoped service" — which is why the two are asserted separately. */
func TestContainerRegistrar_UntypedNilProviderIsRefusedByName(t *testing.T) {
    serviceContainer := NewContainer()

    registerErr := serviceContainer.Register("app.nil.provider", nil)
    if nil == registerErr {
        t.Fatalf("expected an untyped nil provider to be refused")
    }

    if "the provider is required to register a service" != registerErr.Error() {
        t.Fatalf("unexpected refusal message: %q", registerErr.Error())
    }
}
