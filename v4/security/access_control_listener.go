package security

import (
    "errors"
    "fmt"
    nethttp "net/http"

    eventcontract "github.com/precision-soft/melody/v4/event/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/http"
    "github.com/precision-soft/melody/v4/internal"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

func newAnonymousRefusal(reason string) error {
    refusal := exception.NewHttpException(nethttp.StatusUnauthorized, "unauthorized")
    refusal.SetContextValue("reason", reason)

    return refusal
}

/* authenticationUnavailableCause puts ErrAuthenticationUnavailable before the cause in one chain, so the refusal reads one line and its record names each link once */
type authenticationUnavailableCause struct {
    cause error
}

func (instance *authenticationUnavailableCause) Error() string {
    return ErrAuthenticationUnavailable.Error()
}

func (instance *authenticationUnavailableCause) Is(target error) bool {
    return ErrAuthenticationUnavailable == target
}

func (instance *authenticationUnavailableCause) Unwrap() error {
    return instance.cause
}

/* newUnavailableRefusal builds the 503 an unavailable token earns: ErrAuthenticationUnavailable and the cause in its chain, the firewall's Retry-After, and marked logged, since its cause was filed where it was read: by the security resolution listener, by the sign-in door or by its own producer */
func newUnavailableRefusal(firewall *CompiledFirewall, cause error) error {
    refusal := exception.NewHttpError(
        nethttp.StatusServiceUnavailable,
        exception.HttpErrorConfig{
            Message:    "service unavailable",
            Cause:      &authenticationUnavailableCause{cause: cause},
            RetryAfter: firewall.UnavailableRetryAfter(),
        },
    )
    refusal.SetContextValue("reason", "authentication_unavailable")
    refusal.SetContextValue("firewallName", firewall.Name())

    _ = exception.MarkLogged(refusal)

    return refusal
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

            if nil != requestEvent.Response() || nil != requestEvent.Refusal() {
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

                requestEvent.Refuse(newAnonymousRefusal("missing_security_context"))

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

                requestEvent.Refuse(newAnonymousRefusal("missing_token"))

                return nil
            }

            /* read before IsAuthenticated, so a token that says both is refused rather than served; the entry point is not consulted, there is no sign-in to start */
            if cause, unavailable := AuthenticationUnavailableCause(token); true == unavailable {
                _, eventSecurityAuthorizationDeniedErr := eventDispatcher.DispatchName(
                    runtimeInstance,
                    securitycontract.EventSecurityAuthorizationDenied,
                    NewAuthorizationDeniedEvent(
                        requestEvent.Request(),
                        attributes,
                        exception.NewError(
                            "service unavailable",
                            exceptioncontract.Context{
                                "reason": "authentication_unavailable",
                            },
                            cause,
                        ),
                    ),
                )
                if nil != eventSecurityAuthorizationDeniedErr {
                    return eventSecurityAuthorizationDeniedErr
                }

                requestEvent.Refuse(newUnavailableRefusal(securityContext.Firewall(), cause))

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
                        http.CloseDiscardedResponseBody(response, logging.LoggerFromRuntime(runtimeInstance))

                        requestEvent.Refuse(startErr)

                        return nil
                    }

                    /* an entry point that produced no response falls through to the refusal below, since the kernel reads a nil response as "no decision"; IsNilInterface catches the typed nil SetResponse would normalize to nil */
                    if false == internal.IsNilInterface(response) {
                        requestEvent.SetResponse(response)
                        return nil
                    }
                }

                requestEvent.Refuse(newAnonymousRefusal("entry_point_answered_nothing"))

                return nil
            }

            if true == internal.IsNilInterface(accessDecisionManager) {
                requestEvent.Refuse(exception.NewError("security access decision manager is missing", nil, nil))

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
                    http.CloseDiscardedResponseBody(response, logging.LoggerFromRuntime(runtimeInstance))

                    /* both errors are the cause, the authorization decision first, so the renderer resolves the denial status through the chain and the handler's own error stays reachable; the handler error alone would turn a 403 into a 500 */
                    decisionErr = exception.NewError(
                        "access denied handler failed",
                        exceptioncontract.Context{
                            "reason":       "access_denied_handler_failed",
                            "handlerError": handlerErr.Error(),
                        },
                        errors.Join(decisionErr, handlerErr),
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

            requestEvent.Refuse(decisionErr)

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

    matchedRuleValue, matchedIndex, matched := accessControl.MatchRule(path)
    if false == matched {
        return nil, nil, false
    }

    matchedRule := NewMatchedAccessControlRule(
        matchedRuleValue,
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
