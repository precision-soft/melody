package opentelemetry

import (
    "context"
    nethttp "net/http"
    "net/http/httptest"

    "github.com/precision-soft/melody/v3/container"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func testRequestAndRuntime() (httpcontract.Request, runtimecontract.Runtime) {
    serviceContainer := container.NewContainer()
    runtimeInstance := runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)

    httpRequest := httptest.NewRequest(nethttp.MethodGet, "/orders/42", nil)
    request := melodyhttp.NewRequest(httpRequest, nil, runtimeInstance, nil)

    return request, runtimeInstance
}

func okHandler() httpcontract.Handler {
    return func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        return melodyhttp.JsonResponse(nethttp.StatusOK, map[string]any{"ok": true})
    }
}
