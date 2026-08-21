package http

import (
    "context"
    nethttp "net/http"
    "net/http/httptest"
    "strconv"
    "strings"
    "testing"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/config"
    "github.com/precision-soft/melody/v3/internal/testhelper"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "github.com/precision-soft/melody/v3/session"
    "github.com/precision-soft/melody/v3/validation"
)

type jsonHandlerTestRequest struct {
    Name string `json:"name" validate:"notBlank"`
}

/* the shared http container carries the configuration the body-reading door resolves the request
   limit from: JsonHandler binds through the same door Request.BindJson does, so it needs the same
   services the rest of the request cycle needs rather than a container of its own with fewer */
func newJsonHandlerRuntime() runtimecontract.Runtime {
    serviceContainer := newHttpTestContainer()

    serviceContainer.MustRegister(
        validation.ServiceValidator,
        func(resolver containercontract.Resolver) (*validation.Validator, error) {
            return validation.NewValidator(), nil
        },
    )

    return runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)
}

func newJsonHandlerRuntimeWithBodyLimit(maxRequestBodyBytes int) runtimecontract.Runtime {
    serviceContainer := newHttpTestContainerWithSessionStorageAndEnvironmentValues(
        session.NewInMemoryStorage(),
        map[string]string{
            config.HttpMaxRequestBodyBytesKey: strconv.Itoa(maxRequestBodyBytes),
        },
    )

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

func TestJsonHandler_AnswersAnOversizedBodyWith413RatherThanInvalidJson(t *testing.T) {
    runtimeInstance := newJsonHandlerRuntimeWithBodyLimit(8)

    handler := JsonHandler(func(currentRuntime runtimecontract.Runtime, request httpcontract.Request, body jsonHandlerTestRequest) (httpcontract.Response, error) {
        return TextResponse(nethttp.StatusOK, "ok"), nil
    })

    httpRequest := httptest.NewRequest(nethttp.MethodPost, "/x", strings.NewReader(`{"name":"a much longer name than the limit"}`))
    request := NewRequest(httpRequest, nil, runtimeInstance, nil)

    _, handleErr := handler(runtimeInstance, httptest.NewRecorder(), request)

    httpException := exception.AsHttpException(handleErr)
    if nil == httpException {
        t.Fatalf("expected an http exception, got %T: %v", handleErr, handleErr)
    }

    /* the limit used to be laundered into a flat 400 "invalid json": the client retried the same
       payload forever because it was told its json was broken, and the 413-at-warning treatment the
       kernel builds for every other body path was bypassed */
    if nethttp.StatusRequestEntityTooLarge != httpException.StatusCode() {
        t.Fatalf("expected status %d, got %d", nethttp.StatusRequestEntityTooLarge, httpException.StatusCode())
    }
}

func TestJsonHandler_CarriesTheDecoderDiagnosisAsTheRefusalCause(t *testing.T) {
    runtimeInstance := newJsonHandlerRuntime()

    handler := JsonHandler(func(currentRuntime runtimecontract.Runtime, request httpcontract.Request, body jsonHandlerTestRequest) (httpcontract.Response, error) {
        return TextResponse(nethttp.StatusOK, "ok"), nil
    })

    httpRequest := httptest.NewRequest(nethttp.MethodPost, "/x", strings.NewReader(`{"name": 42}`))
    request := NewRequest(httpRequest, nil, runtimeInstance, nil)

    _, handleErr := handler(runtimeInstance, httptest.NewRecorder(), request)

    httpException := exception.AsHttpException(handleErr)
    if nil == httpException {
        t.Fatalf("expected an http exception, got %T: %v", handleErr, handleErr)
    }

    /* the decoder's own diagnosis — offending offset, field, type — used to die on the line that
       replaced it with a flat message, so the operator had nothing to read */
    if nil == httpException.CauseErr() {
        t.Fatalf("expected the decoder cause to travel with the refusal")
    }
}

func TestJsonHandler_ServesValidationDetailUnderTheValidationErrorsKey(t *testing.T) {
    runtimeInstance := newJsonHandlerRuntime()

    handler := JsonHandler(func(currentRuntime runtimecontract.Runtime, request httpcontract.Request, body jsonHandlerTestRequest) (httpcontract.Response, error) {
        return TextResponse(nethttp.StatusOK, "ok"), nil
    })

    httpRequest := httptest.NewRequest(nethttp.MethodPost, "/x", strings.NewReader(`{"name":""}`))
    request := NewRequest(httpRequest, nil, runtimeInstance, nil)

    _, handleErr := handler(runtimeInstance, httptest.NewRecorder(), request)

    httpException := exception.AsHttpException(handleErr)
    if nil == httpException {
        t.Fatalf("expected an http exception, got %T: %v", handleErr, handleErr)
    }

    /* flattened into the message, the per-field detail never reached the client under the one key
       that names it, and the listener's rule-wiring classification — which reads exactly this key —
       could never fire for a route bound through this door */
    if _, exists := httpException.Context()["validationErrors"]; false == exists {
        t.Fatalf("expected the validation detail under validationErrors, got context %v", httpException.Context())
    }
}

func TestJsonHandler_RefusesANullBodyBoundToASlice(t *testing.T) {
    runtimeInstance := newJsonHandlerRuntime()

    handled := false
    handler := JsonHandler(func(currentRuntime runtimecontract.Runtime, request httpcontract.Request, body []jsonHandlerTestRequest) (httpcontract.Response, error) {
        handled = true

        return TextResponse(nethttp.StatusOK, "ok"), nil
    })

    httpRequest := httptest.NewRequest(nethttp.MethodPost, "/x", strings.NewReader(`null`))
    request := NewRequest(httpRequest, nil, runtimeInstance, nil)

    _, handleErr := handler(runtimeInstance, httptest.NewRecorder(), request)

    /* the guard asked only about pointers, so a bulk endpoint typed on a slice took the same null
       past it, the validator reported the nil collection valid, and the handler was entered with it */
    if nil == handleErr {
        t.Fatalf("expected a null body to be refused")
    }

    if true == handled {
        t.Fatalf("handler must not run for a null body")
    }
}

func TestJsonHandler_RefusesANilHandleAtConstruction(t *testing.T) {
    testhelper.AssertPanicsWithError(
        t,
        func() {
            JsonHandler[jsonHandlerTestRequest](nil)
        },
        "json handler may not be nil",
    )
}

func TestJsonHandler_KeepsTheRefusalWhenTheResponderAnswersNothing(t *testing.T) {
    runtimeInstance := newJsonHandlerRuntime()

    handler := JsonHandler(
        func(currentRuntime runtimecontract.Runtime, request httpcontract.Request, body jsonHandlerTestRequest) (httpcontract.Response, error) {
            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
        WithJsonHandlerErrorResponder(func(
            currentRuntime runtimecontract.Runtime,
            request httpcontract.Request,
            status int,
            message string,
            cause error,
        ) (httpcontract.Response, error) {
            return nil, nil
        }),
    )

    httpRequest := httptest.NewRequest(nethttp.MethodPost, "/x", strings.NewReader(`{`))
    request := NewRequest(httpRequest, nil, runtimeInstance, nil)

    response, handleErr := handler(runtimeInstance, httptest.NewRecorder(), request)

    /* returned as it was, the nil pair was read by the kernel as a handler that answered nothing and
       served an empty 204 — a refused write reporting success to its client with no record filed */
    if nil != response {
        t.Fatalf("expected no response, got %v", response)
    }

    if nil == handleErr {
        t.Fatalf("expected the refusal to stand when the responder answers nothing")
    }
}

func TestJsonHandler_ContainsAPanickingResponder(t *testing.T) {
    runtimeInstance := newJsonHandlerRuntime()

    handler := JsonHandler(
        func(currentRuntime runtimecontract.Runtime, request httpcontract.Request, body jsonHandlerTestRequest) (httpcontract.Response, error) {
            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
        WithJsonHandlerErrorResponder(func(
            currentRuntime runtimecontract.Runtime,
            request httpcontract.Request,
            status int,
            message string,
            cause error,
        ) (httpcontract.Response, error) {
            panic("responder exploded")
        }),
    )

    httpRequest := httptest.NewRequest(nethttp.MethodPost, "/x", strings.NewReader(`{`))
    request := NewRequest(httpRequest, nil, runtimeInstance, nil)

    _, handleErr := handler(runtimeInstance, httptest.NewRecorder(), request)
    if nil == handleErr {
        t.Fatalf("expected the contained panic to surface as an error")
    }
}

func TestJsonHandler_HandsTheResponderTheCauseItNeedsToRenderTheDetail(t *testing.T) {
    runtimeInstance := newJsonHandlerRuntime()

    var receivedCause error
    handler := JsonHandler(
        func(currentRuntime runtimecontract.Runtime, request httpcontract.Request, body jsonHandlerTestRequest) (httpcontract.Response, error) {
            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
        WithJsonHandlerErrorResponder(func(
            currentRuntime runtimecontract.Runtime,
            request httpcontract.Request,
            status int,
            message string,
            cause error,
        ) (httpcontract.Response, error) {
            receivedCause = cause

            return TextResponse(status, message), nil
        }),
    )

    httpRequest := httptest.NewRequest(nethttp.MethodPost, "/x", strings.NewReader(`{"name":""}`))
    request := NewRequest(httpRequest, nil, runtimeInstance, nil)

    if _, handleErr := handler(runtimeInstance, httptest.NewRecorder(), request); nil != handleErr {
        t.Fatalf("expected the responder's response to be served, got %v", handleErr)
    }

    /* the responder used to receive a status and a flattened string, so an application that wanted the
       structured body could not get it from this hook either */
    if nil == receivedCause {
        t.Fatalf("expected the responder to be handed the refusal itself")
    }

    if nil == exception.AsHttpException(receivedCause) {
        t.Fatalf("expected the cause to carry the http exception, got %T", receivedCause)
    }
}

func TestWithJsonHandlerErrorResponder_RefusesANilResponder(t *testing.T) {
    testhelper.AssertPanicsWithError(
        t,
        func() {
            WithJsonHandlerErrorResponder(nil)
        },
        "json handler error responder may not be nil",
    )
}
