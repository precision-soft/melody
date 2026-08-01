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

/* @info SetContext replaces the whole map, and it must copy on the way in for the same reason the constructor does: the caller keeps its reference, and the error is read from other goroutines through Context() — a later write into the map it still holds is a concurrent map write against a map read, which no recover reaches */
func TestError_SetContext_ReplacesTheContextAndCopiesTheInput(t *testing.T) {
    err := NewError("message", map[string]any{"old": "value"}, nil)

    replacement := map[string]any{"new": "value"}

    err.SetContext(replacement)

    if nil != err.Context()["old"] {
        t.Fatalf("expected the previous context to be replaced, got %v", err.Context())
    }

    if "value" != err.Context()["new"] {
        t.Fatalf("expected the new context, got %v", err.Context())
    }

    replacement["new"] = "mutated after the call"

    if "value" != err.Context()["new"] {
        t.Fatalf("expected SetContext to copy the caller's map, got %v", err.Context()["new"])
    }
}

/* @info a nil map handed to SetContext must leave the error with an empty context rather than a nil one: the next SetContextValue writes into whatever is there, and the copy is what makes that map non-nil */
func TestError_SetContext_WithNilMap_LeavesAWritableContext(t *testing.T) {
    err := NewError("message", map[string]any{"old": "value"}, nil)

    err.SetContext(nil)

    if 0 != len(err.Context()) {
        t.Fatalf("expected an empty context, got %v", err.Context())
    }

    err.SetContextValue("written", "after")

    if "after" != err.Context()["written"] {
        t.Fatalf("expected the context to stay writable, got %v", err.Context())
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
