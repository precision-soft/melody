package container

import (
    "sync"
    "testing"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
)

/* @info closed-container resolution */

type closedGuardCloser struct {
    mutex  sync.Mutex
    closed bool
}

func (instance *closedGuardCloser) Close() error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.closed = true

    return nil
}

func (instance *closedGuardCloser) IsClosed() bool {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return instance.closed
}

func TestResolve_AfterCloseFailsInsteadOfCreating(t *testing.T) {
    serviceContainer := NewContainer()

    MustRegister[*closedGuardCloser](
        serviceContainer,
        "closed.guard",
        func(resolver containercontract.Resolver) (*closedGuardCloser, error) {
            return &closedGuardCloser{}, nil
        },
    )

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    _, getErr := serviceContainer.Get("closed.guard")
    if nil == getErr {
        t.Fatalf("expected resolution after Close to fail instead of creating a service that would never be closed")
    }
}

func TestResolve_DuringCloseClosesTheCreatedValueInsteadOfLeakingIt(t *testing.T) {
    serviceContainer := NewContainer()

    providerStarted := make(chan struct{})
    providerRelease := make(chan struct{})

    service := &closedGuardCloser{}

    MustRegister[*closedGuardCloser](
        serviceContainer,
        "closed.guard.race",
        func(resolver containercontract.Resolver) (*closedGuardCloser, error) {
            close(providerStarted)
            <-providerRelease

            return service, nil
        },
    )

    resultChannel := make(chan error, 1)
    go func() {
        _, getErr := serviceContainer.Get("closed.guard.race")
        resultChannel <- getErr
    }()

    <-providerStarted

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    close(providerRelease)

    getErr := <-resultChannel
    if nil == getErr {
        t.Fatalf("expected the resolution that finished after Close to fail")
    }

    if false == service.IsClosed() {
        t.Fatalf("expected the value created while Close ran to be closed instead of leaked")
    }
}
