package security

import (
    eventcontract "github.com/precision-soft/melody/v3/event/contract"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/precision-soft/melody/v3/http"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal"
    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

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

            /* IsNilInterface on the request and not `nil ==`: a nil pointer of a request type is a non-nil
            interface a bare check reads as a live request, and the path read below dereferences it. */
            if nil == requestEvent || true == internal.IsNilInterface(requestEvent.Request()) {
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

                requestEvent.SetResponse(
                    http.JsonErrorResponse(
                        401,
                        "unauthorized",
                    ),
                )

                return nil
            }

            token := securityContext.Token()

            /* IsNilInterface and not `nil ==`: the token is the application's token source's, so a nil pointer of its own token type is a non-nil interface a bare check reads as an authenticated caller — and the decision path below dereferences it */
            if true == internal.IsNilInterface(token) {
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

                /* IsNilInterface and not `nil !=`: the entry point comes through NewCompiledFirewall unvalidated, so a typed nil of the application's own type is a non-nil interface this branch takes for a live entry point, and Start below dereferences it on the unauthenticated path */
                if false == internal.IsNilInterface(entryPoint) {
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

                    /* an entry point that produced no response must not let the request through: fall through to the fail-closed 401 rather than writing a nil response the kernel reads as "no decision". IsNilInterface and not `nil !=`: the entry point is the application's, so a typed nil of its own response type is a non-nil interface a bare check would carry through, and SetResponse normalizes it back to the nil that lets the request past authentication. */
                    if false == internal.IsNilInterface(response) {
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

            /* IsNilInterface and not `nil !=`: the same reading the response below already gets, applied to the handler that produces it — it arrives through NewCompiledFirewall unvalidated, so a typed nil is a non-nil interface this branch takes for a live handler and Handle dereferences it on the REFUSAL path, the least exercised one before production */
            if false == internal.IsNilInterface(accessDeniedHandler) {
                response, handlerErr := accessDeniedHandler.Handle(runtimeInstance, requestEvent.Request(), decisionErr)
                /* IsNilInterface and not `nil !=`/`nil ==`: the handler is the application's, so a typed nil of its own response type is a non-nil interface a bare check reads as a live response — SetResponse then normalizes it to nil and the denial is served as a granted request. The nil-response branch below must catch the same typed nil to raise its refusal. */
                if nil == handlerErr && false == internal.IsNilInterface(response) {
                    requestEvent.SetResponse(response)
                    return nil
                }

                if nil == handlerErr && true == internal.IsNilInterface(response) {
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

    matchedIndex, matched := accessControl.MatchRuleIndex(path)
    if false == matched {
        return nil, nil, false
    }

    matchedRuleValue := accessControl.Rules()[matchedIndex]

    matchedRule := NewMatchedAccessControlRule(
        matchedRuleValue.PathPrefix(),
        matchedRuleValue.Attributes(),
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
