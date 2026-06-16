package container

import (
    "testing"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
)

func TestContainer_RegisterType_AndResolveByType(t *testing.T) {
    serviceContainer := NewContainer()

    err := RegisterType[*testService](
        serviceContainer,
        func(resolver containercontract.Resolver) (*testService, error) {
            return &testService{Value: "typed"}, nil
        },
    )
    if nil != err {
        t.Fatalf("unexpected error")
    }

    service := MustFromResolverByType[*testService](serviceContainer)
    if "typed" != service.Value {
        t.Fatalf("unexpected value")
    }
}

func TestContainer_RegisterType_Interface_AndResolveByType(t *testing.T) {
    serviceContainer := NewContainer()

    err := RegisterType[testInterface](
        serviceContainer,
        func(resolver containercontract.Resolver) (testInterface, error) {
            return &testImplementation{name: "impl"}, nil
        },
    )
    if nil != err {
        t.Fatalf("unexpected error")
    }

    value := MustFromResolverByType[testInterface](serviceContainer)
    if "impl" != value.Name() {
        t.Fatalf("unexpected name")
    }
}
