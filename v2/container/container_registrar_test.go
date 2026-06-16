package container

import (
    "testing"

    containercontract "github.com/precision-soft/melody/v2/container/contract"
)

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
