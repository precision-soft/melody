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
