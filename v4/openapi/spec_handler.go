package openapi

import (
    nethttp "net/http"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
)

/* SpecificationHandler serves the document of the running router, resolving the Registry and the Info from the request's scope as the generate command does: a container with no registry panics on the resolution as the command does, and one with no info service serves an empty info block. */
func SpecificationHandler() httpcontract.Handler {
    return func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        router := melodyhttp.RouterMustFromRuntime(runtimeInstance)

        info := Info{}
        if true == runtimeInstance.Scope().Has(ServiceOpenApiInfo) {
            info = runtime.MustFromRuntime[Info](runtimeInstance, ServiceOpenApiInfo)
        }

        registry := runtime.MustFromRuntime[*Registry](runtimeInstance, ServiceOpenApiRegistry)

        document := Generate(info, router.RouteDefinitions(), registry)

        return melodyhttp.JsonResponse(nethttp.StatusOK, document)
    }
}
