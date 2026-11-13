package http

import (
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/config"
    eventcontract "github.com/precision-soft/melody/v4/event/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
)

/* normalizedEventResponse stores a typed-nil response as nil, since every reader of these events asks `nil == Response()`. */
func normalizedEventResponse(response httpcontract.Response) httpcontract.Response {
    if true == internal.IsNilInterface(response) {
        return nil
    }

    return response
}

func NewKernelRequestEvent(
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
) *KernelRequestEvent {
    return &KernelRequestEvent{
        runtimeInstance: runtimeInstance,
        request:         request,
        response:        nil,
    }
}

/* KernelRequestEvent is how a kernel.request listener ends a request before its handler: with a response, its own rendering, or with a refusal, which the kernel renders through kernel.exception so an application's listener decides its body. A listener that does both is a bug, refused by name at the second door, and the kernel answers it fail-closed. */
type KernelRequestEvent struct {
    runtimeInstance runtimecontract.Runtime
    request         httpcontract.Request
    response        httpcontract.Response
    refusal         error
    decisionRefused bool
}

func (instance *KernelRequestEvent) Runtime() runtimecontract.Runtime {
    return instance.runtimeInstance
}

func (instance *KernelRequestEvent) Request() httpcontract.Request {
    return instance.request
}

func (instance *KernelRequestEvent) Response() httpcontract.Response {
    return instance.response
}

func (instance *KernelRequestEvent) SetResponse(response httpcontract.Response) {
    if nil != instance.refusal {
        /* kept so the kernel's fail-closed step closes its body, which nothing else holds */
        instance.response = normalizedEventResponse(response)
        instance.decisionRefused = true

        exception.Panic(
            exception.NewError(
                "a kernel.request listener set a response on a refused request; a listener answers or refuses, and stands aside once Refusal is set",
                exceptioncontract.Context{
                    "door": "KernelRequestEvent.SetResponse",
                },
                nil,
            ),
        )
    }

    instance.response = normalizedEventResponse(response)
}

/* Refuse ends the request with the error: the handler never runs and the kernel raises the error through kernel.exception. The first refusal is the decision, so a later one is ignored. A nil or typed-nil error is refused by name, since reading it as no refusal would serve a request the caller meant to refuse. */
func (instance *KernelRequestEvent) Refuse(err error) {
    if true == internal.IsNilInterface(err) {
        instance.decisionRefused = true

        exception.Panic(
            exception.NewError(
                "a kernel.request listener refused a request with no error",
                exceptioncontract.Context{
                    "door": "KernelRequestEvent.Refuse",
                },
                nil,
            ),
        )
    }

    if nil != instance.response {
        instance.decisionRefused = true

        exception.Panic(
            exception.NewError(
                "a kernel.request listener refused a request that already has a response; a listener answers or refuses, and stands aside once Response is set",
                exceptioncontract.Context{
                    "door": "KernelRequestEvent.Refuse",
                },
                nil,
            ),
        )
    }

    if nil != instance.refusal {
        return
    }

    instance.refusal = err
}

func (instance *KernelRequestEvent) Refusal() error {
    return instance.refusal
}

func (instance *KernelRequestEvent) failClosed(response httpcontract.Response) {
    instance.refusal = nil
    instance.response = normalizedEventResponse(response)
}

func NewKernelControllerEvent(
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
) *KernelControllerEvent {
    return &KernelControllerEvent{
        runtime:  runtimeInstance,
        request:  request,
        response: nil,
    }
}

type KernelControllerEvent struct {
    runtime  runtimecontract.Runtime
    request  httpcontract.Request
    response httpcontract.Response
}

func (instance *KernelControllerEvent) Runtime() runtimecontract.Runtime {
    return instance.runtime
}

func (instance *KernelControllerEvent) Request() httpcontract.Request {
    return instance.request
}

func (instance *KernelControllerEvent) Response() httpcontract.Response {
    return instance.response
}

