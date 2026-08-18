package security

import (
    "fmt"

    eventcontract "github.com/precision-soft/melody/v2/event/contract"
    "github.com/precision-soft/melody/v2/exception"
    exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
    "github.com/precision-soft/melody/v2/http"
    httpcontract "github.com/precision-soft/melody/v2/http/contract"
    "github.com/precision-soft/melody/v2/internal"
    kernelcontract "github.com/precision-soft/melody/v2/kernel/contract"
    "github.com/precision-soft/melody/v2/logging"
    loggingcontract "github.com/precision-soft/melody/v2/logging/contract"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v2/security/contract"
)

/* logAuthorizationRefusal files the one record an authorization refusal leaves, naming the branch that refused. The direct 401 branches answer the request themselves, without the kernel.exception dispatch their 403 sibling travels through; the 403 marks its error as already logged before that dispatch, so both shapes leave exactly one record. */
func logAuthorizationRefusal(runtimeInstance runtimecontract.Runtime, request httpcontract.Request, reason string) {
    logAuthorizationRefusalAtLevel(runtimeInstance, request, reason, loggingcontract.LevelWarning, nil)
}

/* authorizationRefusalLevel answers the level a refusal is filed at. A refusal is a client outcome and is recorded at warning, the level the exception listener gives every deliberate 4xx. The one branch that is not a client outcome is a firewall whose attribute no configured voter looks at: a wiring fault answered fail-closed with the same 403, which nothing about the request can repair. A reason this package did not write — a decision manager of the application's own — is a refusal until it says otherwise. */
func authorizationRefusalLevel(reason string) loggingcontract.Level {
    if RefusalReasonNoVoterSupportsAttribute == reason {
        return loggingcontract.LevelError
    }

    return loggingcontract.LevelWarning
}

/* authorizationRefusalReason reads the branch out of a refusal the decision manager produced. A refusal from elsewhere names nothing, and the record says so rather than inventing a branch. */
func authorizationRefusalReason(decisionErr error) string {
    httpException := exception.AsHttpException(decisionErr)
    if nil == httpException {
        return ""
    }

    reasonValue, exists := httpException.Context()["reason"]
    if false == exists {
        return ""
    }

    reason, isString := reasonValue.(string)
    if false == isString {
        return ""
    }

    return reason
}

func logAuthorizationRefusalAtLevel(
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
    reason string,
    level loggingcontract.Level,
    extraContext loggingcontract.Context,
) {
    logger := logging.LoggerFromRuntime(runtimeInstance)
    if nil == logger {
        return
    }

    logContext := loggingcontract.Context{
        "reason": reason,
    }
    if nil != request && nil != request.HttpRequest() {
        logContext["method"] = request.HttpRequest().Method
        logContext["path"] = request.HttpRequest().URL.Path
    }
    if nil != request && nil != request.RequestContext() {
        logContext["requestId"] = request.RequestContext().RequestId()
    }

    for key, value := range extraContext {
        logContext[key] = value
    }

    /* the record goes through the named methods rather than the level-taking door: a logger that decorates Warning — the capture loggers the guards use are one shape of it — is bypassed by Log */
    if loggingcontract.LevelError == level {
        logger.Error("authorization refused", logContext)

        return
    }

    logger.Warning("authorization refused", logContext)
}

/* exceptionResponseOrFailClosed returns the response the kernel.exception dispatch produced, or a generic fail-closed response when no listener produced one: a nil response written back to the request event is read by the kernel as "no decision" and the request would reach the handler despite being refused. */
func exceptionResponseOrFailClosed(exceptionEvent *http.KernelExceptionEvent) httpcontract.Response {
    response := exceptionEvent.Response()
    if nil == response {
        return http.JsonErrorResponse(500, "internal_server_error")
    }

    return response
}

