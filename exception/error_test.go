package exception

import (
    "errors"
    "sync"
    "testing"
)

func TestError_AlreadyLoggedFlag(t *testing.T) {
    err := NewError("message", nil, nil)

    if true == err.AlreadyLogged() {
        t.Fatalf("expected default alreadyLogged to be false")
    }

    err.MarkAsLogged()

    if false == err.AlreadyLogged() {
        t.Fatalf("expected alreadyLogged to be true")
    }
}

func TestIs_UsesUnwrap(t *testing.T) {
    base := errors.New("base")

    ex := NewError("wrapped", nil, base)

    if false == errors.Is(ex, base) {
        t.Fatalf("expected Is to match base via Unwrap")
    }
}

/* @info the zero value is constructible outside the constructors and carries a nil map; the first context write must allocate it instead of panicking on the assignment */
func TestError_ZeroValueSetContextValue_AllocatesTheMap(t *testing.T) {
    zeroValue := &Error{}

    zeroValue.SetContextValue("key", "value")

    if "value" != zeroValue.Context()["key"] {
        t.Fatalf("expected the written value, got %v", zeroValue.Context())
    }
}

/* @info a creation failure memoized by the container is reachable from the owner request and every waiter request at once, so the mutable fields are locked; the proof is one writer held against one reader on the same instance, which without the lock is a concurrent map iteration and map write */
func TestError_ConcurrentContextWriteAndRead_IsOrdered(t *testing.T) {
    sharedError := NewError("creation failed", map[string]any{"attempt": 1}, nil)

    var startGroup sync.WaitGroup
    startGroup.Add(2)

    var doneGroup sync.WaitGroup
    doneGroup.Add(2)

    go func() {
        defer doneGroup.Done()

        startGroup.Done()
        startGroup.Wait()

        for iteration := 0; iteration < 1000; iteration++ {
            sharedError.SetContextValue("serviceName", iteration)
            sharedError.MarkAsLogged()
        }
    }()

    go func() {
        defer doneGroup.Done()

        startGroup.Done()
        startGroup.Wait()

        for iteration := 0; iteration < 1000; iteration++ {
            _ = sharedError.Context()
            _ = sharedError.AlreadyLogged()
        }
    }()

    doneGroup.Wait()

    if nil == sharedError.Context()["serviceName"] {
        t.Fatalf("expected the written key to survive")
    }
}
