package http

import (
    "encoding/json"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "github.com/precision-soft/melody/config"
    containercontract "github.com/precision-soft/melody/container/contract"
    "github.com/precision-soft/melody/event"
    "github.com/precision-soft/melody/exception"
    httpcontract "github.com/precision-soft/melody/http/contract"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
    "github.com/precision-soft/melody/session"
    "github.com/precision-soft/melody/validation"
)

func TestRequest_BindJsonOversizedBodyReturns413(t *testing.T) {
    var bindErr error

    router := NewRouter()
    router.Handle(
        nethttp.MethodPost,
        "/bind",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            concreteRequest := request.(*Request)

            var target map[string]any
            bindErr = concreteRequest.BindJson(&target)

            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
    )

    serviceContainer := newHttpTestContainer()
    handler := NewKernel(router).ServeHttp(serviceContainer)

    oversizedBody := strings.Repeat("a", 2*1024*1024)
    request := httptest.NewRequest(nethttp.MethodPost, "/bind", strings.NewReader(oversizedBody))
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if nil == bindErr {
        t.Fatalf("expected BindJson to return an error for an oversized body")
    }

    httpException, ok := bindErr.(*exception.HttpException)
    if false == ok {
        t.Fatalf("expected an *exception.HttpException, got %T", bindErr)
    }

    if nethttp.StatusRequestEntityTooLarge != httpException.StatusCode() {
        t.Fatalf("expected status %d for an oversized body, got %d", nethttp.StatusRequestEntityTooLarge, httpException.StatusCode())
    }
}

func TestRequest_BindJsonMaxIntBodyLimitStillBinds(t *testing.T) {
    var bindErr error
    var target map[string]any

    router := NewRouter()
    router.Handle(
        nethttp.MethodPost,
        "/bind",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            concreteRequest := request.(*Request)

            bindErr = concreteRequest.BindJson(&target)

            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
    )

    serviceContainer := newHttpTestContainerWithSessionStorageAndEnvironmentValues(
        session.NewInMemoryStorage(),
        map[string]string{
            config.HttpMaxRequestBodyBytesKey: "9223372036854775807",
        },
    )
    handler := NewKernel(router).ServeHttp(serviceContainer)

    request := httptest.NewRequest(nethttp.MethodPost, "/bind", strings.NewReader(`{"name":"melody"}`))
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if nil != bindErr {
        t.Fatalf("expected BindJson to succeed under a max-int body limit, got %v", bindErr)
    }

    if "melody" != target["name"] {
        t.Fatalf("expected the body to be bound, got %v", target)
    }
}

func TestRequest_BindJsonInvalidJsonCarriesTheDecoderCause(t *testing.T) {
    var bindErr error

    router := NewRouter()
    router.Handle(
        nethttp.MethodPost,
        "/bind",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            concreteRequest := request.(*Request)

            var target map[string]any
            bindErr = concreteRequest.BindJson(&target)

            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
    )

    serviceContainer := newHttpTestContainer()
    handler := NewKernel(router).ServeHttp(serviceContainer)

    request := httptest.NewRequest(nethttp.MethodPost, "/bind", strings.NewReader("{not json"))
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if nil == bindErr {
        t.Fatalf("expected BindJson to refuse malformed json")
    }

    httpException, ok := bindErr.(*exception.HttpException)
    if false == ok {
        t.Fatalf("expected an *exception.HttpException, got %T", bindErr)
    }

    if nil == httpException.CauseErr() {
        t.Fatalf("expected the json decoder's error as the exception cause")
    }
}

type bindAndValidateSubject struct {
    Email string `json:"email" validate:"notBlank,email"`
    Name  string `json:"name" validate:"notBlank,min=3"`
}

/* the exception listener is what turns an HttpException a handler returned into the status and body a client receives; an application registers it at boot, so a test asserting the response rather than the error value has to register it too. */
func newHttpTestContainerWithValidator() containercontract.Container {
    serviceContainer := newHttpTestContainer()

    serviceContainer.MustRegister(
        validation.ServiceValidator,
        func(resolver containercontract.Resolver) (*validation.Validator, error) {
            return validation.NewValidator(), nil
        },
    )

    RegisterKernelExceptionListener(event.EventDispatcherMustFromContainer(serviceContainer), false)

    return serviceContainer
}

/* bindAndValidateOutcome drives one request through the kernel and reports what BindJsonAndValidate answered inside the handler, where the body is still open — reading it after ServeHttp returned would test a closed body rather than the binding. */
func bindAndValidateOutcome(body string) (error, int, string) {
    var bindErr error

    router := NewRouter()

    router.Handle(
        nethttp.MethodPost,
        "/articles",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            subject := bindAndValidateSubject{}

            bindErr = request.(*Request).BindJsonAndValidate(&subject)
            if nil != bindErr {
                return nil, bindErr
            }

            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
    )

    handler := NewKernel(router).ServeHttp(newHttpTestContainerWithValidator())

    recorder := httptest.NewRecorder()
    handler.ServeHTTP(
        recorder,
        httptest.NewRequest(nethttp.MethodPost, "/articles", strings.NewReader(body)),
    )

    return bindErr, recorder.Code, recorder.Body.String()
}

