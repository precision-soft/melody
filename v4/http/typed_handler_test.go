package http

import (
    "context"
    nethttp "net/http"
    "net/http/httptest"
    "reflect"
    "strconv"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v4/config"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    "github.com/precision-soft/melody/v4/session"
    "github.com/precision-soft/melody/v4/validation"
)

type jsonHandlerTestRequest struct {
    Name string `json:"name" validate:"notBlank"`
}

func newJsonHandlerRuntime() containercontract.Runtime {
    serviceContainer := newHttpTestContainer()

    serviceContainer.MustRegister(
        validation.ServiceValidator,
        func(resolver containercontract.Resolver) (*validation.Validator, error) {
            return validation.NewValidator(validation.ValidatorConfig{}), nil
        },
        containercontract.RegisterOptions{},
    )

    return runtime.NewRequest(context.Background(), serviceContainer.NewScope())
}

func newJsonHandlerRuntimeWithBodyLimit(maxRequestBodyBytes int) containercontract.Runtime {
    serviceContainer := newHttpTestContainerWithSessionStorageAndEnvironmentValues(
        session.NewInMemoryStorage(session.InMemoryStorageConfig{}),
        map[string]string{
            config.HttpMaxRequestBodyBytesKey: strconv.Itoa(maxRequestBodyBytes),
        },
    )

    serviceContainer.MustRegister(
        validation.ServiceValidator,
        func(resolver containercontract.Resolver) (*validation.Validator, error) {
            return validation.NewValidator(validation.ValidatorConfig{}), nil
        },
        containercontract.RegisterOptions{},
    )

    return runtime.NewRequest(context.Background(), serviceContainer.NewScope())
}

func newJsonHandlerRuntimeWithLogger(logger loggingcontract.Logger) containercontract.Runtime {
    serviceContainer := newHttpTestContainer()
    serviceContainer.MustOverrideProtectedInstance(logging.ServiceLogger, logger, containercontract.OverrideOptions{})

    serviceContainer.MustRegister(
        validation.ServiceValidator,
        func(resolver containercontract.Resolver) (*validation.Validator, error) {
            return validation.NewValidator(validation.ValidatorConfig{}), nil
        },
        containercontract.RegisterOptions{},
    )

    return runtime.NewRequest(context.Background(), serviceContainer.NewScope())
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
    handler := JsonHandler(func(currentRuntime containercontract.Runtime, request httpcontract.Request, body jsonHandlerTestRequest) (httpcontract.Response, error) {
        captured = body

        return TextResponse(nethttp.StatusOK, "ok"), nil
    }, JsonHandlerConfig{})

    httpRequest := httptest.NewRequest(nethttp.MethodPost, "/x", strings.NewReader(`{"name":"abc"}`))
    request := NewRequest(httpRequest, runtimeInstance, RequestConfig{})

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

    handler := JsonHandler(func(currentRuntime containercontract.Runtime, request httpcontract.Request, body jsonHandlerTestRequest) (httpcontract.Response, error) {
        return TextResponse(nethttp.StatusOK, "ok"), nil
    }, JsonHandlerConfig{})

    httpRequest := httptest.NewRequest(nethttp.MethodPost, "/x", strings.NewReader(`{"name":""}`))
    request := NewRequest(httpRequest, runtimeInstance, RequestConfig{})

    _, handleErr := handler(runtimeInstance, httptest.NewRecorder(), request)
    if nil == handleErr {
        t.Fatalf("expected a validation error for a blank name")
    }

    requireJsonHandlerBadRequest(t, handleErr)
}

