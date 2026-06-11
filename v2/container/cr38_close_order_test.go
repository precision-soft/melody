package container

import (
    "sync"
    "testing"

    containercontract "github.com/precision-soft/melody/v2/container/contract"
)

func TestContainer_Close_ClosesDependentsBeforeDependencies_NamedServiceDependsByTypeOnTypeRegisteredService(t *testing.T) {
    serviceContainer := NewContainer()

    var mutex sync.Mutex
    closeSequence := make([]string, 0, 2)
    recorder := &closeOrderRecorder{
        mutex:         &mutex,
        closeSequence: &closeSequence,
    }

    err := Register(
        serviceContainer,
        "service.b",
        func(resolver containercontract.Resolver) (*closeOrderServiceB, error) {
            return &closeOrderServiceB{recorder: recorder}, nil
        },
        WithTypeRegistration(true),
    )
    if nil != err {
        t.Fatalf("unexpected register error: %v", err)
    }

    err = Register(
        serviceContainer,
        "service.a",
        func(resolver containercontract.Resolver) (*closeOrderServiceA, error) {
            _, dependencyErr := FromResolverByType[*closeOrderServiceB](resolver)
            if nil != dependencyErr {
                return nil, dependencyErr
            }

            return &closeOrderServiceA{recorder: recorder}, nil
        },
    )
    if nil != err {
        t.Fatalf("unexpected register error: %v", err)
    }

    _, err = serviceContainer.Get("service.a")
    if nil != err {
        t.Fatalf("unexpected get error: %v", err)
    }

    err = serviceContainer.Close()
    if nil != err {
        t.Fatalf("unexpected close error: %v", err)
    }

    if 2 != len(closeSequence) {
        t.Fatalf("expected 2 close calls, got %d", len(closeSequence))
    }

    if "a" != closeSequence[0] {
        t.Fatalf("expected dependent a to close first, got %s", closeSequence[0])
    }

    if "b" != closeSequence[1] {
        t.Fatalf("expected dependency b to close second, got %s", closeSequence[1])
    }
}
