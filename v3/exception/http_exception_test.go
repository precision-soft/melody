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

func TestAsHttpException_NilError_AnswersNilAndFalse(t *testing.T) {
    if nil != AsHttpException(nil) {
        t.Fatalf("expected nil for a nil error")
    }

    if true == IsHttpException(nil) {
        t.Fatalf("expected false for a nil error")
    }
}

func TestAsHttpException_ForeignError_AnswersNilAndFalse(t *testing.T) {
    foreign := errors.New("foreign")

    if nil != AsHttpException(foreign) {
        t.Fatalf("expected nil for an unrelated error")
    }

    if true == IsHttpException(foreign) {
        t.Fatalf("expected false for an unrelated error")
    }
}

func TestValidationFailed_SetsValidationErrorsContext(t *testing.T) {
    payload := map[string]any{"a": "b"}

    ex := ValidationFailed(payload)

    if nethttp.StatusUnprocessableEntity != ex.StatusCode() {
        t.Fatalf("unexpected status code")
    }

    if "validation failed" != ex.Message() {
        t.Fatalf("unexpected message")
    }

    errorsValue, exists := ex.Context()["validationErrors"]
    if false == exists {
        t.Fatalf("expected validationErrors context to exist")
    }

    if _, oldKeyPresent := ex.Context()["errors"]; true == oldKeyPresent {
        t.Fatalf("expected the retired errors context key to stay absent")
    }

    errorsMap, ok := errorsValue.(map[string]any)
    if false == ok {
        t.Fatalf("expected validationErrors context to be map[string]any")
    }

    if "b" != errorsMap["a"] {
        t.Fatalf("unexpected validationErrors context content")
    }
}

func TestHttpException_ZeroValueSetContextValue_AllocatesTheMap(t *testing.T) {
    zeroValue := &HttpException{}

    zeroValue.SetContextValue("key", "value")

    if "value" != zeroValue.Context()["key"] {
        t.Fatalf("expected the written value, got %v", zeroValue.Context())
    }
}

func TestIsHttpException_TypedNilMatch_AnswersFalse(t *testing.T) {
    wrappedTypedNil := &nilProviderWrapper{}

    if true == IsHttpException(wrappedTypedNil) {
        t.Fatalf("expected false for a typed-nil match")
    }

    if nil != AsHttpException(wrappedTypedNil) {
        t.Fatalf("expected nil for a typed-nil match")
    }
}

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

func TestAsHttpException_TypedNilIsRefusedInsteadOfDereferenced(t *testing.T) {
    var typedNilException *HttpException

    var asError error = typedNilException

    if nil == asError {
        t.Fatalf("test setup broken: the typed nil must be a non-nil interface")
    }

    if nil != AsHttpException(asError) {
        t.Fatalf("expected a typed nil to answer no http exception")
    }

    if true == IsHttpException(asError) {
        t.Fatalf("expected Is and As to agree on a typed nil")
    }

    var typedNilError *Error

    var errorAsInterface error = typedNilError

    if nil != AsHttpException(errorAsInterface) {
        t.Fatalf("expected a typed-nil *Error to answer no http exception")
    }
}
