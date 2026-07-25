package http

import (
    "context"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "github.com/precision-soft/melody/v3/validation"
)

type jsonHandlerTestRequest struct {
    Name string `json:"name" validate:"notBlank"`
}

func newJsonHandlerRuntime() runtimecontract.Runtime {
    serviceContainer := container.NewContainer()

    serviceContainer.MustRegister(
        validation.ServiceValidator,
        func(resolver containercontract.Resolver) (*validation.Validator, error) {
            return validation.NewValidator(), nil
        },
    )

    return runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)
}

func requireJsonHandlerBadRequest(t *testing.T, handleErr error) {
    t.Helper()

    if nil == handleErr {
        t.Fatalf("expected a rejection")
    }

    httpException := exception.AsHttpException(handleErr)
    if nil == httpException {
        t.Fatalf("expected an http exception, got %T: %v", handleErr, handleErr)
    }

    if nethttp.StatusBadRequest != httpException.StatusCode() {
        t.Fatalf("expected the rejection to carry status %d, got %d", nethttp.StatusBadRequest, httpException.StatusCode())
    }
}

func TestJsonHandler_DecodesValidBodyAndCallsHandle(t *testing.T) {
    runtimeInstance := newJsonHandlerRuntime()

    var captured jsonHandlerTestRequest
    handler := JsonHandler(func(currentRuntime runtimecontract.Runtime, request httpcontract.Request, body jsonHandlerTestRequest) (httpcontract.Response, error) {
        captured = body

        return TextResponse(nethttp.StatusOK, "ok"), nil
    })

    httpRequest := httptest.NewRequest(nethttp.MethodPost, "/x", strings.NewReader(`{"name":"abc"}`))
    request := NewRequest(httpRequest, nil, runtimeInstance, nil)

    response, handleErr := handler(runtimeInstance, httptest.NewRecorder(), request)
    if nil != handleErr {
        t.Fatalf("unexpected error: %v", handleErr)
    }

    if nil == response {
        t.Fatalf("expected a response")
    }

    if "abc" != captured.Name {
        t.Fatalf("expected decoded body name 'abc', got %q", captured.Name)
    }
}

func TestJsonHandler_RejectsInvalidBody(t *testing.T) {
    runtimeInstance := newJsonHandlerRuntime()

    handler := JsonHandler(func(currentRuntime runtimecontract.Runtime, request httpcontract.Request, body jsonHandlerTestRequest) (httpcontract.Response, error) {
        return TextResponse(nethttp.StatusOK, "ok"), nil
    })

    httpRequest := httptest.NewRequest(nethttp.MethodPost, "/x", strings.NewReader(`{"name":""}`))
    request := NewRequest(httpRequest, nil, runtimeInstance, nil)

    _, handleErr := handler(runtimeInstance, httptest.NewRecorder(), request)
    if nil == handleErr {
        t.Fatalf("expected a validation error for a blank name")
    }

    requireJsonHandlerBadRequest(t, handleErr)
}

func TestJsonHandler_RejectsTrailingData(t *testing.T) {
    runtimeInstance := newJsonHandlerRuntime()

    handled := false
    handler := JsonHandler(func(currentRuntime runtimecontract.Runtime, request httpcontract.Request, body jsonHandlerTestRequest) (httpcontract.Response, error) {
        handled = true

        return TextResponse(nethttp.StatusOK, "ok"), nil
    })

    httpRequest := httptest.NewRequest(nethttp.MethodPost, "/x", strings.NewReader(`{"name":"abc"} garbage`))
    request := NewRequest(httpRequest, nil, runtimeInstance, nil)

    _, handleErr := handler(runtimeInstance, httptest.NewRecorder(), request)
    if nil == handleErr {
        t.Fatalf("expected an error for trailing data after the first json value")
    }
    if true == handled {
        t.Fatalf("handler must not run when the body carries trailing data")
    }

    requireJsonHandlerBadRequest(t, handleErr)
}

func TestJsonHandler_RejectsANullBodyForAPointerRequest(t *testing.T) {
    runtimeInstance := newJsonHandlerRuntime()

    handled := false
    receivedNil := false
    handler := JsonHandler(func(currentRuntime runtimecontract.Runtime, request httpcontract.Request, body *jsonHandlerTestRequest) (httpcontract.Response, error) {
        handled = true
        receivedNil = nil == body

        return TextResponse(nethttp.StatusOK, "ok"), nil
    })

    httpRequest := httptest.NewRequest(nethttp.MethodPost, "/x", strings.NewReader(`null`))
    request := NewRequest(httpRequest, nil, runtimeInstance, nil)

    _, handleErr := handler(runtimeInstance, httptest.NewRecorder(), request)
    if nil == handleErr {
        t.Fatalf("expected a null body to be rejected, handler ran: %v, received a nil body: %v", handled, receivedNil)
    }

    if true == handled {
        t.Fatalf("handler must not run for a null body")
    }

    requireJsonHandlerBadRequest(t, handleErr)
}

func TestJsonHandler_AcceptsAPointerRequestBody(t *testing.T) {
    runtimeInstance := newJsonHandlerRuntime()

    var captured *jsonHandlerTestRequest
    handler := JsonHandler(func(currentRuntime runtimecontract.Runtime, request httpcontract.Request, body *jsonHandlerTestRequest) (httpcontract.Response, error) {
        captured = body

        return TextResponse(nethttp.StatusOK, "ok"), nil
    })

    httpRequest := httptest.NewRequest(nethttp.MethodPost, "/x", strings.NewReader(`{"name":"abc"}`))
    request := NewRequest(httpRequest, nil, runtimeInstance, nil)

    _, handleErr := handler(runtimeInstance, httptest.NewRecorder(), request)
    if nil != handleErr {
        t.Fatalf("unexpected error: %v", handleErr)
    }

    if nil == captured || "abc" != captured.Name {
        t.Fatalf("expected the pointer body to be decoded, got %v", captured)
    }
}

func TestJsonHandler_RejectsAnEmptyObjectThatFailsValidation(t *testing.T) {
    runtimeInstance := newJsonHandlerRuntime()

    handled := false
    handler := JsonHandler(func(currentRuntime runtimecontract.Runtime, request httpcontract.Request, body *jsonHandlerTestRequest) (httpcontract.Response, error) {
        handled = true

        return TextResponse(nethttp.StatusOK, "ok"), nil
    })

    httpRequest := httptest.NewRequest(nethttp.MethodPost, "/x", strings.NewReader(`{}`))
    request := NewRequest(httpRequest, nil, runtimeInstance, nil)

    _, handleErr := handler(runtimeInstance, httptest.NewRecorder(), request)
    if nil == handleErr {
        t.Fatalf("expected a blank name to be rejected")
    }

    if true == handled {
        t.Fatalf("handler must not run when validation fails")
    }

    requireJsonHandlerBadRequest(t, handleErr)
}
