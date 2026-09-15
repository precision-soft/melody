package security

import (
    "fmt"
    nethttp "net/http"
    "runtime/debug"

    eventcontract "github.com/precision-soft/melody/v3/event/contract"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/precision-soft/melody/v3/http"
    "github.com/precision-soft/melody/v3/internal"
    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
    "github.com/precision-soft/melody/v3/logging"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
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
