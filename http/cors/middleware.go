package cors

import (
    nethttp "net/http"

    "github.com/precision-soft/melody/http"
    httpcontract "github.com/precision-soft/melody/http/contract"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
)

/* Middleware decorates the handler path: it answers a preflight and applies the response headers for responses produced inside the middleware chain. A response produced by an event listener — a security refusal, an error page — never enters the chain, so this door never sees it, and a preflight to a path access control protects is refused before the chain is built. An application that needs those covered registers the listener doors through RegisterListeners. */
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

            /* emitted on every path so a shared cache cannot serve an origin-less body to an allowed origin */
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
