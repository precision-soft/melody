package http

import (
    "context"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
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
}
