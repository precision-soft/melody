package exception

import (
    "errors"
    "fmt"
    nethttp "net/http"
    "sync"
    "testing"
)

func TestHttpException_ErrorIncludesCauseWhenPresent(t *testing.T) {
    ex := NewHttpExceptionWithCause(nethttp.StatusBadRequest, "bad request", errors.New("cause"))

    if "bad request: cause" != ex.Error() {
        t.Fatalf("unexpected error string: %s", ex.Error())
    }
}

func TestIsHttpExceptionAndAsHttpException(t *testing.T) {
    ex := NotFound("x")

    wrapped := fmt.Errorf("wrapper: %w", ex)

    if false == IsHttpException(wrapped) {
        t.Fatalf("expected IsHttpException true")
    }

    resolved := AsHttpException(wrapped)
    if nil == resolved {
        t.Fatalf("expected AsHttpException to return instance")
    }
    if nethttp.StatusNotFound != resolved.StatusCode() {
        t.Fatalf("unexpected status code")
    }
    if "x" != resolved.Message() {
        t.Fatalf("unexpected message")
    }
}

/* @info the http exception carries the same mutable context as its sibling and must copy it on the way in for the same reason: the caller keeps its map, and the exception is read from the response path while the handler that built it may still be holding the reference */
func TestHttpException_SetContext_ReplacesTheContextAndCopiesTheInput(t *testing.T) {
    ex := NewHttpException(nethttp.StatusBadRequest, "bad request")

    ex.SetContextValue("old", "value")

    replacement := map[string]any{"new": "value"}

    ex.SetContext(replacement)

    if nil != ex.Context()["old"] {
        t.Fatalf("expected the previous context to be replaced, got %v", ex.Context())
    }

    replacement["new"] = "mutated after the call"

    if "value" != ex.Context()["new"] {
        t.Fatalf("expected SetContext to copy the caller's map, got %v", ex.Context()["new"])
    }
}

/* @info CauseErr is how the cause reaches a caller that does not walk the chain — the logger's enrichment reads it directly to build the cause chain of a record */
func TestHttpException_CauseErr_ReturnsTheWrappedCause(t *testing.T) {
    cause := errors.New("cause")

    ex := NewHttpExceptionWithCause(nethttp.StatusBadGateway, "bad gateway", cause)

    if cause != ex.CauseErr() {
        t.Fatalf("expected the cause to be returned")
    }

    if nil != NewHttpException(nethttp.StatusBadGateway, "bad gateway").CauseErr() {
        t.Fatalf("expected no cause when none was given")
    }
}

/* @info the entry guard is what keeps the pair honest on a plain nil: errors.As panics when handed a nil target chain of its own, and IsHttpException is written as "As found something", so a nil that got past here would decide both answers */
func TestAsHttpException_NilError_AnswersNilAndFalse(t *testing.T) {
    if nil != AsHttpException(nil) {
        t.Fatalf("expected nil for a nil error")
    }

    if true == IsHttpException(nil) {
        t.Fatalf("expected false for a nil error")
    }
}

/* @info an error that is not in the chain at all is the ordinary miss, and it must answer the same nil the guards answer */
func TestAsHttpException_ForeignError_AnswersNilAndFalse(t *testing.T) {
    foreign := errors.New("foreign")

    if nil != AsHttpException(foreign) {
        t.Fatalf("expected nil for an unrelated error")
    }

    if true == IsHttpException(foreign) {
        t.Fatalf("expected false for an unrelated error")
    }
}

func TestValidationFailed_SetsErrorsContext(t *testing.T) {
    payload := map[string]any{"a": "b"}

    ex := ValidationFailed(payload)

    if nethttp.StatusUnprocessableEntity != ex.StatusCode() {
        t.Fatalf("unexpected status code")
    }

    if "validation failed" != ex.Message() {
        t.Fatalf("unexpected message")
    }

    /* @info the detail travels under the errors key, the one the kernel exception listener serves to the client; validationErrors was a key nothing on the response path reads */
    errorsValue, exists := ex.Context()["errors"]
    if false == exists {
        t.Fatalf("expected errors context to exist")
    }

    errorsMap, ok := errorsValue.(map[string]any)
    if false == ok {
        t.Fatalf("expected errors context to be map[string]any")
    }

    if "b" != errorsMap["a"] {
        t.Fatalf("unexpected errors context content")
    }
}

/* @info the zero value is constructible outside the constructors and carries a nil map; the first context write must allocate it instead of panicking on the assignment */
func TestHttpException_ZeroValueSetContextValue_AllocatesTheMap(t *testing.T) {
    zeroValue := &HttpException{}

    zeroValue.SetContextValue("key", "value")

    if "value" != zeroValue.Context()["key"] {
        t.Fatalf("expected the written value, got %v", zeroValue.Context())
    }
}

/* @info the pair cannot disagree on a typed nil: Is answered true while As answered nil, and a caller that trusted Is and then dereferenced panicked on the disagreement */
func TestIsHttpException_TypedNilMatch_AnswersFalse(t *testing.T) {
    wrappedTypedNil := &nilProviderWrapper{}

    if true == IsHttpException(wrappedTypedNil) {
        t.Fatalf("expected false for a typed-nil match")
    }

    if nil != AsHttpException(wrappedTypedNil) {
        t.Fatalf("expected nil for a typed-nil match")
    }
}

/* @info the mutable fields are locked for the reason Error documents; the proof is one writer held against one reader on the same instance */
func TestHttpException_ConcurrentContextWriteAndRead_IsOrdered(t *testing.T) {
    sharedException := NewHttpException(500, "boom")

    var startGroup sync.WaitGroup
    startGroup.Add(2)

    var doneGroup sync.WaitGroup
    doneGroup.Add(2)

    go func() {
        defer doneGroup.Done()

        startGroup.Done()
        startGroup.Wait()

        for iteration := 0; iteration < 1000; iteration++ {
            sharedException.SetContextValue("serviceName", iteration)
            sharedException.MarkAsLogged()
        }
    }()

    go func() {
        defer doneGroup.Done()

        startGroup.Done()
        startGroup.Wait()

        for iteration := 0; iteration < 1000; iteration++ {
            _ = sharedException.Context()
            _ = sharedException.AlreadyLogged()
        }
    }()

    doneGroup.Wait()

    if nil == sharedException.Context()["serviceName"] {
        t.Fatalf("expected the written key to survive")
    }
}
