package exception

import (
    "errors"
    "fmt"
    "strings"
    "sync"
    "testing"

    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    loggingcontract "github.com/precision-soft/melody/logging/contract"
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

func TestError_ZeroValueSetContextValue_AllocatesTheMap(t *testing.T) {
    zeroValue := &Error{}

    zeroValue.SetContextValue("key", "value")

    if "value" != zeroValue.Context()["key"] {
        t.Fatalf("expected the written value, got %v", zeroValue.Context())
    }
}

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

/* the proof is one writer held open against one reader on the same instance, which without the lock is a concurrent map iteration and map write; a memoized creation failure is reachable from the owner request and every waiter at once. */
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

/* errors.As and errors.Is call Unwrap on every link, so a typed-nil *Error stored as another error's cause is walked on a nil receiver — FromError(nil) is the natural producer of that link */
func TestError_UnwrapOnANilReceiverAnswersNil(t *testing.T) {
    var typedNil *Error

    if nil != typedNil.Unwrap() {
        t.Fatalf("expected a nil receiver to unwrap to nil")
    }

    chain := fmt.Errorf("ctx: %w", NewError("outer", nil, typedNil))

    var target *Error
    if false == errors.As(chain, &target) || "outer" != target.Message() {
        t.Fatalf("expected the walk to reach the outer error past the typed-nil link, got %v", target)
    }
}

/* FromError(nil) answers a typed nil, and errors.Join skips only a nil INTERFACE, so a join holding it calls Error on the nil receiver when rendered; Error answers for the receiver as Unwrap does. */
func TestError_ErrorOnANilReceiverAnswersInsteadOfDereferencing(t *testing.T) {
    var typedNil *Error

    if "error carries no value" != typedNil.Error() {
        t.Fatalf("expected the nil receiver to answer the placeholder message, got %q", typedNil.Error())
    }

    joined := errors.Join(FromError(nil), errors.New("other"))
    if false == strings.Contains(joined.Error(), "other") {
        t.Fatalf("expected the join holding a typed nil to render, got %q", joined.Error())
    }
}

/* every accessor answers the nil receiver, as every accessor of ExitError does: a caller that read the typed nil FromError(nil) produces through the interface reached these before any guard. */
func TestError_EveryAccessorAnswersTheNilReceiver(t *testing.T) {
    var typedNil *Error

    if "" != typedNil.Message() || nil != typedNil.Context() || nil != typedNil.CauseErr() || true == typedNil.AlreadyLogged() {
        t.Fatalf("expected the nil receiver answered by every reader")
    }

    if loggingcontract.LevelError != typedNil.Level() {
        t.Fatalf("expected the nil receiver to answer the error level, got %v", typedNil.Level())
    }

    typedNil.SetContext(exceptioncontract.Context{"key": "value"})
    typedNil.SetContextValue("key", "value")
    typedNil.MarkAsLogged()
}