func TestJsonHandler_RejectsTrailingData(t *testing.T) {
    runtimeInstance := newJsonHandlerRuntime()

    handled := false
    handler := JsonHandler(func(currentRuntime containercontract.Runtime, request httpcontract.Request, body jsonHandlerTestRequest) (httpcontract.Response, error) {
        handled = true

        return TextResponse(nethttp.StatusOK, "ok"), nil
    }, JsonHandlerConfig{})

    httpRequest := httptest.NewRequest(nethttp.MethodPost, "/x", strings.NewReader(`{"name":"abc"} garbage`))
    request := NewRequest(httpRequest, runtimeInstance, RequestConfig{})

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
    handler := JsonHandler(func(currentRuntime containercontract.Runtime, request httpcontract.Request, body *jsonHandlerTestRequest) (httpcontract.Response, error) {
        handled = true
        receivedNil = nil == body

        return TextResponse(nethttp.StatusOK, "ok"), nil
    }, JsonHandlerConfig{})

    httpRequest := httptest.NewRequest(nethttp.MethodPost, "/x", strings.NewReader(`null`))
    request := NewRequest(httpRequest, runtimeInstance, RequestConfig{})

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
    handler := JsonHandler(func(currentRuntime containercontract.Runtime, request httpcontract.Request, body *jsonHandlerTestRequest) (httpcontract.Response, error) {
        captured = body

        return TextResponse(nethttp.StatusOK, "ok"), nil
    }, JsonHandlerConfig{})

    httpRequest := httptest.NewRequest(nethttp.MethodPost, "/x", strings.NewReader(`{"name":"abc"}`))
    request := NewRequest(httpRequest, runtimeInstance, RequestConfig{})

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
    handler := JsonHandler(func(currentRuntime containercontract.Runtime, request httpcontract.Request, body *jsonHandlerTestRequest) (httpcontract.Response, error) {
        handled = true

        return TextResponse(nethttp.StatusOK, "ok"), nil
    }, JsonHandlerConfig{})

    httpRequest := httptest.NewRequest(nethttp.MethodPost, "/x", strings.NewReader(`{}`))
    request := NewRequest(httpRequest, runtimeInstance, RequestConfig{})

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

    handler := JsonHandler(func(currentRuntime containercontract.Runtime, request httpcontract.Request, body jsonHandlerTestRequest) (httpcontract.Response, error) {
        return TextResponse(nethttp.StatusOK, "ok"), nil
    }, JsonHandlerConfig{})

    httpRequest := httptest.NewRequest(nethttp.MethodPost, "/x", strings.NewReader(`{"name":"a much longer name than the limit"}`))
    request := NewRequest(httpRequest, runtimeInstance, RequestConfig{})

    _, handleErr := handler(runtimeInstance, httptest.NewRecorder(), request)

    httpException := exception.AsHttpException(handleErr)
    if nil == httpException {
        t.Fatalf("expected an http exception, got %T: %v", handleErr, handleErr)
    }

    if nethttp.StatusRequestEntityTooLarge != httpException.StatusCode() {
        t.Fatalf("expected status %d, got %d", nethttp.StatusRequestEntityTooLarge, httpException.StatusCode())
    }
}

func TestJsonHandler_CarriesTheDecoderDiagnosisAsTheRefusalCause(t *testing.T) {
    runtimeInstance := newJsonHandlerRuntime()

    handler := JsonHandler(func(currentRuntime containercontract.Runtime, request httpcontract.Request, body jsonHandlerTestRequest) (httpcontract.Response, error) {
        return TextResponse(nethttp.StatusOK, "ok"), nil
    }, JsonHandlerConfig{})

    httpRequest := httptest.NewRequest(nethttp.MethodPost, "/x", strings.NewReader(`{"name": 42}`))
    request := NewRequest(httpRequest, runtimeInstance, RequestConfig{})

    _, handleErr := handler(runtimeInstance, httptest.NewRecorder(), request)

    httpException := exception.AsHttpException(handleErr)
    if nil == httpException {
        t.Fatalf("expected an http exception, got %T: %v", handleErr, handleErr)
    }

    if nil == httpException.CauseErr() {
        t.Fatalf("expected the decoder cause to travel with the refusal")
    }
}

func TestJsonHandler_ServesValidationDetailUnderTheValidationErrorsKey(t *testing.T) {
    runtimeInstance := newJsonHandlerRuntime()

    handler := JsonHandler(func(currentRuntime containercontract.Runtime, request httpcontract.Request, body jsonHandlerTestRequest) (httpcontract.Response, error) {
        return TextResponse(nethttp.StatusOK, "ok"), nil
    }, JsonHandlerConfig{})

    httpRequest := httptest.NewRequest(nethttp.MethodPost, "/x", strings.NewReader(`{"name":""}`))
    request := NewRequest(httpRequest, runtimeInstance, RequestConfig{})

    _, handleErr := handler(runtimeInstance, httptest.NewRecorder(), request)

    httpException := exception.AsHttpException(handleErr)
    if nil == httpException {
        t.Fatalf("expected an http exception, got %T: %v", handleErr, handleErr)
    }

    if _, exists := httpException.Context()["validationErrors"]; false == exists {
        t.Fatalf("expected the validation detail under validationErrors, got context %v", httpException.Context())
    }
}

