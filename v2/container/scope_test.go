package container

import (
    "reflect"
    "sync"
    "sync/atomic"
    "testing"

    containercontract "github.com/precision-soft/melody/v2/container/contract"
)

type scopeTestService struct {
    value string
}

func TestScope_GetDelegatesToContainerAndCachesPerScope(t *testing.T) {
    serviceContainer := NewContainer()

    calls := 0

    err := serviceContainer.Register(
        "service.test",
        func(resolver containercontract.Resolver) (*scopeTestService, error) {
            calls++
            return &scopeTestService{value: "ok"}, nil
        },
    )
    if nil != err {
        t.Fatalf("unexpected register error: %v", err)
    }

    scope := serviceContainer.NewScope()

    _, err = scope.Get("service.test")
    if nil != err {
        t.Fatalf("unexpected get error: %v", err)
    }

    _, err = scope.Get("service.test")
    if nil != err {
        t.Fatalf("unexpected get error: %v", err)
    }

    if 1 != calls {
        t.Fatalf("expected provider to be called once per container singleton")
    }
}

func TestScope_OverrideInstance_IsolatedFromContainer(t *testing.T) {
    serviceContainer := NewContainer()

    err := serviceContainer.Register(
        "service.test",
        func(resolver containercontract.Resolver) (*scopeTestService, error) {
            return &scopeTestService{value: "container"}, nil
        },
    )
    if nil != err {
        t.Fatalf("unexpected register error: %v", err)
    }

    scope := serviceContainer.NewScope()

    err = scope.OverrideProtectedInstance(
        "service.test",
        &scopeTestService{value: "scope"},
    )
    if nil != err {
        t.Fatalf("unexpected override error: %v", err)
    }

    valueAny, err := scope.Get("service.test")
    if nil != err {
        t.Fatalf("unexpected get error: %v", err)
    }

    scopeValue := valueAny.(*scopeTestService)
    if "scope" != scopeValue.value {
        t.Fatalf("expected scope override value")
    }

    containerValueAny, err := serviceContainer.Get("service.test")
    if nil != err {
        t.Fatalf("unexpected container get error: %v", err)
    }

    containerValue := containerValueAny.(*scopeTestService)
    if "container" != containerValue.value {
        t.Fatalf("expected container value to remain unchanged")
    }
}

func TestScope_ClosePanicsOnGet(t *testing.T) {
    serviceContainer := NewContainer()

    scope := serviceContainer.NewScope()
    _ = scope.Close()

    defer func() {
        if nil == recover() {
            t.Fatalf("expected panic")
        }
    }()

    _, _ = scope.Get("service.test")
}

func TestScope_HasReturnsFalseWhenClosed(t *testing.T) {
    serviceContainer := NewContainer()

    scope := serviceContainer.NewScope()
    _ = scope.Close()

    if true == scope.Has("a") {
        t.Fatalf("expected false")
    }
}

func TestScope_CloseIsIdempotent(t *testing.T) {
    serviceContainer := NewContainer()

    scopeInstance := serviceContainer.NewScope()

    if err := scopeInstance.Close(); nil != err {
        t.Fatalf("unexpected first close error: %v", err)
    }

    if err := scopeInstance.Close(); nil != err {
        t.Fatalf("unexpected second close error: %v", err)
    }
}

func TestScope_OverrideAfterCloseReturnsError(t *testing.T) {
    serviceContainer := NewContainer()

    scopeInstance := serviceContainer.NewScope()
    _ = scopeInstance.Close()

    defer func() {
        if nil == recover() {
            t.Fatalf("expected panic on override after close")
        }
    }()

    _ = scopeInstance.OverrideProtectedInstance("service.after_close", &scopeTestService{value: "late"})
}

func TestScope_GetByTypeAfterCloseReturnsError(t *testing.T) {
    serviceContainer := NewContainer()

    scopeInstance := serviceContainer.NewScope()
    _ = scopeInstance.Close()

    defer func() {
        if nil == recover() {
            t.Fatalf("expected panic on get-by-type after close")
        }
    }()

    _, _ = scopeInstance.GetByType(reflect.TypeOf((*scopeTestService)(nil)))
}