func TestRequest_BindJsonAndValidateBindsAValidBody(t *testing.T) {
    var bound bindAndValidateSubject

    router := NewRouter()

    router.Handle(
        nethttp.MethodPost,
        "/articles",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            if bindErr := request.(*Request).BindJsonAndValidate(&bound); nil != bindErr {
                return nil, bindErr
            }

            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
    )

    handler := NewKernel(router).ServeHttp(newHttpTestContainerWithValidator())

    recorder := httptest.NewRecorder()
    handler.ServeHTTP(
        recorder,
        httptest.NewRequest(nethttp.MethodPost, "/articles", strings.NewReader(`{"email":"writer@example.com","name":"melody"}`)),
    )

    if nethttp.StatusOK != recorder.Code {
        t.Fatalf("expected a valid body to bind, got: %d — %s", recorder.Code, recorder.Body.String())
    }

    if "writer@example.com" != bound.Email || "melody" != bound.Name {
        t.Fatalf("expected the body to reach the target, got: %+v", bound)
    }
}

func TestRequest_BindJsonAndValidateAnswers400WithThePerFieldDetail(t *testing.T) {
    _, statusCode, body := bindAndValidateOutcome(`{"email":"not-an-email","name":"x"}`)

    if nethttp.StatusBadRequest != statusCode {
        t.Fatalf("expected a failed validation to answer 400, got: %d", statusCode)
    }

    decoded := map[string]any{}
    if unmarshalErr := json.Unmarshal([]byte(body), &decoded); nil != unmarshalErr {
        t.Fatalf("expected a json body, got: %s", body)
    }

    rawErrors, present := decoded["errors"]
    if false == present {
        t.Fatalf("expected the per-field detail under the errors key, got: %s", body)
    }

    reportedErrors, isList := rawErrors.([]any)
    if false == isList {
        t.Fatalf("expected the errors key to carry a list, got: %T", rawErrors)
    }

    if 0 == len(reportedErrors) {
        t.Fatalf("expected at least one violation to be reported")
    }

    firstViolation, isObject := reportedErrors[0].(map[string]any)
    if false == isObject {
        t.Fatalf("expected each violation to be an object, got: %T", reportedErrors[0])
    }

    for _, key := range []string{"field", "message", "code"} {
        if _, keyPresent := firstViolation[key]; false == keyPresent {
            t.Fatalf("expected each violation to carry %q, got: %v", key, firstViolation)
        }
    }
}

func TestRequest_BindJsonAndValidateCarriesTheErrorsContextKey(t *testing.T) {
    bindErr, _, _ := bindAndValidateOutcome(`{"email":"not-an-email","name":"x"}`)

    if nil == bindErr {
        t.Fatalf("expected an invalid body to be refused")
    }

    httpException, isHttpException := bindErr.(*exception.HttpException)
    if false == isHttpException {
        t.Fatalf("expected an http exception, got: %T", bindErr)
    }

    if nethttp.StatusBadRequest != httpException.StatusCode() {
        t.Fatalf("expected 400, got: %d", httpException.StatusCode())
    }

    rawErrors, present := httpException.Context()["errors"]
    if false == present {
        t.Fatalf("expected the violations under the errors context key, got: %v", httpException.Context())
    }

    violations, isValidationErrors := rawErrors.(validation.ValidationErrors)
    if false == isValidationErrors {
        t.Fatalf("expected the violations to keep their type, got: %T", rawErrors)
    }

    if false == violations.HasErrors() {
        t.Fatalf("expected the reported violations not to be empty")
    }
}

func TestRequest_BindJsonAndValidateReturnsTheBindingFailureBeforeValidating(t *testing.T) {
    bindErr, statusCode, _ := bindAndValidateOutcome(`{"email":`)

    if nil == bindErr {
        t.Fatalf("expected an unparseable body to be refused")
    }

    if nethttp.StatusBadRequest != statusCode {
        t.Fatalf("expected 400, got: %d", statusCode)
    }

    httpException, isHttpException := bindErr.(*exception.HttpException)
    if false == isHttpException {
        t.Fatalf("expected an http exception, got: %T", bindErr)
    }

    if false == strings.HasPrefix(httpException.Error(), "invalid json") {
        t.Fatalf("expected the binding failure rather than a validation one, got: %q", httpException.Error())
    }

    if true == strings.Contains(httpException.Error(), "validation failed") {
        t.Fatalf("expected the validator not to have been consulted, got: %q", httpException.Error())
    }

    if _, present := httpException.Context()["errors"]; true == present {
        t.Fatalf("expected no validation violations on a body that never parsed")
    }
}
