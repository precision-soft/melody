package openapi

import (
    nethttp "net/http"

    melodyhttp "github.com/precision-soft/melody/v3/http"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func SpecHandler(info Info, registry *Registry) httpcontract.Handler {
    return func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        router := melodyhttp.RouterMustFromContainer(runtimeInstance.Container())

        document := Generate(info, router.RouteDefinitions(), registry)

        return melodyhttp.JsonResponse(nethttp.StatusOK, document)
    }
}