func (instance *KernelControllerEvent) SetResponse(response httpcontract.Response) {
    instance.response = normalizedEventResponse(response)
}

func NewKernelResponseEvent(request httpcontract.Request, response httpcontract.Response) *KernelResponseEvent {
    return &KernelResponseEvent{
        request:  request,
        response: normalizedEventResponse(response),
    }
}

type KernelResponseEvent struct {
    request  httpcontract.Request
    response httpcontract.Response
}

func (instance *KernelResponseEvent) Request() httpcontract.Request {
    return instance.request
}

func (instance *KernelResponseEvent) Response() httpcontract.Response {
    return instance.response
}

func (instance *KernelResponseEvent) SetResponse(response httpcontract.Response) {
    instance.response = normalizedEventResponse(response)
}

func NewKernelTerminateEvent(
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
    response httpcontract.Response,
) *KernelTerminateEvent {
    return &KernelTerminateEvent{
        runtime:  runtimeInstance,
        request:  request,
        response: normalizedEventResponse(response),
    }
}

type KernelTerminateEvent struct {
    runtime  runtimecontract.Runtime
    request  httpcontract.Request
    response httpcontract.Response
}

func (instance *KernelTerminateEvent) Runtime() runtimecontract.Runtime {
    return instance.runtime
}

func (instance *KernelTerminateEvent) Request() httpcontract.Request {
    return instance.request
}

func (instance *KernelTerminateEvent) Response() httpcontract.Response {
    return instance.response
}

type KernelExceptionEventConfig struct {
    /* UnavailableRetryAfter is the Retry-After the event carries for a marked infrastructure failure: 5s when left out, 1s to 1h in whole seconds, unlimited refused, a refused value a panic naming the field. */
    UnavailableRetryAfter bound.Duration
}

/* NewKernelExceptionEvent builds the event a kernel dispatches on kernel.exception; a kernel of its own passes the delay it was configured with, so the built-in listener serves it. */
func NewKernelExceptionEvent(
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
    err error,
    eventConfig KernelExceptionEventConfig,
) *KernelExceptionEvent {
    unavailableRetryAfter, retryAfterErr := config.ResolveUnavailableRetryAfter(eventConfig.UnavailableRetryAfter, "KernelExceptionEventConfig.UnavailableRetryAfter")
    if nil != retryAfterErr {
        exception.Panic(exception.FromError(retryAfterErr))
    }

    return &KernelExceptionEvent{
        runtime:               runtimeInstance,
        request:               request,
        err:                   err,
        response:              nil,
        unavailableRetryAfter: unavailableRetryAfter,
    }
}

type KernelExceptionEvent struct {
    runtime               runtimecontract.Runtime
    request               httpcontract.Request
    err                   error
    response              httpcontract.Response
    unavailableRetryAfter time.Duration
}

/* UnavailableRetryAfter is the Retry-After a marked infrastructure failure is answered with, the delay the kernel was configured with; a listener that builds its own response sets it with SetRetryAfter, one that answers through RenderExceptionResponse passes it in ExceptionRenderConfig. */
func (instance *KernelExceptionEvent) UnavailableRetryAfter() time.Duration {
    return instance.unavailableRetryAfter
}

func (instance *KernelExceptionEvent) Runtime() runtimecontract.Runtime {
    return instance.runtime
}

func (instance *KernelExceptionEvent) Request() httpcontract.Request {
    return instance.request
}

func (instance *KernelExceptionEvent) Err() error {
    return instance.err
}

func (instance *KernelExceptionEvent) Response() httpcontract.Response {
    return instance.response
}

/* SetResponse answers the event and ends the dispatch: the first responder wins, and a listener that must see every error sits at a higher priority. */
func (instance *KernelExceptionEvent) SetResponse(response httpcontract.Response) {
    instance.response = normalizedEventResponse(response)
}

func (instance *KernelExceptionEvent) IsPropagationStopped() bool {
    return nil != instance.response
}

var _ eventcontract.PropagationStopper = (*KernelExceptionEvent)(nil)
