package middleware

import (
    "context"
    nethttp "net/http"

    eventcontract "github.com/precision-soft/melody/v2/event/contract"
    "github.com/precision-soft/melody/v2/exception"
    exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
    "github.com/precision-soft/melody/v2/http"
    httpcontract "github.com/precision-soft/melody/v2/http/contract"
    "github.com/precision-soft/melody/v2/internal"
    kernelcontract "github.com/precision-soft/melody/v2/kernel/contract"
    "github.com/precision-soft/melody/v2/logging"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

/* RateLimitRequestListenerPriority places the limiter ahead of the security chain: token resolution 50, access control 20. */
const RateLimitRequestListenerPriority = 200

/* RegisterRateLimitRequestListener shares the middleware's configuration: registering both meters a request once per door. */
func RegisterRateLimitRequestListener(
    eventDispatcher eventcontract.EventDispatcher,
    config *RateLimitConfig,
) {
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
            key := config.KeyExtractor()(request)

            allowed := false
            if runtimeLimiter, isRuntimeLimiter := config.Limiter().(httpcontract.RuntimeRateLimiter); true == isRuntimeLimiter {
                var allowErr error
                allowed, allowErr = runtimeLimiter.AllowWithRuntime(runtimeInstance, key)
                if nil != allowErr && false == exception.IsAlreadyLogged(allowErr) {
                    /* the returned allowed value already reflects the limiter's failure policy; the listener only reports the store failure */
                    logger := logging.LoggerFromRuntime(runtimeInstance)
                    if nil != logger {
                        if true == exception.ChainIs(allowErr, context.Canceled) {
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

            /* dispatched here: a returned error would abort kernel.request onto its fail-closed 500, and a deliberate 429 becomes 500 */
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

            if false == internal.IsNilInterface(response) {
                requestEvent.SetResponse(response)

                return nil
            }

            requestEvent.SetResponse(http.JsonErrorResponse(nethttp.StatusTooManyRequests, "too many requests"))

            return nil
        },
        RateLimitRequestListenerPriority,
    )
}
