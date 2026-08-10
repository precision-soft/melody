package security

import (
    eventcontract "github.com/precision-soft/melody/event/contract"
    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    "github.com/precision-soft/melody/http"
    httpcontract "github.com/precision-soft/melody/http/contract"
    kernelcontract "github.com/precision-soft/melody/kernel/contract"
    "github.com/precision-soft/melody/logging"
    loggingcontract "github.com/precision-soft/melody/logging/contract"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
    securitycontract "github.com/precision-soft/melody/security/contract"
)

/* logAuthorizationRefusal files the one record a direct 401 refusal leaves. These branches answer the request themselves — deliberately, without the kernel.exception dispatch their 403 sibling travels through — and used to complete without a trace: whether the token was absent, unauthenticated, or the security context missing entirely (a wiring fault, not a client mistake) was indistinguishable from the journal, while the byte-identical-looking 403 filed a warning naming its reason. The refusal is recorded at warning, the level the exception listener gives every deliberate 4xx. */
func logAuthorizationRefusal(runtimeInstance runtimecontract.Runtime, request httpcontract.Request, reason string) {
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

            if nil == accessDecisionManager {
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

            if nil != accessDeniedHandler {
                response, handlerErr := accessDeniedHandler.Handle(runtimeInstance, requestEvent.Request(), decisionErr)
                if nil == handlerErr && nil != response {
                    requestEvent.SetResponse(response)
                    return nil
                }

                if nil == handlerErr && nil == response {
                    decisionErr = exception.NewError(
                        "access denied handler returned nil response",
                        exceptioncontract.Context{
                            "reason": "access_denied_handler_nil_response",
                        },
                        decisionErr,
                    )
                }

                if nil != handlerErr {
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

    /* mark access control as a required kernel.request listener: if another listener stops propagation before it runs, the dispatch fails closed rather than letting the request reach the handler with access control silently skipped. A no-op on a dispatcher that does not support required listeners, so this stays optional. */
    if registrar, ok := eventDispatcher.(eventcontract.RequiredListenerRegistrar); true == ok {
        registrar.MarkListenerRequired(accessControlRegistration)
    }
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
