package security

import (
    "fmt"

    eventcontract "github.com/precision-soft/melody/v3/event/contract"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/precision-soft/melody/v3/http"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal"
    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
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

            /* IsNilInterface and not `nil ==`: a nil pointer of a request type is a non-nil interface, and the path read below dereferences it */
            if nil == requestEvent || true == internal.IsNilInterface(requestEvent.Request()) {
                return nil
            }

            if nil != requestEvent.Response() {
                return nil
            }

            path := ""
            if nil != requestEvent.Request().HttpRequest() && nil != requestEvent.Request().HttpRequest().URL {
                /* the spelling the router matched, not the decoded URL.Path, so a rule cannot claim "/public%2F" as "/public/" while the router serves it through another route */
                path = http.RequestPathAsRouted(internal.RequestPathAsSent(requestEvent.Request().HttpRequest().URL))
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

            /* IsNilInterface: a typed nil token of the application's own type would read as an authenticated caller */
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

                /* IsNilInterface: the entry point comes through NewCompiledFirewall unvalidated, and Start below dereferences it */
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

                    /* an entry point that produced no response falls through to the fail-closed 401, since the kernel reads a nil response as "no decision"; IsNilInterface catches the typed nil SetResponse would normalize to nil */
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

            /* IsNilInterface: the handler comes through NewCompiledFirewall unvalidated, and Handle dereferences it on the refusal path */
            if false == internal.IsNilInterface(accessDeniedHandler) {
                response, handlerErr := accessDeniedHandler.Handle(runtimeInstance, requestEvent.Request(), decisionErr)
                /* IsNilInterface: SetResponse would normalize a typed nil response to nil and serve the denial as a grant; the nil-response branch below catches the same typed nil */
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
                    /* the authorization decision stays the cause, so the exception listener resolves the denial status through the chain; the handler error alone would turn a 403 into a 500 */
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

    /* access control is a required kernel.request listener: a listener that stops propagation before it makes the dispatch fail closed. A dispatcher without that capability disarms the guarantee for the whole process, which is reported on the emergency channel because this runs before the configured logger is resolved. */
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