func TestScope_HasTypeReturnsFalseWhenClosed(t *testing.T) {
    serviceContainer := NewContainer()

    scopeInstance := serviceContainer.NewScope()
    _ = scopeInstance.Close()

    if true == scopeInstance.HasType(reflect.TypeOf((*scopeTestService)(nil))) {
        t.Fatalf("expected false after close")
    }
}

func TestScope_ConcurrentGetAndClose(t *testing.T) {
    serviceContainer := NewContainer()

    err := serviceContainer.Register(
        "service.concurrent",
        func(resolver containercontract.Resolver) (*scopeTestService, error) {
            return &scopeTestService{value: "ok"}, nil
        },
    )
    if nil != err {
        t.Fatalf("unexpected register error: %v", err)
    }

    scopeInstance := serviceContainer.NewScope()

    var waitGroup sync.WaitGroup
    var closeOnce sync.Once
    closeSignal := make(chan struct{})

    readerCount := 32
    panics := atomic.Int64{}

    for readerIndex := 0; readerIndex < readerCount; readerIndex++ {
        waitGroup.Add(1)
        go func() {
            defer waitGroup.Done()
            defer func() {
                if nil != recover() {
                    panics.Add(1)
                }
            }()

            for iteration := 0; iteration < 200; iteration++ {
                _, getErr := scopeInstance.Get("service.concurrent")
                if nil != getErr {
                    return
                }

                if iteration == 50 {
                    closeOnce.Do(func() {
                        close(closeSignal)
                    })
                }

                select {
                case <-closeSignal:
                default:
                }
            }
        }()
    }

    waitGroup.Add(1)
    go func() {
        defer waitGroup.Done()
        <-closeSignal
        _ = scopeInstance.Close()
    }()

    waitGroup.Wait()
}

func TestScope_ConcurrentOverrideAndGet(t *testing.T) {
    serviceContainer := NewContainer()

    err := serviceContainer.Register(
        "service.mutable",
        func(resolver containercontract.Resolver) (*scopeTestService, error) {
            return &scopeTestService{value: "base"}, nil
        },
    )
    if nil != err {
        t.Fatalf("unexpected register error: %v", err)
    }

    scopeInstance := serviceContainer.NewScope()

    var waitGroup sync.WaitGroup

    for writerIndex := 0; writerIndex < 8; writerIndex++ {
        waitGroup.Add(1)
        go func() {
            defer waitGroup.Done()
            for iteration := 0; iteration < 200; iteration++ {
                _ = scopeInstance.OverrideProtectedInstance(
                    "service.mutable",
                    &scopeTestService{value: "override"},
                )
            }
        }()
    }

    for readerIndex := 0; readerIndex < 8; readerIndex++ {
        waitGroup.Add(1)
        go func() {
            defer waitGroup.Done()
            for iteration := 0; iteration < 200; iteration++ {
                _, _ = scopeInstance.Get("service.mutable")
                _ = scopeInstance.Has("service.mutable")
            }
        }()
    }

    waitGroup.Wait()
}

func TestScope_ConcurrentHasAndClose(t *testing.T) {
    serviceContainer := NewContainer()

    scopeInstance := serviceContainer.NewScope()

    var waitGroup sync.WaitGroup
    var closeOnce sync.Once
    closeSignal := make(chan struct{})

    for readerIndex := 0; readerIndex < 16; readerIndex++ {
        waitGroup.Add(1)
        go func() {
            defer waitGroup.Done()
            for iteration := 0; iteration < 500; iteration++ {
                _ = scopeInstance.Has("service.any")
                _ = scopeInstance.HasType(reflect.TypeOf((*scopeTestService)(nil)))

                if iteration == 100 {
                    closeOnce.Do(func() {
                        close(closeSignal)
                    })
                }
            }
        }()
    }

    waitGroup.Add(1)
    go func() {
        defer waitGroup.Done()
        <-closeSignal
        _ = scopeInstance.Close()
    }()

    waitGroup.Wait()
}
