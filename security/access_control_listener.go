package security

import (
	"strings"

	eventcontract "github.com/precision-soft/melody/event/contract"
	"github.com/precision-soft/melody/exception"
	exceptioncontract "github.com/precision-soft/melody/exception/contract"
	"github.com/precision-soft/melody/http"
	kernelcontract "github.com/precision-soft/melody/kernel/contract"
	runtimecontract "github.com/precision-soft/melody/runtime/contract"
	securitycontract "github.com/precision-soft/melody/security/contract"
)

func RegisterKernelAccessControlListener(kernelInstance kernelcontract.Kernel, registry *FirewallRegistry) {
	if nil == registry {
		exception.Panic(
			exception.NewError("the firewall registry is nil for access control listener", nil, nil),
		)
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

				if nil != entryPoint {
					response, startErr := entryPoint.Start(runtimeInstance, requestEvent.Request())
					if nil != startErr {
						exceptionEvent := http.NewKernelExceptionEvent(runtimeInstance, requestEvent.Request(), startErr)

						_, eventSecurityAuthorizationDeniedErr = eventDispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelException, exceptionEvent)
						if nil != eventSecurityAuthorizationDeniedErr {
							return eventSecurityAuthorizationDeniedErr
						}

						requestEvent.SetResponse(exceptionEvent.Response())
						return nil
					}

					requestEvent.SetResponse(response)
					return nil
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

				requestEvent.SetResponse(exceptionEvent.Response())

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
					decisionErr = handlerErr
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

			requestEvent.SetResponse(exceptionEvent.Response())

			return nil
		},
		KernelAccessControlListenerPriority,
	)
}

func matchAccessControlRule(accessControl *AccessControl, path string, source Source, firewallName string) (*MatchedAccessControlRule, []string, bool) {
	if nil == accessControl {
		return nil, nil, false
	}

	normalizedPath := strings.TrimSpace(path)
	if "" == normalizedPath {
		normalizedPath = "/"
	}

	bestIndex := -1
	bestPrefixLength := -1

	fallbackIndex := -1

	for index, rule := range accessControl.Rules() {
		if "" == rule.pathPrefix {
			if -1 == fallbackIndex {
				fallbackIndex = index
			}
			continue
		}

		if true == strings.HasPrefix(normalizedPath, rule.pathPrefix) {
			currentLength := len(rule.pathPrefix)

			if bestPrefixLength < currentLength {
				bestPrefixLength = currentLength
				bestIndex = index
			}

			continue
		}
	}

	matchedIndex := bestIndex
	if -1 == matchedIndex {
		matchedIndex = fallbackIndex
	}

	if -1 == matchedIndex {
		return nil, nil, false
	}

	rules := accessControl.Rules()
	matched := rules[matchedIndex]

	matchedRule := NewMatchedAccessControlRule(
		matched.pathPrefix,
		matched.attributes,
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
