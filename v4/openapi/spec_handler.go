package openapi

import (
    nethttp "net/http"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
)

func SpecificationHandler(info Info, registry *Registry) httpcontract.Handler {
    return func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        router := melodyhttp.RouterMustFromRuntime(runtimeInstance)

        document := Generate(info, router.RouteDefinitions(), registry)

        return melodyhttp.JsonResponse(nethttp.StatusOK, document)
    }
}
