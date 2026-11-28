package http

import (
    "context"
    "errors"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v4/config"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/session"
    "github.com/precision-soft/melody/v4/validation"
    validationcontract "github.com/precision-soft/melody/v4/validation/contract"
)

func TestRequest_BindJsonOversizedBodyReturns413(t *testing.T) {
    var bindErr error

    router := NewRouter()
    router.Handle(
        "/bind",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            concreteRequest := request.(*Request)

            var target map[string]any
            bindErr = concreteRequest.BindJson(&target)

            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodPost}},
    )

    serviceContainer := newHttpTestContainer()
    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

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
        "/bind",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            concreteRequest := request.(*Request)

            bindErr = concreteRequest.BindJson(&target)

            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodPost}},
    )

    serviceContainer := newHttpTestContainerWithSessionStorageAndEnvironmentValues(
        session.NewInMemoryStorage(session.InMemoryStorageConfig{}),
        map[string]string{
            config.HttpMaxRequestBodyBytesKey: "9223372036854775807",
        },
    )
    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

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
        "/bind",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            concreteRequest := request.(*Request)

            var target map[string]any
            bindErr = concreteRequest.BindJson(&target)

            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodPost}},
    )

    serviceContainer := newHttpTestContainer()
    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

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

func newHttpTestContainerWithValidator() containercontract.Container {
    serviceContainer := newHttpTestContainer()

    serviceContainer.MustRegister(
        validation.ServiceValidator,
        func(resolver containercontract.Resolver) (*validation.Validator, error) {
            return validation.NewValidator(validation.ValidatorConfig{}), nil
        },
        containercontract.RegisterOptions{},
    )

    return serviceContainer
}

func bindAndValidateOutcome(body string) (error, int, string) {
    var bindErr error

    router := NewRouter()

    router.Handle(
        "/articles",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            subject := bindAndValidateSubject{}

            bindErr = request.(*Request).BindJsonAndValidate(&subject)
            if nil != bindErr {
                return nil, bindErr
            }

            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodPost}},
    )

    handler := NewKernel(router, KernelConfig{}).ServeHttp(newHttpTestContainerWithValidator())

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
        "/articles",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            if bindErr := request.(*Request).BindJsonAndValidate(&bound); nil != bindErr {
                return nil, bindErr
            }

            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodPost}},
    )

    handler := NewKernel(router, KernelConfig{}).ServeHttp(newHttpTestContainerWithValidator())

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

func TestRequest_BindJsonAndValidateCarriesTheValidationErrorsContextKey(t *testing.T) {
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

    rawErrors, present := httpException.Context()["validationErrors"]
    if false == present {
        t.Fatalf("expected the violations under the validationErrors context key, got: %v", httpException.Context())
    }

    if _, oldKeyPresent := httpException.Context()["errors"]; true == oldKeyPresent {
        t.Fatalf("expected the retired errors context key to stay absent, got: %v", httpException.Context())
    }

    violations, isValidationErrors := rawErrors.(validation.ValidationErrors)
    if false == isValidationErrors {
        t.Fatalf("expected the violations to keep their type, got: %T", rawErrors)
    }

    if false == violations.HasErrors() {
        t.Fatalf("expected the reported violations not to be empty")
    }
}

func TestRequest_BindJsonAndValidateBoundsTheViolationsOfABodyOfManyFailingElements(t *testing.T) {
    type element struct {
        Name string `json:"name" validate:"notBlank"`
    }
    type subject struct {
        Items []element `json:"items"`
    }

    var bindErr error

    router := NewRouter()

    router.Handle(
        "/articles",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            bindErr = request.(*Request).BindJsonAndValidate(&subject{})
            if nil != bindErr {
                return nil, bindErr
            }

            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodPost}},
    )

    handler := NewKernel(router, KernelConfig{}).ServeHttp(newHttpTestContainerWithValidator())

    recorder := httptest.NewRecorder()
    handler.ServeHTTP(
        recorder,
        httptest.NewRequest(nethttp.MethodPost, "/articles", strings.NewReader(`{"items":[{}`+strings.Repeat(`,{}`, 5000)+`]}`)),
    )

    if nethttp.StatusBadRequest != recorder.Code {
        t.Fatalf("expected 400, got: %d", recorder.Code)
    }

    httpException, isHttpException := bindErr.(*exception.HttpException)
    if false == isHttpException {
        t.Fatalf("expected an http exception, got: %T", bindErr)
    }

    violations, isValidationErrors := httpException.Context()["validationErrors"].(validation.ValidationErrors)
    if false == isValidationErrors {
        t.Fatalf("expected the violations under the validationErrors context key, got: %v", httpException.Context())
    }

    if 1001 != len(violations) || "errorLimitExceeded" != violations[1000].Code() {
        t.Fatalf("expected 1001 entries closed by the limit entry, got %d", len(violations))
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

func serveBindJsonAndValidateIntoAPointer(t *testing.T, body string) (error, int) {
    t.Helper()

    var bindErr error

    router := NewRouter()
    router.Handle(
        "/articles",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            var subject *bindAndValidateSubject

            bindErr = request.(*Request).BindJsonAndValidate(&subject)
            if nil != bindErr {
                return nil, bindErr
            }

            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodPost}},
    )

    recorder := httptest.NewRecorder()
    NewKernel(router, KernelConfig{}).ServeHttp(newHttpTestContainerWithValidator()).ServeHTTP(
        recorder,
        httptest.NewRequest(nethttp.MethodPost, "/articles", strings.NewReader(body)),
    )

    return bindErr, recorder.Code
}

