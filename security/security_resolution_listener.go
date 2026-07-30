package security

import (
    "fmt"
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

            if nil == requestEvent || nil == requestEvent.Request() {
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

                logger := logging.LoggerFromRuntime(runtimeInstance)
                if nil != logger {
                    logger.Error(
                        "security token source resolution failed",
                        exception.LogContext(resolveErr),
                    )
                }

                exceptionEvent := http.NewKernelExceptionEvent(runtimeInstance, requestEvent.Request(), resolveErr)

                _, dispatchErr := eventDispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelException, exceptionEvent)
                if nil != dispatchErr {
                    return dispatchErr
                }

                requestEvent.SetResponse(exceptionResponseOrFailClosed(exceptionEvent))
                return nil
            }

            if nil == token {
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

        /* @important the recovered value may itself be a nil token source panicking on Resolve: read its name only when it is present, or the deferred function panics a second time after recover and the original diagnostic escapes unrecovered */
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
