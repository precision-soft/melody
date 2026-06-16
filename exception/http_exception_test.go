package exception

import (
    "errors"
    "fmt"
    nethttp "net/http"
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

    errorsValue, exists := ex.Context()["validationErrors"]
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
