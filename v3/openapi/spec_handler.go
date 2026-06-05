package openapi

import (
    nethttp "net/http"

    melodyhttp "github.com/precision-soft/melody/v3/http"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/**
 * SpecHandler serves the OpenAPI 3 document as JSON, generated on each request from the live route
 * table and the supplied registry. It is the runtime counterpart to the generate command: register it
 * on a route (for example GET /openapi.json) to expose the spec from the running application. Behind a
 * load balancer every instance generates the same document from its identical route table, so the
 * response is stable regardless of which instance answers.
 */
func SpecHandler(info Info, registry *Registry) httpcontract.Handler {
    return func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        router := melodyhttp.RouterMustFromContainer(runtimeInstance.Container())

        document := Generate(info, router.RouteDefinitions(), registry)

        return melodyhttp.JsonResponse(nethttp.StatusOK, document)
    }
}