func TestJsonHandler_RefusesANullBodyBoundToASlice(t *testing.T) {
    runtimeInstance := newJsonHandlerRuntime()

    handled := false
    handler := JsonHandler(func(currentRuntime containercontract.Runtime, request httpcontract.Request, body []jsonHandlerTestRequest) (httpcontract.Response, error) {
        handled = true

        return TextResponse(nethttp.StatusOK, "ok"), nil
    }, JsonHandlerConfig{})

    httpRequest := httptest.NewRequest(nethttp.MethodPost, "/x", strings.NewReader(`null`))
    request := NewRequest(httpRequest, runtimeInstance, RequestConfig{})

    _, handleErr := handler(runtimeInstance, httptest.NewRecorder(), request)

    if nil == handleErr {
        t.Fatalf("expected a null body to be refused")
    }

    if true == handled {
        t.Fatalf("handler must not run for a null body")
    }
}

type nullBodyProbe struct {
    Name string `json:"name"`
}

func jsonHandlerOutcome[T any](t *testing.T, body string) (bool, error) {
    t.Helper()

    runtimeInstance := newJsonHandlerRuntime()

    handled := false
    handler := JsonHandler(func(currentRuntime containercontract.Runtime, request httpcontract.Request, bound T) (httpcontract.Response, error) {
        handled = true

        return TextResponse(nethttp.StatusOK, "ok"), nil
    }, JsonHandlerConfig{})

    request := NewRequest(httptest.NewRequest(nethttp.MethodPost, "/x", strings.NewReader(body)), runtimeInstance, RequestConfig{})

    _, handleErr := handler(runtimeInstance, httptest.NewRecorder(), request)

    return handled, handleErr
}

func TestJsonHandler_RefusesANullBodyForEveryNilableKind(t *testing.T) {
    outcomes := map[string]func(body string) (bool, error){
        "pointer": func(body string) (bool, error) { return jsonHandlerOutcome[*nullBodyProbe](t, body) },
        "slice":   func(body string) (bool, error) { return jsonHandlerOutcome[[]nullBodyProbe](t, body) },
        "map":     func(body string) (bool, error) { return jsonHandlerOutcome[map[string]any](t, body) },
        "any":     func(body string) (bool, error) { return jsonHandlerOutcome[any](t, body) },
    }

    controls := map[string]string{"pointer": `{}`, "slice": `[]`, "map": `{}`, "any": `{}`}

    for kind, outcome := range outcomes {
        handled, handleErr := outcome(`null`)
        if nil == handleErr || true == handled {
            t.Fatalf("%s: expected a null body refused before the handler, handled %v error %v", kind, handled, handleErr)
        }

        if httpException := exception.AsHttpException(handleErr); nil == httpException || nethttp.StatusBadRequest != httpException.StatusCode() {
            t.Fatalf("%s: expected the refusal answered 400, got %v", kind, handleErr)
        }

        handled, handleErr = outcome(controls[kind])
        if nil != handleErr || false == handled {
            t.Fatalf("%s: expected %s handled, handled %v error %v", kind, controls[kind], handled, handleErr)
        }
    }
}

func TestJsonHandler_RefusesANilHandleAtConstruction(t *testing.T) {
    testhelper.AssertPanicsWithError(
        t,
        func() {
            JsonHandler[jsonHandlerTestRequest](nil, JsonHandlerConfig{})
        },
        "json handler may not be nil",
    )
}

