package http

import (
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "github.com/precision-soft/melody/config"
    "github.com/precision-soft/melody/exception"
    httpcontract "github.com/precision-soft/melody/http/contract"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
    "github.com/precision-soft/melody/session"
)

/* @info an oversized JSON body must surface as 413, not 400, when the kernel MaxBytesReader caps the read before the BindJson LimitReader does */

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

/* @info a body limit at the top of the int64 range must leave binding functional: the one-byte over-limit allowance would wrap negative and read every body as empty, answering 400 for valid JSON */

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

/* @info the decoder's own diagnosis — offending offset, field, type — travels as the exception cause: the flat message denied the log any way to distinguish a malformed body from a type mismatch on a specific field */
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
