package middleware

import (
    nethttp "net/http"

    eventcontract "github.com/precision-soft/melody/v3/event/contract"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/http"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal"
    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* RateLimitRequestListenerPriority places the limiter ahead of token resolution (50) and access control (20), so a request over budget is answered before it pays an authenticator round. */
const RateLimitRequestListenerPriority = 200

/* RegisterRateLimitRequestListener meters every request on kernel.request, before authentication and access control, so a burst the security chain refuses is charged too; RateLimitMiddleware meters only the handler path. The default key is the client address. Both doors share the configuration, so registering both meters a request once per door. */
func RegisterRateLimitRequestListener(
    eventDispatcher eventcontract.EventDispatcher,
    config *RateLimitConfig,
) {
    /* a typed-nil limiter is refused, as the middleware door refuses it */
    if nil == config || true == internal.IsNilInterface(config.Limiter()) {
        exception.Panic(
            exception.NewError("limiter is required for rate limit request listener", nil, nil),
        )
    }

    if nil == config.KeyExtractor() {
        config.SetKeyExtractor(func(request httpcontract.Request) string {
            return config.clientIp(request)
        })
    }

    if nil == config.OnLimitExceeded() {
        config.SetOnLimitExceeded(defaultOnLimitExceeded)
    }

    eventDispatcher.AddListener(
        kernelcontract.EventKernelRequest,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            requestEvent, ok := eventValue.Payload().(*http.KernelRequestEvent)
            if false == ok || nil == requestEvent || true == internal.IsNilInterface(requestEvent.Request()) {
                return nil
            }

            if nil != requestEvent.Response() {
                return nil
            }

            request := requestEvent.Request()

            allowed := allowRequestUnderLimit(config, runtimeInstance, request)

            if true == allowed {
                return nil
            }

            response, limitErr := config.OnLimitExceeded()(request)

            /* the listener dispatches kernel.exception itself, since returning the error would abort the kernel.request dispatch onto its fail-closed 500 */
            if nil != limitErr {
                exceptionEvent := http.NewKernelExceptionEvent(runtimeInstance, request, limitErr)

                _, dispatchErr := eventDispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelException, exceptionEvent)
                if nil != dispatchErr {
                    return dispatchErr
                }

                exceptionResponse := exceptionEvent.Response()
                if nil == exceptionResponse {
                    exceptionResponse = http.JsonErrorResponse(nethttp.StatusTooManyRequests, "too many requests")
                }

                requestEvent.SetResponse(exceptionResponse)

                return nil
            }

            /* IsNilInterface, since the application's handler may answer a typed-nil response, which would leave the refused request served */
            if false == internal.IsNilInterface(response) {
                requestEvent.SetResponse(response)

                return nil
            }

            /* a limit handler that answered neither response nor error still refused the request */
            requestEvent.SetResponse(http.JsonErrorResponse(nethttp.StatusTooManyRequests, "too many requests"))

            return nil
        },
        RateLimitRequestListenerPriority,
    )
}
