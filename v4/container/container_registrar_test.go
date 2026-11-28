package container

import (
    "testing"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/internal/testhelper"
)

func TestContainer_RegisterAndGetService(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    err := serviceContainer.Register(
        "service.test",
        func(resolver containercontract.Resolver) (*testService, error) {
            return &testService{Value: "ok"}, nil
        },
        RegisterOptions{},
    )
    if nil != err {
        t.Fatalf("unexpected error")
    }

    valueAny, err := serviceContainer.Get(t.Context(), "service.test")
    if nil != err {
        t.Fatalf("unexpected get error: %v", err)
    }

    service := valueAny.(*testService)
    if "ok" != service.Value {
        t.Fatalf("unexpected value")
    }
}

func TestContainer_Register_ReturnsErrorOnInvalidArguments(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    err := serviceContainer.Register(
        "",
        func(resolver containercontract.Resolver) (*testService, error) {
            return &testService{}, nil
        },
        RegisterOptions{},
    )
    if nil == err {
        t.Fatalf("expected error")
    }
}

func TestContainer_MustRegister_PanicsOnInvalidArguments(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    /* an unqualified recover accepts any panic, so the message names the refusal under test */
    testhelper.AssertPanicsWithError(
        t,
        func() {
            serviceContainer.MustRegister(
                "",
                func(resolver containercontract.Resolver) (*testService, error) {
                    return &testService{}, nil
                },
                RegisterOptions{},
            )
        },
        "service name is required to register a service",
    )
}

func TestContainerRegistrar_UntypedNilProviderIsRefusedByName(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    registerErr := serviceContainer.Register("app.nil.provider", nil, RegisterOptions{})
    if nil == registerErr {
        t.Fatalf("expected an untyped nil provider to be refused")
    }

    if "the provider is required to register a service" != registerErr.Error() {
        t.Fatalf("unexpected refusal message: %q", registerErr.Error())
    }
}
