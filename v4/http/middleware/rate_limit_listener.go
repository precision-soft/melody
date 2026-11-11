package middleware

import (
    nethttp "net/http"

    eventcontract "github.com/precision-soft/melody/v4/event/contract"
    "github.com/precision-soft/melody/v4/http"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
    "github.com/precision-soft/melody/v4/logging"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
)

/* RateLimitRequestListenerPriority places the limiter ahead of token resolution (50) and access control (20), so a request over budget is answered before it pays an authenticator round. */
const RateLimitRequestListenerPriority = 200

/* RegisterRateLimitRequestListener meters every request still unanswered on kernel.request, before authentication and access control — a request a listener ahead of it already answered is left unmetered —, so a burst the security chain refuses is charged too; RateLimitMiddleware meters only the handler path. The default key is the client address. A nil or typed-nil limiter is refused, and the configuration is read when the listener is registered; registering both doors meters a request once per door. */
func RegisterRateLimitRequestListener(
    eventDispatcher eventcontract.EventDispatcher,
    limiter httpcontract.RateLimiter,
    config RateLimitConfig,
) {
    /* a typed-nil limiter is refused, as the middleware door refuses it */
    gate := newRateLimitGate(limiter, config, "limiter is required for rate limit request listener", "RegisterRateLimitRequestListener")

    eventDispatcher.AddListener(
        kernelcontract.EventKernelRequest,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            requestEvent, ok := eventValue.Payload().(*http.KernelRequestEvent)
            if false == ok || nil == requestEvent || true == internal.IsNilInterface(requestEvent.Request()) {
                return nil
            }

            if nil != requestEvent.Response() || nil != requestEvent.Refusal() {
                return nil
            }

            gate.Apply(runtimeInstance, requestEvent)

            return nil
        },
        RateLimitRequestListenerPriority,
    )
}

/* NewRateLimitGate is the request listener's decision for a door that meters only its own requests on kernel.request. A nil or typed-nil limiter is refused. */
func NewRateLimitGate(limiter httpcontract.RateLimiter, config RateLimitConfig) *RateLimitGate {
    return newRateLimitGate(limiter, config, "limiter is required for rate limit gate", "NewRateLimitGate")
}

func newRateLimitGate(limiter httpcontract.RateLimiter, config RateLimitConfig, refusalMessage string, door string) *RateLimitGate {
    return &RateLimitGate{settings: resolveRateLimitConfig(limiter, config, refusalMessage, door)}
}

type RateLimitGate struct {
    settings rateLimitSettings
}

/* Apply charges the request and reports whether it stops it, over budget or because a store could not answer, in which case the event already carries the answer or the refusal. */
func (instance *RateLimitGate) Apply(runtimeInstance runtimecontract.Runtime, requestEvent *http.KernelRequestEvent) bool {
    request := requestEvent.Request()

    allowed, unavailableErr := allowRequestUnderLimit(instance.settings, runtimeInstance, request)

    /* a denial from a store that could not answer is refused with the marked error, 503 with Retry-After, and the limit handler is not called */
    if nil != unavailableErr {
        requestEvent.Refuse(unavailableErr)

        return true
    }

    if true == allowed {
        return false
    }

    response, limitErr := instance.settings.onLimitExceeded(request)

    if false == internal.IsNilInterface(limitErr) {
        http.CloseDiscardedResponseBody(response, logging.LoggerFromRuntime(runtimeInstance))

        requestEvent.Refuse(limitErr)

        return true
    }

    /* IsNilInterface, since the application's handler may answer a typed-nil response, which would leave the refused request served */
    if false == internal.IsNilInterface(response) {
        requestEvent.SetResponse(response)

        return true
    }

    /* a limit handler that answered neither response nor error still refused the request */
    requestEvent.SetResponse(http.JsonErrorResponse(nethttp.StatusTooManyRequests, "too many requests"))

    return true
}