func TestRequest_BindJsonAndValidateRefusesANullBodyAsTheTypedHandlerDoes(t *testing.T) {
    bindErr, statusCode := serveBindJsonAndValidateIntoAPointer(t, `null`)

    httpException := exception.AsHttpException(bindErr)
    if nil == httpException || "empty request body" != httpException.Message() || nethttp.StatusBadRequest != statusCode {
        t.Fatalf("expected the null body refused with 400, got %v and %d", bindErr, statusCode)
    }
}

func TestRequest_BindJsonAndValidateValidatesAnEmptyObjectBoundThroughAPointer(t *testing.T) {
    bindErr, statusCode := serveBindJsonAndValidateIntoAPointer(t, `{}`)

    httpException := exception.AsHttpException(bindErr)
    if nil == httpException || "validation failed" != httpException.Message() {
        t.Fatalf("expected the empty object refused by validation, got %v and %d", bindErr, statusCode)
    }
}

func TestRequest_BindJsonReturnsOnACyclicChainFromTheBody(t *testing.T) {
    var bindErr error

    router := NewRouter()
    router.Handle(
        "/bind",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            var target map[string]any
            bindErr = request.(*Request).BindJson(&target)

            return TextResponse(nethttp.StatusOK, "served"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodPost}},
    )

    handler := NewKernel(router, KernelConfig{}).ServeHttp(newHttpTestContainer())

    netRequest := httptest.NewRequest(nethttp.MethodPost, "/bind", &cyclicBodyReader{})
    netRequest.Header.Set("Content-Type", "application/json")

    assertReturnsOnACyclicChain(t, func() {
        handler.ServeHTTP(httptest.NewRecorder(), netRequest)
    })

    if nil == bindErr {
        t.Fatal("expected the refusal of a body that cannot be read")
    }
}

func TestRequest_BindJsonOverANilRuntimeIsRefusedNamingTheRuntime(t *testing.T) {
    for name, bind := range map[string]func(request *Request, target any) error{
        "BindJson":            func(request *Request, target any) error { return request.BindJson(target) },
        "BindJsonAndValidate": func(request *Request, target any) error { return request.BindJsonAndValidate(target) },
    } {
        t.Run(name, func(t *testing.T) {
            httpRequest := httptest.NewRequest(nethttp.MethodPost, "/bind", strings.NewReader(`{"name":"value"}`))
            httpRequest.Header.Set("Content-Type", "application/json")
            request := NewRequest(httpRequest, nil, RequestConfig{})

            defer func() {
                recoveredErr, isError := recover().(error)
                if false == isError {
                    t.Fatalf("expected a panic carrying an error")
                }

                if false == strings.Contains(recoveredErr.Error(), "runtime may not be nil") {
                    t.Fatalf("expected the nil runtime refusal, got %v", recoveredErr)
                }
            }()

            var target map[string]any
            _ = bind(request, &target)
        })
    }
}

func TestRequest_ValidateBoundBodyOverANilRuntimeIsRefusedNamingTheRuntime(t *testing.T) {
    defer func() {
        recoveredErr, isError := recover().(error)
        if false == isError {
            t.Fatalf("expected a panic carrying an error")
        }

        if false == strings.Contains(recoveredErr.Error(), "runtime may not be nil") {
            t.Fatalf("expected the nil runtime refusal, got %v", recoveredErr)
        }
    }()

    target := map[string]any{"name": "value"}
    _ = validateBoundBody(nil, &target)
}

type runtimeRecordingConstraint struct {
    seen containercontract.Runtime
}

func (instance *runtimeRecordingConstraint) Validate(validationContext validationcontract.ValidationContext, value any) validationcontract.ValidationError {
    instance.seen = validationContext.Runtime

    return nil
}

func TestValidateBoundBody_HandsTheRequestsRuntimeToTheConstraints(t *testing.T) {
    recording := &runtimeRecordingConstraint{}
    serviceContainer := newHttpTestContainer()
    serviceContainer.MustRegister(
        validation.ServiceValidator,
        func(resolver containercontract.Resolver) (*validation.Validator, error) {
            validatorInstance := validation.NewValidator(validation.ValidatorConfig{})
            validatorInstance.RegisterConstraint("recording", recording)

            return validatorInstance, nil
        },
        containercontract.RegisterOptions{},
    )

    runtimeInstance := runtime.NewRequest(context.Background(), serviceContainer.NewScope())

    target := &struct {
        Name string `json:"name" validate:"recording"`
    }{Name: "a"}

    if validateErr := validateBoundBody(runtimeInstance, target); nil != validateErr {
        t.Fatalf("unexpected validation error: %v", validateErr)
    }

    if runtimeInstance != recording.seen {
        t.Fatalf("expected the request's runtime handed to the constraint, got %v", recording.seen)
    }
}

func TestBoundBodyValidationAnswer_AValidatorErrorThatIsNoValidationIsAServerFailure(t *testing.T) {
    validatorErr := errors.New("the runtime is nil or carries no context")

    answer := boundBodyValidationAnswer(validatorErr)
    if nil == answer || false == errors.Is(answer, validatorErr) {
        t.Fatalf("expected the validator's error carried as the cause, got %v", answer)
    }

    var httpException *exception.HttpException
    if true == errors.As(answer, &httpException) {
        t.Fatalf("expected a server failure, not the client's %d", httpException.StatusCode())
    }

    if nil != boundBodyValidationAnswer(nil) {
        t.Fatalf("expected no error for a valid body")
    }

    var validationException *exception.HttpException
    if false == errors.As(boundBodyValidationAnswer(validation.ValidationErrors{validation.NewValidationError("name", "is required", "required", nil)}), &validationException) || nethttp.StatusBadRequest != validationException.StatusCode() {
        t.Fatalf("expected validation errors answered as the client's 400")
    }
}
