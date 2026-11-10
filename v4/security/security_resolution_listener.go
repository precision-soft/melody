package security

import (
    "fmt"
    nethttp "net/http"
    "runtime/debug"

    eventcontract "github.com/precision-soft/melody/v4/event/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/http"
    "github.com/precision-soft/melody/v4/internal"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
    "github.com/precision-soft/melody/v4/logging"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
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

            if nil != requestEvent.Response() || nil != requestEvent.Refusal() {
                return nil
            }

            firewall, matched := registry.Match(requestEvent.Request())
            if false == matched || nil == firewall {
                return nil
            }

            if nil != firewall.firewall {
                checkErr := firewall.firewall.Check(requestEvent.Request())
                if nil != checkErr {
                    setSecurityContextOnRuntime(runtimeInstance, firewall, NewAnonymousToken())

                    requestEvent.Refuse(checkErr)

                    return nil
                }
            }

            token, resolveErr, panicked := resolveTokenSourceSafely(firewall, runtimeInstance, requestEvent)

            /* a marked error is the second spelling of the unavailable token; a panic stays a refusal, whatever it carried, and an http status around the mark is kept */
            if nil != resolveErr && false == panicked && true == answeredAsUnavailable(resolveErr) {
                token = NewUnavailableToken(resolveErr)
                resolveErr = nil
            }

            if nil != resolveErr {
                setSecurityContextOnRuntime(runtimeInstance, firewall, NewAnonymousToken())

                /* normalized before the record is written, so the logged mark sticks to the error and the kernel does not file the same failure again */
                resolveErr = exception.FromError(resolveErr)

                logger := logging.LoggerFromRuntime(runtimeInstance)
                if nil != logger {
                    /* a token the client got wrong surfaces as a sub-500 HttpException and is recorded at warning, like its api-key sibling; a backend that is down, or any error that is not a deliberate 4xx, keeps the error level. The switch calls the named level methods, so a logger that overrides only one of them is honoured. */
                    resolutionMessage := "security token source resolution failed"
                    resolutionContext := resolutionLogContext(resolveErr, requestEvent, nil)

                    httpException := exception.AsHttpException(resolveErr)
                    if nil != httpException && nethttp.StatusInternalServerError > httpException.StatusCode() {
                        logger.Warning(resolutionMessage, resolutionContext)
                    } else {
                        logger.Error(resolutionMessage, resolutionContext)
                    }

                    _ = exception.MarkLogged(resolveErr)
                }

                requestEvent.Refuse(resolveErr)

                return nil
            }

            /* a typed nil token from the application's source is the nil it means, not a live token published into the security context */
            if true == internal.IsNilInterface(token) {
                token = NewAnonymousToken()
            }

            /* the one record of a credential nobody could check, for framework and custom sources alike, unless its producer filed the cause; the refusal access control raises for it is marked logged */
            if cause, unavailable := AuthenticationUnavailableCause(token); true == unavailable && false == exception.IsAlreadyLogged(cause) {
                logger := logging.LoggerFromRuntime(runtimeInstance)
                if nil != logger {
                    logger.Error(
                        "security token source could not verify the credential: its platform is unavailable",
                        resolutionLogContext(
                            cause,
                            requestEvent,
                            exceptioncontract.Context{
                                "firewallName":    firewall.Name(),
                                "tokenSourceName": firewall.TokenSource().Name(),
                            },
                        ),
                    )
                }
            }

            setSecurityContextOnRuntime(runtimeInstance, firewall, token)

            serveFirewallDoor(runtimeInstance, requestEvent, firewall, token)

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
) (token securitycontract.Token, resolveErr error, panicked bool) {
    tokenSource := firewall.TokenSource()

    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            return
        }

        panicked = true

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
                "panicValue":      internal.DescribeRecoveredValue(recoveredValue),
                "panicStack":      string(debug.Stack()),
            },
            recoveredErr,
        )
    }()

    token, resolveErr = tokenSource.Resolve(runtimeInstance, requestEvent.Request())

    return token, resolveErr, false
}

func resolutionLogContext(
    err error,
    requestEvent *http.KernelRequestEvent,
    extra exceptioncontract.Context,
) exceptioncontract.Context {
    method := ""
    path := ""
    if nil != requestEvent.Request().HttpRequest() {
        method = requestEvent.Request().HttpRequest().Method
        if nil != requestEvent.Request().HttpRequest().URL {
            path = requestEvent.Request().HttpRequest().URL.Path
        }
    }

    context := exceptioncontract.Context{
        "method": internal.BoundDiagnosticText(method),
        "path":   internal.BoundDiagnosticText(path),
    }
    for key, value := range extra {
        context[key] = value
    }

    return exception.LogContext(err, exception.LogContextConfig{Extra: context})
}
