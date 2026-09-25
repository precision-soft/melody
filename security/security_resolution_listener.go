package security

import (
    "fmt"
    nethttp "net/http"
    "runtime/debug"

    eventcontract "github.com/precision-soft/melody/event/contract"
    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    "github.com/precision-soft/melody/http"
    "github.com/precision-soft/melody/internal"
    kernelcontract "github.com/precision-soft/melody/kernel/contract"
    "github.com/precision-soft/melody/logging"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
    securitycontract "github.com/precision-soft/melody/security/contract"
)

func RegisterKernelSecurityResolutionListener(kernelInstance kernelcontract.Kernel, registry *FirewallRegistry) {
    if nil == registry {
        exception.Panic(exception.NewError("firewall registry is nil for security resolution listener", nil, nil))
    }

    eventDispatcher := kernelInstance.EventDispatcher()

    eventDispatcher.AddListener(
        kernelcontract.EventKernelRequest,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            requestEvent, ok := eventValue.Payload().(*http.KernelRequestEvent)
            if false == ok {
                return nil
            }

            /* IsNilInterface: a nil pointer of a request type is a non-nil interface, and the firewall walk below dereferences it */
            if nil == requestEvent || true == internal.IsNilInterface(requestEvent.Request()) {
                return nil
            }

            if nil != requestEvent.Response() {
                return nil
            }

            firewall, matched := registry.Match(requestEvent.Request())
            if false == matched || nil == firewall {
                return nil
            }

            firewallRules := firewall.Rules()
            if 0 != len(firewallRules) {
                firewallInstance := NewFirewall(firewallRules...)
                checkErr := firewallInstance.Check(requestEvent.Request())
                if nil != checkErr {
                    setSecurityContextOnRuntime(runtimeInstance, firewall, NewAnonymousToken())

                    exceptionEvent := http.NewKernelExceptionEvent(runtimeInstance, requestEvent.Request(), checkErr)

                    _, dispatchErr := eventDispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelException, exceptionEvent)
                    if nil != dispatchErr {
                        return dispatchErr
                    }

                    requestEvent.SetResponse(exceptionResponseOrFailClosed(exceptionEvent))
                    return nil
                }
            }

            token, resolveErr := resolveTokenSourceSafely(firewall, runtimeInstance, requestEvent)

            if nil != resolveErr {
                setSecurityContextOnRuntime(runtimeInstance, firewall, NewAnonymousToken())

                /* normalized before the record is written, so the logged mark sticks to the error and the kernel exception listener does not file the same failure again */
                resolveErr = exception.FromError(resolveErr)

                logger := logging.LoggerFromRuntime(runtimeInstance)
                if nil != logger {
                    method := ""
                    path := ""
                    if nil != requestEvent.Request().HttpRequest() {
                        method = requestEvent.Request().HttpRequest().Method
                        if nil != requestEvent.Request().HttpRequest().URL {
                            path = requestEvent.Request().HttpRequest().URL.Path
                        }
                    }

                    /* a token the client got wrong surfaces as a sub-500 HttpException and is recorded at warning, like its api-key sibling; a backend that is down, or any error that is not a deliberate 4xx, keeps the error level. The switch calls the named level methods, so a logger that overrides only one of them is honoured. */
                    resolutionMessage := "security token source resolution failed"
                    resolutionContext := exception.LogContext(
                        resolveErr,
                        exceptioncontract.Context{
                            "method": method,
                            "path":   path,
                        },
                    )

                    httpException := exception.AsHttpException(resolveErr)
                    if nil != httpException && nethttp.StatusInternalServerError > httpException.StatusCode() {
                        logger.Warning(resolutionMessage, resolutionContext)
                    } else {
                        logger.Error(resolutionMessage, resolutionContext)
                    }

                    _ = exception.MarkLogged(resolveErr)
                }

                exceptionEvent := http.NewKernelExceptionEvent(runtimeInstance, requestEvent.Request(), resolveErr)

                _, dispatchErr := eventDispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelException, exceptionEvent)
                if nil != dispatchErr {
                    return dispatchErr
                }

                requestEvent.SetResponse(exceptionResponseOrFailClosed(exceptionEvent))
                return nil
            }

            /* a typed nil token from the application's source is the nil it means, not a live token published into the security context */
            if true == internal.IsNilInterface(token) {
                token = NewAnonymousToken()
            }

            setSecurityContextOnRuntime(runtimeInstance, firewall, token)

            return nil
        },
        KernelFirewallListenerPriority,
    )
}

func setSecurityContextOnRuntime(
    runtimeInstance runtimecontract.Runtime,
    firewall *CompiledFirewall,
    token securitycontract.Token,
) {
    securityContext := NewSecurityContext(firewall, token)

    SecurityContextSetOnRuntime(runtimeInstance, securityContext)
}

func resolveTokenSourceSafely(
    firewall *CompiledFirewall,
    runtimeInstance runtimecontract.Runtime,
    requestEvent *http.KernelRequestEvent,
) (token securitycontract.Token, resolveErr error) {
    tokenSource := firewall.TokenSource()

    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            return
        }

        var recoveredErr error
        if err, ok := recoveredValue.(error); true == ok {
            recoveredErr = err
        }

        /* the recovered value may be a nil token source panicking on Resolve: its name is read only when it is present, or the deferred function would panic again after recover */
        tokenSourceName := ""
        if false == internal.IsNilInterface(tokenSource) {
            tokenSourceName = tokenSource.Name()
        }

        resolveErr = exception.NewError(
            "security token source panicked during resolution",
            exceptioncontract.Context{
                "firewallName":    firewall.Name(),
                "tokenSourceName": tokenSourceName,
                "panicType":       fmt.Sprintf("%T", recoveredValue),
                "panicValue":      fmt.Sprintf("%v", recoveredValue),
                "panicStack":      string(debug.Stack()),
            },
            recoveredErr,
        )
    }()

    token, resolveErr = tokenSource.Resolve(runtimeInstance, requestEvent.Request())

    return token, resolveErr
}
