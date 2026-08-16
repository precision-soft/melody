package cors

import (
    nethttp "net/http"

    "github.com/precision-soft/melody/v2/http"
    httpcontract "github.com/precision-soft/melody/v2/http/contract"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
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

            /* a streaming handler (Server-Sent Events, a long poll) commits its headers straight to the writer and returns a response the write path then discards, so the cross-origin headers applied to that response below never reach the connection. Apply them to the writer here, before the handler runs: Vary on every path so a shared cache keys on the origin, and the allow-origin headers when the origin is allowed. The write path replaces these with the response's own copy for an ordinary handler, so its response is decorated once as before. */
            addVaryOrigin(writer.Header())
            if true == allowOrigin {
                service.ApplyResponseHeaders(origin, writer.Header())
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
