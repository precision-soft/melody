package middleware

import (
    "context"
    "errors"
    nethttp "net/http"

    eventcontract "github.com/precision-soft/melody/v3/event/contract"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/precision-soft/melody/v3/http"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal"
    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
    "github.com/precision-soft/melody/v3/logging"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* RateLimitRequestListenerPriority places the limiter ahead of the security chain: token resolution listens at 50 and access control at 20, so a request over budget is answered before it pays an authenticator round — and before a refusal ends the request without the middleware chain ever being built. */
const RateLimitRequestListenerPriority = 200

/* RegisterRateLimitRequestListener meters every request on kernel.request, before authentication and access control. RateLimitMiddleware meters only what reaches the handler path: a request the security chain refuses is answered before the middleware chain is built, so a burst of wrong credentials consumes no budget there. This door charges that burst and answers it once the budget is gone. The default key is the client address, which exists before any token is resolved; a key extractor reading the authenticated identity falls back the same way the middleware does. Both doors share the configuration, so registering both meters a request once per door — use distinct budgets or one door. */
func RegisterRateLimitRequestListener(
    eventDispatcher eventcontract.EventDispatcher,
    config *RateLimitConfig,
) {
    /* the limiter is read through the interface, the same refusal the middleware door gives a typed nil */
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
            if false == ok || nil == requestEvent || nil == requestEvent.Request() {
                return nil
            }

            if nil != requestEvent.Response() {
                return nil
            }

            request := requestEvent.Request()
            key := config.KeyExtractor()(request)

            allowed := false
            if runtimeLimiter, isRuntimeLimiter := config.Limiter().(httpcontract.RuntimeRateLimiter); true == isRuntimeLimiter {
                var allowErr error
                allowed, allowErr = runtimeLimiter.AllowWithRuntime(runtimeInstance, key)
                if nil != allowErr && false == exception.IsAlreadyLogged(allowErr) {
                    /* the returned allowed value already reflects the limiter's failure policy; the listener only reports the store failure. A failure that is the caller's own cancellation — the client disconnected while the limiter's round trip was in flight — is recorded at warning under its own name, because at error it read as a store outage and paged the operator for a client hanging up. This door meters every request, ahead of authentication, so it sees more of those disconnects than the middleware does. A limiter that filed its own record marks it, and then this is the second copy rather than the only one. */
                    logger := logging.LoggerFromRuntime(runtimeInstance)
                    if nil != logger {
                        if true == errors.Is(allowErr, context.Canceled) {
                            logger.Warning(
                                "rate limiter call cancelled",
                                exception.LogContext(allowErr, exceptioncontract.Context{"key": key}),
                            )
                        } else {
                            logger.Error(
                                "rate limiter store failure",
                                exception.LogContext(allowErr, exceptioncontract.Context{"key": key}),
                            )
                        }
                    }
                }
            } else {
                allowed = config.Limiter().Allow(key)
            }

            if true == allowed {
                return nil
            }

            response, limitErr := config.OnLimitExceeded()(request)

            /* the middleware hands this error to the handler-error path, which renders it through kernel.exception; the listener dispatches the same event itself, because returning the error would abort the kernel.request dispatch onto its fail-closed 500 page and a deliberate 429 would come out a 500 */
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

            if nil != response {
                requestEvent.SetResponse(response)

                return nil
            }

            /* a limit handler that produced neither response nor error still refused the request: answer it rather than serve it unmetered */
            requestEvent.SetResponse(http.JsonErrorResponse(nethttp.StatusTooManyRequests, "too many requests"))

            return nil
        },
        RateLimitRequestListenerPriority,
    )
}
