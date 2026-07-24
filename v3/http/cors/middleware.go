package cors

import (
    nethttp "net/http"

    "github.com/precision-soft/melody/v3/http"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func Middleware(service *Service) httpcontract.Middleware {
    if nil == service {
        service = DefaultService()
    }

    return func(next httpcontract.Handler) httpcontract.Handler {
        return func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            origin := service.RequestOrigin(request)
            allowOrigin := "" != origin && true == service.OriginAllowed(origin)

            if true == allowOrigin && true == service.IsPreflight(request) {
                response := http.EmptyResponse(nethttp.StatusNoContent)
                service.ApplyPreflightHeaders(origin, response.Headers())

                return response, nil
            }

            response, nextMiddlewareErr := next(runtimeInstance, writer, request)
            if nil == response {
                return response, nextMiddlewareErr
            }

            if nil == response.Headers() {
                response.SetHeaders(make(nethttp.Header))
            }

            /* @important emitted on every path so a shared cache cannot serve an origin-less body to an allowed origin */
            addVaryOrigin(response.Headers())

            if true == allowOrigin {
                service.ApplyResponseHeaders(origin, response.Headers())
            }

            return response, nextMiddlewareErr
        }
    }
}

func DefaultMiddleware() httpcontract.Middleware {
    return Middleware(DefaultService())
}

func Restrictive(allowedOrigins ...string) httpcontract.Middleware {
    return Middleware(RestrictiveService(allowedOrigins))
}