func TestJsonHandler_KeepsTheRefusalWhenTheResponderAnswersNothing(t *testing.T) {
    runtimeInstance := newJsonHandlerRuntime()

    handler := JsonHandler(
        func(currentRuntime containercontract.Runtime, request httpcontract.Request, body jsonHandlerTestRequest) (httpcontract.Response, error) {
            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
        JsonHandlerConfig{ErrorResponder: func(
            currentRuntime containercontract.Runtime,
            request httpcontract.Request,
            refusal JsonHandlerRefusal,
        ) (httpcontract.Response, error) {
            return nil, nil
        }},
    )

    httpRequest := httptest.NewRequest(nethttp.MethodPost, "/x", strings.NewReader(`{`))
    request := NewRequest(httpRequest, runtimeInstance, RequestConfig{})

    response, handleErr := handler(runtimeInstance, httptest.NewRecorder(), request)

    if nil != response {
        t.Fatalf("expected no response, got %v", response)
    }

    if nil == handleErr {
        t.Fatalf("expected the refusal to stand when the responder answers nothing")
    }
}

func TestJsonHandler_ContainsAPanickingResponder(t *testing.T) {
    recordingLogger := &errorContextRecordingLogger{}
    runtimeInstance := newJsonHandlerRuntimeWithLogger(recordingLogger)

    handler := JsonHandler(
        func(currentRuntime containercontract.Runtime, request httpcontract.Request, body jsonHandlerTestRequest) (httpcontract.Response, error) {
            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
        JsonHandlerConfig{ErrorResponder: func(
            currentRuntime containercontract.Runtime,
            request httpcontract.Request,
            refusal JsonHandlerRefusal,
        ) (httpcontract.Response, error) {
            panic("responder exploded")
        }},
    )

    httpRequest := httptest.NewRequest(nethttp.MethodPost, "/x", strings.NewReader(`{`))
    request := NewRequest(httpRequest, runtimeInstance, RequestConfig{})

    response, handleErr := handler(runtimeInstance, httptest.NewRecorder(), request)
    if nil != response {
        t.Fatalf("expected no response from a responder that panicked, got %v", response)
    }

    requireJsonHandlerBadRequest(t, handleErr)

    logContext, logged := recordingLogger.errorContextFor("json handler error responder panicked")
    if false == logged {
        t.Fatalf("expected the contained panic to be recorded where it was caught; nothing downstream can still produce it")
    }

    errorText, hasErrorText := logContext["error"].(string)
    if false == hasErrorText || false == strings.Contains(errorText, "responder exploded") {
        t.Fatalf("expected the record to name the panic, got context %v", logContext)
    }

    panicStack, hasStack := logContext["panicStack"].(string)
    if false == hasStack || "" == panicStack {
        t.Fatalf("expected the record to carry the panic-site stack, got context %v", logContext)
    }

    if false == strings.Contains(panicStack, "goroutine") {
        t.Fatalf("expected a goroutine stack in the panicStack field, got %q", panicStack)
    }
}

func TestJsonHandler_HandsTheResponderTheCauseItNeedsToRenderTheDetail(t *testing.T) {
    runtimeInstance := newJsonHandlerRuntime()

    var receivedCause error
    var receivedRefusal JsonHandlerRefusal
    handler := JsonHandler(
        func(currentRuntime containercontract.Runtime, request httpcontract.Request, body jsonHandlerTestRequest) (httpcontract.Response, error) {
            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
        JsonHandlerConfig{ErrorResponder: func(
            currentRuntime containercontract.Runtime,
            request httpcontract.Request,
            refusal JsonHandlerRefusal,
        ) (httpcontract.Response, error) {
            receivedCause = refusal.Cause
            receivedRefusal = refusal

            return TextResponse(refusal.Status, refusal.Message), nil
        }},
    )

    httpRequest := httptest.NewRequest(nethttp.MethodPost, "/x", strings.NewReader(`{"name":""}`))
    request := NewRequest(httpRequest, runtimeInstance, RequestConfig{})

    if _, handleErr := handler(runtimeInstance, httptest.NewRecorder(), request); nil != handleErr {
        t.Fatalf("expected the responder's response to be served, got %v", handleErr)
    }

    if 0 == receivedRefusal.Status || "" == receivedRefusal.Message {
        t.Fatalf("expected the responder to be handed the status and the message, got %+v", receivedRefusal)
    }

    if nil == receivedCause {
        t.Fatalf("expected the responder to be handed the refusal itself")
    }

    if nil == exception.AsHttpException(receivedCause) {
        t.Fatalf("expected the cause to carry the http exception, got %T", receivedCause)
    }
}

func handleJsonTestHandle(currentRuntime containercontract.Runtime, request httpcontract.Request, body jsonHandlerTestRequest) (httpcontract.Response, error) {
    return TextResponse(nethttp.StatusOK, body.Name), nil
}

func TestHandleJson_DeclaresTheRequestTypeOnTheRoute(t *testing.T) {
    router := NewRouter()

    HandleJson(router, "/items", handleJsonTestHandle, RouteOptions{Name: "items"}, JsonHandlerConfig{})

    definition, exists := router.RouteDefinition("items")
    if false == exists || reflect.TypeFor[jsonHandlerTestRequest]() != definition.RequestType() {
        t.Fatalf("expected the bound type declared on the route, got %v", definition)
    }
}

func TestHandleJson_ServesThroughJsonHandler(t *testing.T) {
    router := NewRouter()

    HandleJson(router, "/items", handleJsonTestHandle, RouteOptions{Methods: []string{nethttp.MethodPost}}, JsonHandlerConfig{})

    matchResult, matched := router.Match(nethttp.MethodPost, "/items", "", "")
    if false == matched {
        t.Fatalf("expected the route to match")
    }

    runtimeInstance := newJsonHandlerRuntime()

    response, handleErr := matchResult.Handler(runtimeInstance, httptest.NewRecorder(), NewRequest(httptest.NewRequest(nethttp.MethodPost, "/items", strings.NewReader(`{"name":"abc"}`)), runtimeInstance, RequestConfig{}))
    if nil != handleErr || nil == response || nethttp.StatusOK != response.StatusCode() {
        t.Fatalf("expected the handle to answer, got %v %v", response, handleErr)
    }

    _, refusedErr := matchResult.Handler(runtimeInstance, httptest.NewRecorder(), NewRequest(httptest.NewRequest(nethttp.MethodPost, "/items", strings.NewReader(`{"name":""}`)), runtimeInstance, RequestConfig{}))
    requireJsonHandlerBadRequest(t, refusedErr)
}

func TestHandleJson_OptionsDeclaringAnotherType_Refused(t *testing.T) {
    router := NewRouter()

    defer func() {
        recoveredErr, _ := recover().(*exception.Error)
        if nil == recoveredErr || "the route options declare another request type than the handler binds" != recoveredErr.Message() || "/items" != recoveredErr.Context()["pattern"] || "string" != recoveredErr.Context()["declared"] || "http.jsonHandlerTestRequest" != recoveredErr.Context()["bound"] {
            t.Fatalf("expected the conflicting type refused naming both, got %v", recoveredErr)
        }

        if 0 != len(router.RouteDefinitions()) {
            t.Fatalf("expected no route registered")
        }
    }()

    HandleJson(router, "/items", handleJsonTestHandle, RouteOptions{RequestType: reflect.TypeFor[string]()}, JsonHandlerConfig{})
}

func TestHandleJson_OptionsDeclaringTheSameType_Admitted(t *testing.T) {
    router := NewRouter()

    HandleJson(router, "/items", handleJsonTestHandle, RouteOptions{Name: "items", RequestType: reflect.TypeFor[jsonHandlerTestRequest]()}, JsonHandlerConfig{})

    if _, exists := router.RouteDefinition("items"); false == exists {
        t.Fatalf("expected the route registered")
    }
}

func TestHandleJson_OnAGroup_KeepsTheTypeAndThePrefix(t *testing.T) {
    router := NewRouter()
    group := router.Group(GroupConfig{PathPrefix: "/api", NamePrefix: "api."})

    HandleJson(group, "/items", handleJsonTestHandle, RouteOptions{Name: "items", Protection: RouteProtectionConfig{CsrfDisabled: true}, Methods: []string{nethttp.MethodPost}}, JsonHandlerConfig{})

    definition, exists := router.RouteDefinition("api.items")
    if false == exists || "/api/items" != definition.Pattern() || reflect.TypeFor[jsonHandlerTestRequest]() != definition.RequestType() {
        t.Fatalf("expected the grouped route under its prefix with its type, got %v", definition)
    }

    protection, _ := definition.Attributes()[RouteAttributeProtection].(RouteProtectionConfig)
    if false == protection.CsrfDisabled {
        t.Fatalf("expected the grouped route to keep its protection")
    }
}
