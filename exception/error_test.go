package exception

import (
	"errors"
	"fmt"
	nethttp "net/http"
	"testing"

	loggingcontract "github.com/precision-soft/melody/logging/contract"
)

func TestNewError_SetsFields(t *testing.T) {
	cause := errors.New("cause")

	err := NewError(
		"message",
		map[string]any{
			"key": "value",
		},
		cause,
	)

	if "message" != err.Error() {
		t.Fatalf("unexpected message: %s", err.Error())
	}

	if nil == err.Context() {
		t.Fatalf("expected context")
	}

	if "value" != err.Context()["key"] {
		t.Fatalf("unexpected context value")
	}

	if cause != err.CauseErr() {
		t.Fatalf("unexpected cause")
	}

	if loggingcontract.LevelError != err.Level() {
		t.Fatalf("unexpected level")
	}
}

func TestNewError_CopiesContextInConstructor(t *testing.T) {
	context := map[string]any{
		"key": "value",
	}

	err := NewError(
		"message",
		context,
		nil,
	)

	context["key"] = "changed"

	if "value" != err.Context()["key"] {
		t.Fatalf("expected constructor to copy context")
	}
}

func TestNewErrorWithLevel_OverridesLevel(t *testing.T) {
	err := newWithLevel(
		"message",
		nil,
		nil,
		loggingcontract.LevelInfo,
	)

	if loggingcontract.LevelInfo != err.Level() {
		t.Fatalf("unexpected level")
	}
}

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

func TestPanic_WithNilErrorPanics(t *testing.T) {
	defer func() {
		recoveredValue := recover()
		if nil == recoveredValue {
			t.Fatalf("expected panic")
		}
	}()

	Panic(nil)
}

func TestPanic_WithErrorPanicsWithSamePointer(t *testing.T) {
	expected := NewError("panic", nil, nil)

	defer func() {
		recoveredValue := recover()
		if expected != recoveredValue {
			t.Fatalf("expected panic value to be the same *Error instance")
		}
	}()

	Panic(expected)
}

func TestFromError_ReturnsNilOnNil(t *testing.T) {
	if nil != FromError(nil) {
		t.Fatalf("expected nil")
	}
}

func TestFromError_ReturnsSameWhenAlreadyException(t *testing.T) {
	expected := NewError("x", nil, nil)

	if expected != FromError(expected) {
		t.Fatalf("expected same instance")
	}
}

func TestFromError_WrapsNonExceptionError(t *testing.T) {
	base := errors.New("base")

	ex := FromError(base)
	if nil == ex {
		t.Fatalf("expected *Error")
	}

	if base.Error() != ex.Message() {
		t.Fatalf("expected message to equal base error string")
	}

	if base != ex.CauseErr() {
		t.Fatalf("expected cause to be base error")
	}

	if loggingcontract.LevelError != ex.Level() {
		t.Fatalf("expected default level error")
	}
}

func TestIs_UsesUnwrap(t *testing.T) {
	base := errors.New("base")

	ex := NewError("wrapped", nil, base)

	if false == errors.Is(ex, base) {
		t.Fatalf("expected Is to match base via Unwrap")
	}
}

func TestHttpException_DefaultMessageWhenEmpty(t *testing.T) {
	ex := BadRequest("")
	if nethttp.StatusBadRequest != ex.StatusCode() {
		t.Fatalf("unexpected status code")
	}
	if "bad request" != ex.Message() {
		t.Fatalf("expected default message")
	}
}

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
