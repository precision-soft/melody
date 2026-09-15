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

/* RegisterRateLimitRequestListener meters kernel.request before authentication and access control, including refused credentials. The default key is the client address. Registering handler rate limiting too charges both budgets; use separate budgets or only one entry point. */
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
