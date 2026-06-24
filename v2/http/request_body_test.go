package http

import (
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v2/exception"
    httpcontract "github.com/precision-soft/melody/v2/http/contract"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
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