func RegisterKernelAccessControlListener(kernelInstance kernelcontract.Kernel, registry *FirewallRegistry) {
    if nil == registry {
        exception.Panic(
            exception.NewError("the firewall registry is nil for access control listener", nil, nil),
        )
    }

    eventDispatcher := kernelInstance.EventDispatcher()

    accessControlRegistration := eventDispatcher.AddListener(
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

            path := ""
            if nil != requestEvent.Request().HttpRequest() && nil != requestEvent.Request().HttpRequest().URL {
                path = requestEvent.Request().HttpRequest().URL.Path
            }

            securityContext, exists := SecurityContextFromRuntime(runtimeInstance)

            var accessControl *AccessControl
            var accessDecisionManager securitycontract.AccessDecisionManager
            var entryPoint securitycontract.EntryPoint
            var accessDeniedHandler securitycontract.AccessDeniedHandler
            firewallName := ""
            accessControlSource := SourceNone

            if true == exists && nil != securityContext {
                firewall := securityContext.Firewall()

                firewallName = securityContext.Firewall().Name()

                accessControl = firewall.AccessControl()
                accessDecisionManager = firewall.AccessDecisionManager()
                entryPoint = firewall.EntryPoint()
                accessDeniedHandler = firewall.AccessDeniedHandler()
                accessControlSource = SourceFirewall
            }

            if nil == accessControl {
                accessControl = registry.GlobalAccessControl()
                accessControlSource = SourceGlobal
            }

            if nil == accessControl {
                return nil
            }

            matchedRule, attributes, matched := matchAccessControlRule(accessControl, path, accessControlSource, firewallName)
            if false == matched {
                return nil
            }

            if true == exists && nil != securityContext {
                securityContext.SetMatchedRule(matchedRule)
            }

            if true == containsPublicAccessAttribute(attributes) {
                _, eventSecurityAuthorizationGrantedErr := eventDispatcher.DispatchName(
                    runtimeInstance,
                    securitycontract.EventSecurityAuthorizationGranted,
                    NewAuthorizationGrantedEvent(
                        requestEvent.Request(),
                        attributes,
                    ),
                )

                return eventSecurityAuthorizationGrantedErr
            }

            if false == exists || nil == securityContext {
                _, eventSecurityAuthorizationDeniedErr := eventDispatcher.DispatchName(
                    runtimeInstance,
                    securitycontract.EventSecurityAuthorizationDenied,
                    NewAuthorizationDeniedEvent(
                        requestEvent.Request(),
                        attributes,
                        exception.NewError(
                            "unauthorized",
                            exceptioncontract.Context{
                                "reason": "missing_security_context",
                            },
                            nil,
                        ),
                    ),
                )
                if nil != eventSecurityAuthorizationDeniedErr {
                    return eventSecurityAuthorizationDeniedErr
                }

                logAuthorizationRefusal(runtimeInstance, requestEvent.Request(), "missing_security_context")

                requestEvent.SetResponse(
                    http.JsonErrorResponse(
                        401,
                        "unauthorized",
                    ),
                )

                return nil
            }

            token := securityContext.Token()

            if nil == token {
                _, eventSecurityAuthorizationDeniedErr := eventDispatcher.DispatchName(
                    runtimeInstance,
                    securitycontract.EventSecurityAuthorizationDenied,
                    NewAuthorizationDeniedEvent(
                        requestEvent.Request(),
                        attributes,
                        exception.NewError(
                            "unauthorized",
                            exceptioncontract.Context{
                                "reason": "missing_token",
                            },
                            nil,
                        ),
                    ),
                )
                if nil != eventSecurityAuthorizationDeniedErr {
                    return eventSecurityAuthorizationDeniedErr
                }

                logAuthorizationRefusal(runtimeInstance, requestEvent.Request(), "missing_token")

                requestEvent.SetResponse(
                    http.JsonErrorResponse(
                        401,
                        "unauthorized",
                    ),
                )

                return nil
            }

            if false == token.IsAuthenticated() {
                _, eventSecurityAuthorizationDeniedErr := eventDispatcher.DispatchName(
                    runtimeInstance,
                    securitycontract.EventSecurityAuthorizationDenied,
                    NewAuthorizationDeniedEvent(
                        requestEvent.Request(),
                        attributes,
                        exception.NewError(
                            "unauthorized",
                            exceptioncontract.Context{
                                "reason": "token_not_authenticated",
                            },
                            nil,
                        ),
                    ),
                )
                if nil != eventSecurityAuthorizationDeniedErr {
                    return eventSecurityAuthorizationDeniedErr
                }

                logAuthorizationRefusal(runtimeInstance, requestEvent.Request(), "token_not_authenticated")

                if nil != entryPoint {
                    response, startErr := entryPoint.Start(runtimeInstance, requestEvent.Request())
                    if nil != startErr {
                        exceptionEvent := http.NewKernelExceptionEvent(runtimeInstance, requestEvent.Request(), startErr)

                        _, eventSecurityAuthorizationDeniedErr = eventDispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelException, exceptionEvent)
                        if nil != eventSecurityAuthorizationDeniedErr {
                            return eventSecurityAuthorizationDeniedErr
                        }

                        requestEvent.SetResponse(exceptionResponseOrFailClosed(exceptionEvent))
                        return nil
                    }

                    /* an entry point that produced no response must not let the request through: fall through to the fail-closed 401 rather than writing a nil response the kernel reads as "no decision" */
                    if nil != response {
                        requestEvent.SetResponse(response)
                        return nil
                    }
                }

                requestEvent.SetResponse(
                    http.JsonErrorResponse(
                        401,
                        "unauthorized",
                    ),
                )

                return nil
            }

            /* the typed nil is judged here too, not only refused at compile: NewCompiledFirewall is a public door and a registry assembled through it never passes the compile step, so a plain comparison would let a manager holding nothing through to DecideAll and answer every request behind the firewall with a recovered panic instead of the missing-manager response this branch exists to give */
            if true == internal.IsNilInterface(accessDecisionManager) {
                exceptionEvent := http.NewKernelExceptionEvent(
                    runtimeInstance,
                    requestEvent.Request(),
                    exception.NewError("security access decision manager is missing", nil, nil),
                )

                _, eventKernelExceptionErr := eventDispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelException, exceptionEvent)
                if nil != eventKernelExceptionErr {
                    return eventKernelExceptionErr
                }

                requestEvent.SetResponse(exceptionResponseOrFailClosed(exceptionEvent))

                return nil
            }

            decisionErr := accessDecisionManager.DecideAll(token, attributes, requestEvent.Request())
            if nil == decisionErr {
                _, eventSecurityAuthorizationGrantedErr := eventDispatcher.DispatchName(
                    runtimeInstance,
                    securitycontract.EventSecurityAuthorizationGranted,
                    NewAuthorizationGrantedEvent(
                        requestEvent.Request(),
                        attributes,
                    ),
                )

                return eventSecurityAuthorizationGrantedErr
            }

            /* the refusal leaves exactly one record, filed on whichever exit the path takes, and that record says what the denied handler did with it. It cannot be filed before the handler runs and it cannot be filed only after: a handler that answers the request returns early and dispatches no kernel.exception, so its exit used to complete without a trace of the refusal it had just answered, while a handler that FAILS is a permanently broken refusal page whose failure reached no record at all — the mark set below suppresses the exception listener that used to file the wrap carrying it. Both exits file here, and the one carrying a broken handler is filed at error with the handler's own outcome named, whatever level the decision itself earned. */
            refusalReason := authorizationRefusalReason(decisionErr)
            refusalLevel := authorizationRefusalLevel(refusalReason)
            refusalContext := loggingcontract.Context{
                "attributes":    attributes,
                "firewallName":  firewallName,
                "matchedRule":   "",
                "matchedSource": string(accessControlSource),
            }
            if nil != matchedRule {
                refusalContext["matchedRule"] = matchedRule.PathPrefix()
            }

            if nil != accessDeniedHandler {
                response, handlerErr := accessDeniedHandler.Handle(runtimeInstance, requestEvent.Request(), decisionErr)
                if nil == handlerErr && nil != response {
                    logAuthorizationRefusalAtLevel(
                        runtimeInstance,
                        requestEvent.Request(),
                        refusalReason,
                        refusalLevel,
                        refusalContext,
                    )

                    requestEvent.SetResponse(response)

                    return nil
                }

                if nil == handlerErr && nil == response {
                    refusalContext["deniedHandlerOutcome"] = "nil_response"
                    refusalLevel = loggingcontract.LevelError

                    decisionErr = exception.NewError(
                        "access denied handler returned nil response",
                        exceptioncontract.Context{
                            "reason": "access_denied_handler_nil_response",
                        },
                        decisionErr,
                    )
                }

                if nil != handlerErr {
                    refusalContext["deniedHandlerOutcome"] = "failed"
                    refusalContext["handlerError"] = handlerErr.Error()
                    refusalLevel = loggingcontract.LevelError

                    /* keep the authorization decision as the cause so the exception listener still resolves the denial status through the cause chain: replacing it with the handler error turns a 403 into whatever the handler failure maps to, usually a 500, and drops the refused attributes */
                    decisionErr = exception.NewError(
                        "access denied handler failed",
                        exceptioncontract.Context{
                            "reason":       "access_denied_handler_failed",
                            "handlerError": handlerErr.Error(),
                        },
                        decisionErr,
                    )
                }
            }

            logAuthorizationRefusalAtLevel(
                runtimeInstance,
                requestEvent.Request(),
                refusalReason,
                refusalLevel,
                refusalContext,
            )

            _, eventSecurityAuthorizationDeniedErr := eventDispatcher.DispatchName(
                runtimeInstance,
                securitycontract.EventSecurityAuthorizationDenied,
                NewAuthorizationDeniedEvent(
                    requestEvent.Request(),
                    attributes,
                    decisionErr,
                ),
            )
            if nil != eventSecurityAuthorizationDeniedErr {
                return eventSecurityAuthorizationDeniedErr
            }

            /* the mark rides the value the dispatch carries, after every wrap: MarkLogged marks the nearest AlreadyLogged implementer in the chain and IsAlreadyLogged reads it at that depth, so a wrapper added after the mark is an unmarked link in front of it and the exception listener would file the second record this listener has already taken responsibility for */
            decisionErr = exception.Logged(decisionErr)

            exceptionEvent := http.NewKernelExceptionEvent(runtimeInstance, requestEvent.Request(), decisionErr)

            _, eventKernelExceptionErr := eventDispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelException, exceptionEvent)
            if nil != eventKernelExceptionErr {
                return eventKernelExceptionErr
            }

            requestEvent.SetResponse(exceptionResponseOrFailClosed(exceptionEvent))

            return nil
        },
        KernelAccessControlListenerPriority,
    )

    /* mark access control as a required kernel.request listener: if another listener stops propagation before it runs, the dispatch fails closed rather than letting the request reach the handler with access control silently skipped. The capability is optional, so a dispatcher of the application's own still registers the listener — but it is what ARMS the fail-closed guarantee, and a dispatcher that does not carry it disarms the guarantee for the whole process. That is said out loud, naming the dispatcher, the way the framework's own adapter refuses the same condition rather than swallowing it: the record goes to the emergency channel because this runs at boot, before any resolution of the configured logger. */
    registrar, ok := eventDispatcher.(eventcontract.RequiredListenerRegistrar)
    if false == ok {
        logging.EmergencyLogger().Warning(
            "the event dispatcher cannot mark the access control listener required",
            loggingcontract.Context{
                "dispatcherType": fmt.Sprintf("%T", eventDispatcher),
                "consequence":    "a listener that stops propagation before access control lets the request reach its handler unchecked",
            },
        )

        return
    }

    registrar.MarkListenerRequired(accessControlRegistration)
}

func matchAccessControlRule(accessControl *AccessControl, path string, source Source, firewallName string) (*MatchedAccessControlRule, []string, bool) {
    if nil == accessControl {
        return nil, nil, false
    }

    matchedIndex, matched := accessControl.matchRuleIndex(path)
    if false == matched {
        return nil, nil, false
    }

    matchedRuleValue := accessControl.Rules()[matchedIndex]

    matchedRule := NewMatchedAccessControlRule(
        matchedRuleValue.pathPrefix,
        matchedRuleValue.attributes,
        source,
        matchedIndex,
        firewallName,
    )

    return matchedRule, matchedRule.Attributes(), true
}

func containsPublicAccessAttribute(attributes []string) bool {
    for _, attribute := range attributes {
        if securitycontract.AttributePublicAccess == attribute {
            return true
        }
    }

    return false
}
