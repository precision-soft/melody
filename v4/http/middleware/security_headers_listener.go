package middleware

import (
    nethttp "net/http"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    eventcontract "github.com/precision-soft/melody/v4/event/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/http"
    "github.com/precision-soft/melody/v4/internal"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
)

/* SecurityHeadersResponseListenerPriority runs the listener after the cors response listener (-100) and every listener at the default priority. */
const SecurityHeadersResponseListenerPriority = -200

/* RegisterSecurityHeadersResponseListener decorates every response the kernel publishes through kernel.response, the ones a kernel.request listener produced and the error pages included, adding headers only. It carries the melody.security_headers tag, which the application declares on kernel.response, so it still runs after a listener ahead of it failed; the 500 the write step substitutes after that publish goes out undecorated. A nil security headers value is refused. */
func RegisterSecurityHeadersResponseListener(eventDispatcher eventcontract.EventDispatcher, securityHeaders *SecurityHeaders) {
    if nil == securityHeaders {
        exception.Panic(
            exception.NewError(
                "the security headers are nil",
                exceptioncontract.Context{
                    "argument": "securityHeaders",
                    "door":     "RegisterSecurityHeadersResponseListener",
                },
                nil,
            ),
        )
    }

    eventDispatcher.AddListener(
        kernelcontract.EventKernelResponse,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            responseEvent, ok := eventValue.Payload().(*http.KernelResponseEvent)
            if false == ok || nil == responseEvent {
                return nil
            }

            response := responseEvent.Response()
            if true == internal.IsNilInterface(response) {
                return nil
            }

            if nil == response.Headers() {
                response.SetHeaders(make(nethttp.Header))
            }

            securityHeaders.Apply(response.Headers(), responseEvent.Request())

            return nil
        },
        eventcontract.ListenerConfig{Priority: SecurityHeadersResponseListenerPriority, Tags: []string{kernelcontract.ListenerTagKernelSecurityHeaders}},
    )
}
