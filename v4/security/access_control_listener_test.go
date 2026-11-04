package security

import (
    "errors"
    "io"
    "os"
    "strings"
    "testing"

    eventcontract "github.com/precision-soft/melody/v4/event/contract"
    "github.com/precision-soft/melody/v4/exception"
    httpPkg "github.com/precision-soft/melody/v4/http"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/logging"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
    "github.com/precision-soft/melody/v4/security/accesscontrol"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

/* the two decisions answer separately and each records that it was the one asked: a stub that returns one field for both cannot tell the listener's DecideAll apart from a DecideAny, and every rule in this file carrying a single attribute means the two agree on the input as well. The recorded attributes are the other half — the listener has to hand the rule's whole set to the decision, not the first of them. */
type accessControlListenerTestAccessDecisionManager struct {
    decideAllErr     error
    decideAnyErr     error
    calledMethod     string
    calledAttributes []string
}

func (instance *accessControlListenerTestAccessDecisionManager) DecideAll(token securitycontract.Token, attributes []string, subject any) error {
    instance.calledMethod = "DecideAll"
    instance.calledAttributes = append([]string{}, attributes...)

    return instance.decideAllErr
}

func (instance *accessControlListenerTestAccessDecisionManager) DecideAny(token securitycontract.Token, attributes []string, subject any) error {
    instance.calledMethod = "DecideAny"
    instance.calledAttributes = append([]string{}, attributes...)

    return instance.decideAnyErr
}

type accessControlListenerTestEntryPoint struct {
    response httpcontract.Response
    err      error
    calls    int
}

func (instance *accessControlListenerTestEntryPoint) Start(runtimeInstance runtimecontract.Runtime, request httpcontract.Request) (httpcontract.Response, error) {
    instance.calls++
    return instance.response, instance.err
}

type accessControlListenerTestAccessDeniedHandler struct {
    response httpcontract.Response
    err      error
    calls    int
}

func (instance *accessControlListenerTestAccessDeniedHandler) Handle(runtimeInstance runtimecontract.Runtime, request httpcontract.Request, decisionErr error) (httpcontract.Response, error) {
    instance.calls++
    return instance.response, instance.err
}

func TestMatchAccessControlRule_SetsMetadataCorrectly(t *testing.T) {
    control := NewAccessControl([]AccessControlRule{
        accesscontrol.NewRawPrefixRule("/admin", accesscontrol.RuleConfig{Attributes: []string{"ROLE_ADMIN"}}),
        accesscontrol.NewRawPrefixRule("/admin/settings", accesscontrol.RuleConfig{Attributes: []string{"ROLE_SETTINGS"}}),
    })

    matchedRule, attributes, matched := matchAccessControlRule(control, "/admin/settings/users", SourceFirewall, "main")
    if false == matched {
        t.Fatalf("expected matched")
    }
    if nil == matchedRule {
        t.Fatalf("expected matched rule")
    }

    if "/admin/settings" != matchedRule.PathPrefix() {
        t.Fatalf("unexpected path prefix")
    }
    if SourceFirewall != matchedRule.Source() {
        t.Fatalf("unexpected source")
    }
    if "main" != matchedRule.Firewall() {
        t.Fatalf("unexpected firewall")
    }
    if 1 != matchedRule.RuleIndex() {
        t.Fatalf("unexpected rule index")
    }

    if 1 != len(attributes) {
        t.Fatalf("expected one attribute")
    }
    if "ROLE_SETTINGS" != attributes[0] {
        t.Fatalf("unexpected attribute")
    }
}

func TestMatchAccessControlRule_NormalizesEmptyPathToRoot(t *testing.T) {
    control := NewAccessControl([]AccessControlRule{
        accesscontrol.NewRawPrefixRule("/", accesscontrol.RuleConfig{Attributes: []string{"ROLE_ROOT"}}),
    })

    matchedRule, attributes, matched := matchAccessControlRule(control, "", SourceGlobal, "")
    if false == matched {
        t.Fatalf("expected matched")
    }
    if nil == matchedRule {
        t.Fatalf("expected matched rule")
    }
    if "/" != matchedRule.PathPrefix() {
        t.Fatalf("unexpected path prefix")
    }
    if 0 != matchedRule.RuleIndex() {
        t.Fatalf("unexpected rule index")
    }
    if 1 != len(attributes) {
        t.Fatalf("expected one attribute")
    }
    if "ROLE_ROOT" != attributes[0] {
        t.Fatalf("unexpected attribute")
    }
}

func TestMatchAccessControlRule_FallbackRuleSelectedOnce(t *testing.T) {
    control := NewAccessControl([]AccessControlRule{
        accesscontrol.NewRawPrefixRule("", accesscontrol.RuleConfig{Attributes: []string{"ROLE_ANY"}}),
        accesscontrol.NewRawPrefixRule("/admin", accesscontrol.RuleConfig{Attributes: []string{"ROLE_ADMIN"}}),
    })

    matchedRule, attributes, matched := matchAccessControlRule(control, "/public", SourceGlobal, "")
    if false == matched {
        t.Fatalf("expected matched")
    }
    if nil == matchedRule {
        t.Fatalf("expected matched rule")
    }
    if "" != matchedRule.PathPrefix() {
        t.Fatalf("unexpected path prefix")
    }
    if 0 != matchedRule.RuleIndex() {
        t.Fatalf("unexpected rule index")
    }
    if 1 != len(attributes) {
        t.Fatalf("expected one attribute")
    }
    if "ROLE_ANY" != attributes[0] {
        t.Fatalf("unexpected attribute")
    }
}

func TestMatchAccessControlRule_RegexRuleIsHonoredAndNotShadowedByEarlierRegex(t *testing.T) {
    accessControl := NewAccessControl([]AccessControlRule{
        accesscontrol.NewRegexRule("^/health(/|$)", accesscontrol.RuleConfig{Attributes: []string{securitycontract.AttributePublicAccess}}),
        accesscontrol.NewRegexRule("^/admin", accesscontrol.RuleConfig{Attributes: []string{"ROLE_ADMIN"}}),
    })

    matchedRule, attributes, matched := matchAccessControlRule(accessControl, "/admin/secret", SourceFirewall, "main")
    if false == matched {
        t.Fatalf("expected a matched rule")
    }
    if 1 != len(attributes) || "ROLE_ADMIN" != attributes[0] {
        t.Fatalf("expected ROLE_ADMIN, got %v", attributes)
    }
    if 1 != matchedRule.RuleIndex() {
        t.Fatalf("expected the second regex rule (index 1), got %d", matchedRule.RuleIndex())
    }
}

func TestMatchAccessControlRule_TheRecordNamesARegexRuleApartFromARawFallback(t *testing.T) {
    accessControl := NewAccessControl([]AccessControlRule{
        accesscontrol.NewRegexRule("^/admin", accesscontrol.RuleConfig{Attributes: []string{"ROLE_ADMIN"}}),
        accesscontrol.NewRawPrefixRule("", accesscontrol.RuleConfig{Attributes: []string{"ROLE_ANY"}}),
    })

    regexRecord, regexAttributes, regexMatched := matchAccessControlRule(accessControl, "/admin/users", SourceFirewall, "main")
    if false == regexMatched {
        t.Fatalf("expected the regex rule to match")
    }
    if "^/admin" != regexRecord.Pattern() || accesscontrol.MatchingRegex != regexRecord.Matching() || "" != regexRecord.PathPrefix() {
        t.Fatalf("unexpected regex record: pattern %q, mode %s, prefix %q", regexRecord.Pattern(), regexRecord.Matching(), regexRecord.PathPrefix())
    }
    if 0 != regexRecord.RuleIndex() || SourceFirewall != regexRecord.Source() || "main" != regexRecord.Firewall() {
        t.Fatalf("unexpected regex record metadata: index %d, source %v, firewall %q", regexRecord.RuleIndex(), regexRecord.Source(), regexRecord.Firewall())
    }
    if 1 != len(regexAttributes) || "ROLE_ADMIN" != regexAttributes[0] {
        t.Fatalf("unexpected regex attributes: %v", regexAttributes)
    }

    fallbackRecord, _, fallbackMatched := matchAccessControlRule(accessControl, "/public", SourceGlobal, "")
    if false == fallbackMatched {
        t.Fatalf("expected the fallback rule to match")
    }
    if "" != fallbackRecord.Pattern() || accesscontrol.MatchingRawPrefix != fallbackRecord.Matching() || "" != fallbackRecord.PathPrefix() {
        t.Fatalf("unexpected fallback record: pattern %q, mode %s, prefix %q", fallbackRecord.Pattern(), fallbackRecord.Matching(), fallbackRecord.PathPrefix())
    }
    if 1 != fallbackRecord.RuleIndex() || SourceGlobal != fallbackRecord.Source() {
        t.Fatalf("unexpected fallback record metadata: index %d, source %v", fallbackRecord.RuleIndex(), fallbackRecord.Source())
    }
}

func TestMatchAccessControlRule_ExactRuleDoesNotMatchPrefixPaths(t *testing.T) {
    accessControl := NewAccessControl([]AccessControlRule{
        accesscontrol.NewExactRule("/admin", accesscontrol.RuleConfig{Attributes: []string{"ROLE_ADMIN"}}),
    })

    _, _, matchedPrefix := matchAccessControlRule(accessControl, "/admin-public", SourceFirewall, "main")
    if true == matchedPrefix {
        t.Fatalf("expected exact rule to not match /admin-public")
    }

    _, attributes, matchedExact := matchAccessControlRule(accessControl, "/admin", SourceFirewall, "main")
    if false == matchedExact || 1 != len(attributes) || "ROLE_ADMIN" != attributes[0] {
        t.Fatalf("expected exact /admin to match ROLE_ADMIN, got matched=%v attributes=%v", matchedExact, attributes)
    }
}

func TestMatchAccessControlRule_SegmentPrefixRespectsBoundary(t *testing.T) {
    accessControl := NewAccessControl([]AccessControlRule{
        accesscontrol.NewSegmentPrefixRule("/admin", accesscontrol.RuleConfig{Attributes: []string{"ROLE_ADMIN"}}),
    })

    _, _, matchedSibling := matchAccessControlRule(accessControl, "/administration", SourceFirewall, "main")
    if true == matchedSibling {
        t.Fatalf("expected segment-prefix /admin to not match /administration")
    }

    _, _, matchedChild := matchAccessControlRule(accessControl, "/admin/users", SourceFirewall, "main")
    if false == matchedChild {
        t.Fatalf("expected segment-prefix /admin to match /admin/users")
    }
}

func TestAccessControlListener_WhenNoSecurityContext_EmitsAuthorizationDeniedAndSets401(t *testing.T) {
    kernel := newTestKernel()
    runtimeInstance := newTestRuntime()

    registry := NewFirewallRegistry(
        NewCompiledConfiguration(nil, NewAccessControl([]AccessControlRule{accesscontrol.NewRawPrefixRule("/admin", accesscontrol.RuleConfig{Attributes: []string{"ROLE_ADMIN"}})})),
    )

    deniedCount := 0
    kernel.EventDispatcher().AddListener(
        securitycontract.EventSecurityAuthorizationDenied,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            deniedCount++
            return nil
        },
        0,
    )

    RegisterKernelAccessControlListener(kernel, registry)

    request := newSecurityTestRequest("GET", "/admin", nil, runtimeInstance)
    requestEvent := httpPkg.NewKernelRequestEvent(runtimeInstance, request)

    _, err := kernel.EventDispatcher().DispatchName(
        runtimeInstance,
        "kernel.request",
        requestEvent,
    )
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if 1 != deniedCount {
        t.Fatalf("expected one authorization denied event")
    }

    if nil == requestEvent.Response() {
        t.Fatalf("expected response to be set")
    }
}

/* the listener reads the spelling the router routes: "/public%2F" is the one-segment resource the protected catch-all serves, so the closed rule claims and refuses it */
func TestAccessControlListener_ReadsThePathTheRouterRoutes(t *testing.T) {
    for path, denied := range map[string]bool{
        "/public":         false,
        "/public/":        false,
        "/public%2F":      true,
        "/public%2Fx":     true,
        "/admin%2Fusers":  true,
        "/admin%2Fusers{": true,
        "/private":        true,
    } {
        kernel := newTestKernel()
        runtimeInstance := newTestRuntime()

        registry := NewFirewallRegistry(
            NewCompiledConfiguration(nil, NewAccessControl([]AccessControlRule{
                accesscontrol.NewExactRule("/public", accesscontrol.RuleConfig{Attributes: []string{securitycontract.AttributePublicAccess}}),
                accesscontrol.NewSegmentPrefixRule("/", accesscontrol.RuleConfig{Attributes: []string{"ROLE_ADMIN"}}),
            })),
        )

        deniedCount := 0
        kernel.EventDispatcher().AddListener(
            securitycontract.EventSecurityAuthorizationDenied,
            func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
                deniedCount++
                return nil
            },
            0,
        )

        RegisterKernelAccessControlListener(kernel, registry)

        request := newSecurityTestRequest("GET", path, nil, runtimeInstance)
        requestEvent := httpPkg.NewKernelRequestEvent(runtimeInstance, request)

        _, err := kernel.EventDispatcher().DispatchName(runtimeInstance, "kernel.request", requestEvent)
        if nil != err {
            t.Fatalf("unexpected error for %q: %v", path, err)
        }

        if denied != (1 == deniedCount) {
            t.Fatalf("expected %q to be denied: %v, got %d denials", path, denied, deniedCount)
        }

        if denied != (nil != requestEvent.Response()) {
            t.Fatalf("expected %q to carry a refusal response: %v", path, denied)
        }
    }
}

func TestAccessControlListener_ReadsAnEncodedSeparatorTheEscapedPathWouldRespell(t *testing.T) {
    for path, denied := range map[string]bool{
        "/admin/users":     true,
        "/admin%2Fusers":   false,
        "/admin%2Fusers{":  false,
        "/admin%2Fusers|x": false,
    } {
        kernel := newTestKernel()
        runtimeInstance := newTestRuntime()

        registry := NewFirewallRegistry(
            NewCompiledConfiguration(nil, NewAccessControl([]AccessControlRule{
                accesscontrol.NewSegmentPrefixRule("/admin", accesscontrol.RuleConfig{Attributes: []string{"ROLE_ADMIN"}}),
            })),
        )

        deniedCount := 0
        kernel.EventDispatcher().AddListener(
            securitycontract.EventSecurityAuthorizationDenied,
            func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
                deniedCount++
                return nil
            },
            0,
        )

        RegisterKernelAccessControlListener(kernel, registry)

        request := newSecurityTestRequest("GET", path, nil, runtimeInstance)
        requestEvent := httpPkg.NewKernelRequestEvent(runtimeInstance, request)

        _, err := kernel.EventDispatcher().DispatchName(runtimeInstance, "kernel.request", requestEvent)
        if nil != err {
            t.Fatalf("unexpected error for %q: %v", path, err)
        }

        if denied != (1 == deniedCount) {
            t.Fatalf("expected %q to be denied: %v, got %d denials", path, denied, deniedCount)
        }
    }
}

/* the security context is PUT on the runtime here, carrying a nil token. Without that the listener never reaches the token check at all — it answers from the missing-context branch above, which is a different refusal for a different reason, and the whole nil-token block could be deleted with this test still green. The reason is what tells the two apart, so it is what this asserts. */
func TestAccessControlListener_WhenSecurityContextHasNilToken_EmitsAuthorizationDeniedAndSets401(t *testing.T) {
    kernel := newTestKernel()
    runtimeInstance := newTestRuntime()

    firewall := NewCompiledFirewall(CompiledFirewallConfig{
        Name:                        "main",
        Matcher:                     NewPathPrefixMatcher("/"),
        MatcherDescription:          "m",
        AccessControl:               NewAccessControl([]AccessControlRule{accesscontrol.NewRawPrefixRule("/admin", accesscontrol.RuleConfig{Attributes: []string{"ROLE_ADMIN"}})}),
        AccessDecisionManager:       &accessControlListenerTestAccessDecisionManager{decideAllErr: nil},
        LoginPath:                   "/admin/login",
        LogoutPath:                  "/admin/logout",
        RoleHierarchySource:         SourceFirewall,
        AccessDecisionManagerSource: SourceFirewall,
        AccessControlSource:         SourceFirewall,
        EntryPointSource:            SourceFirewall,
    })

    registry := NewFirewallRegistry(
        NewCompiledConfiguration(nil, NewAccessControl([]AccessControlRule{accesscontrol.NewRawPrefixRule("/admin", accesscontrol.RuleConfig{Attributes: []string{"ROLE_ADMIN"}})})),
    )

    SecurityContextSetOnRuntime(runtimeInstance, NewSecurityContext(firewall, nil))

    deniedCount := 0
    deniedReason := ""
    kernel.EventDispatcher().AddListener(
        securitycontract.EventSecurityAuthorizationDenied,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            deniedCount++

            deniedEvent, isDeniedEvent := eventValue.Payload().(*AuthorizationDeniedEvent)
            if false == isDeniedEvent {
                t.Fatalf("expected an authorization denied event, got %T", eventValue.Payload())
            }

            melodyErr, isMelodyErr := deniedEvent.Err().(*exception.Error)
            if false == isMelodyErr {
                t.Fatalf("expected the refusal to carry its reason, got %T", deniedEvent.Err())
            }

            deniedReason, _ = melodyErr.Context()["reason"].(string)

            return nil
        },
        0,
    )

    RegisterKernelAccessControlListener(kernel, registry)

    request := newSecurityTestRequest("GET", "/admin", nil, runtimeInstance)
    requestEvent := httpPkg.NewKernelRequestEvent(runtimeInstance, request)

    _, err := kernel.EventDispatcher().DispatchName(
        runtimeInstance,
        "kernel.request",
        requestEvent,
    )
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if 1 != deniedCount {
        t.Fatalf("expected one authorization denied event")
    }

    if "missing_token" != deniedReason {
        t.Fatalf("expected the nil-token branch to answer, got reason %q", deniedReason)
    }

    if nil == requestEvent.Response() {
        t.Fatalf("expected response to be set")
    }
}

func TestAccessControlListener_WhenTokenNotAuthenticated_UsesEntryPointResponse(t *testing.T) {
    kernel := newTestKernel()
    runtimeInstance := newTestRuntime()

    entryPoint := &accessControlListenerTestEntryPoint{
        response: httpPkg.JsonErrorResponse(401, "unauthorized"),
        err:      nil,
    }

    firewall := NewCompiledFirewall(CompiledFirewallConfig{
        Name:                        "main",
        Matcher:                     NewPathPrefixMatcher("/"),
        MatcherDescription:          "m",
        AccessControl:               NewAccessControl([]AccessControlRule{accesscontrol.NewRawPrefixRule("/admin", accesscontrol.RuleConfig{Attributes: []string{"ROLE_ADMIN"}})}),
        AccessDecisionManager:       &accessControlListenerTestAccessDecisionManager{decideAllErr: nil},
        EntryPoint:                  entryPoint,
        LoginPath:                   "/admin/login",
        LogoutPath:                  "/admin/logout",
        RoleHierarchySource:         SourceFirewall,
        AccessDecisionManagerSource: SourceFirewall,
        AccessControlSource:         SourceFirewall,
        EntryPointSource:            SourceFirewall,
    })

    securityContext := NewSecurityContext(firewall, NewAnonymousToken())
    SecurityContextSetOnRuntime(runtimeInstance, securityContext)

    registry := NewFirewallRegistry(
        NewCompiledConfiguration([]*CompiledFirewall{firewall}, nil),
    )

    RegisterKernelAccessControlListener(kernel, registry)

    request := newSecurityTestRequest("GET", "/admin", nil, runtimeInstance)
    requestEvent := httpPkg.NewKernelRequestEvent(runtimeInstance, request)

    _, err := kernel.EventDispatcher().DispatchName(
        runtimeInstance,
        "kernel.request",
        requestEvent,
    )
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if 1 != entryPoint.calls {
        t.Fatalf("expected entry point to be called once")
    }
    if nil == requestEvent.Response() {
        t.Fatalf("expected response to be set")
    }
}

func TestAccessControlListener_WhenDecisionGranted_EmitsAuthorizationGranted(t *testing.T) {
    kernel := newTestKernel()
    runtimeInstance := newTestRuntime()

    firewall := NewCompiledFirewall(CompiledFirewallConfig{
        Name:                        "main",
        Matcher:                     NewPathPrefixMatcher("/"),
        MatcherDescription:          "m",
        AccessControl:               NewAccessControl([]AccessControlRule{accesscontrol.NewRawPrefixRule("/admin", accesscontrol.RuleConfig{Attributes: []string{"ROLE_ADMIN"}})}),
        AccessDecisionManager:       &accessControlListenerTestAccessDecisionManager{decideAllErr: nil},
        LoginPath:                   "/admin/login",
        LogoutPath:                  "/admin/logout",
        RoleHierarchySource:         SourceFirewall,
        AccessDecisionManagerSource: SourceFirewall,
        AccessControlSource:         SourceFirewall,
    })

    securityContext := NewSecurityContext(firewall, NewAuthenticatedToken("user", []string{"ROLE_ADMIN"}, nil))
    SecurityContextSetOnRuntime(runtimeInstance, securityContext)

    registry := NewFirewallRegistry(
        NewCompiledConfiguration([]*CompiledFirewall{firewall}, nil),
    )

    grantedCount := 0
    kernel.EventDispatcher().AddListener(
        securitycontract.EventSecurityAuthorizationGranted,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            grantedCount++
            return nil
        },
        0,
    )

    RegisterKernelAccessControlListener(kernel, registry)

    request := newSecurityTestRequest("GET", "/admin", nil, runtimeInstance)
    requestEvent := httpPkg.NewKernelRequestEvent(runtimeInstance, request)

    _, err := kernel.EventDispatcher().DispatchName(
        runtimeInstance,
        "kernel.request",
        requestEvent,
    )
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if 1 != grantedCount {
        t.Fatalf("expected one authorization granted event")
    }
}

func TestAccessControlListener_KeepsTheRegexRuleThatDecidedOnTheSecurityContext(t *testing.T) {
    kernel := newTestKernel()
    runtimeInstance := newTestRuntime()

    firewall := NewCompiledFirewall(CompiledFirewallConfig{
        Name:                        "main",
        Matcher:                     NewPathPrefixMatcher("/"),
        MatcherDescription:          "m",
        AccessControl:               NewAccessControl([]AccessControlRule{accesscontrol.NewRegexRule("^/admin", accesscontrol.RuleConfig{Attributes: []string{"ROLE_ADMIN"}})}),
        AccessDecisionManager:       &accessControlListenerTestAccessDecisionManager{decideAllErr: nil},
        LoginPath:                   "/admin/login",
        LogoutPath:                  "/admin/logout",
        RoleHierarchySource:         SourceFirewall,
        AccessDecisionManagerSource: SourceFirewall,
        AccessControlSource:         SourceFirewall,
    })

    securityContext := NewSecurityContext(firewall, NewAuthenticatedToken("user", []string{"ROLE_ADMIN"}, nil))
    SecurityContextSetOnRuntime(runtimeInstance, securityContext)

    registry := NewFirewallRegistry(
        NewCompiledConfiguration([]*CompiledFirewall{firewall}, nil),
    )

    RegisterKernelAccessControlListener(kernel, registry)

    request := newSecurityTestRequest("GET", "/admin", nil, runtimeInstance)
    requestEvent := httpPkg.NewKernelRequestEvent(runtimeInstance, request)

    _, err := kernel.EventDispatcher().DispatchName(
        runtimeInstance,
        "kernel.request",
        requestEvent,
    )
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    matchedRule := securityContext.MatchedRule()
    if nil == matchedRule {
        t.Fatalf("expected the rule that decided to be kept on the security context")
    }
    if "^/admin" != matchedRule.Pattern() || accesscontrol.MatchingRegex != matchedRule.Matching() {
        t.Fatalf("unexpected record: pattern %q, mode %s", matchedRule.Pattern(), matchedRule.Matching())
    }
    if SourceFirewall != matchedRule.Source() || "main" != matchedRule.Firewall() || 0 != matchedRule.RuleIndex() {
        t.Fatalf("unexpected record metadata: source %v, firewall %q, index %d", matchedRule.Source(), matchedRule.Firewall(), matchedRule.RuleIndex())
    }
}

func TestAccessControlListener_WhenDecisionDenied_EmitsAuthorizationDeniedAndSetsExceptionResponse(t *testing.T) {
    kernel := newTestKernel()
    runtimeInstance := newTestRuntime()

    firewall := NewCompiledFirewall(CompiledFirewallConfig{
        Name:                        "main",
        Matcher:                     NewPathPrefixMatcher("/"),
        MatcherDescription:          "m",
        AccessControl:               NewAccessControl([]AccessControlRule{accesscontrol.NewRawPrefixRule("/admin", accesscontrol.RuleConfig{Attributes: []string{"ROLE_ADMIN"}})}),
        AccessDecisionManager:       &accessControlListenerTestAccessDecisionManager{decideAllErr: errors.New("denied")},
        LoginPath:                   "/admin/login",
        LogoutPath:                  "/admin/logout",
        RoleHierarchySource:         SourceFirewall,
        AccessDecisionManagerSource: SourceFirewall,
        AccessControlSource:         SourceFirewall,
    })

    securityContext := NewSecurityContext(firewall, NewAuthenticatedToken("user", []string{"ROLE_USER"}, nil))
    SecurityContextSetOnRuntime(runtimeInstance, securityContext)

    registry := NewFirewallRegistry(
        NewCompiledConfiguration([]*CompiledFirewall{firewall}, nil),
    )

    deniedCount := 0
    kernel.EventDispatcher().AddListener(
        securitycontract.EventSecurityAuthorizationDenied,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            deniedCount++
            return nil
        },
        0,
    )

    registerTestKernelExceptionListener(kernel)
    RegisterKernelAccessControlListener(kernel, registry)

    request := newSecurityTestRequest("GET", "/admin", nil, runtimeInstance)
    requestEvent := httpPkg.NewKernelRequestEvent(runtimeInstance, request)

    _, err := kernel.EventDispatcher().DispatchName(
        runtimeInstance,
        "kernel.request",
        requestEvent,
    )
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if 1 != deniedCount {
        t.Fatalf("expected one authorization denied event")
    }
    if nil == requestEvent.Response() {
        t.Fatalf("expected response to be set")
    }
}

func TestAccessControlListener_WhenEntryPointReturnsNoResponse_FailsClosed(t *testing.T) {
    kernel := newTestKernel()
    runtimeInstance := newTestRuntime()

    /* an entry point that answers no response must not let the request through: the listener falls through to a fail-closed 401 rather than leaving the response nil, which the kernel reads as "no decision" and proceeds to the handler */
    entryPoint := &accessControlListenerTestEntryPoint{
        response: nil,
        err:      nil,
    }

    firewall := NewCompiledFirewall(CompiledFirewallConfig{
        Name:                        "main",
        Matcher:                     NewPathPrefixMatcher("/"),
        MatcherDescription:          "m",
        AccessControl:               NewAccessControl([]AccessControlRule{accesscontrol.NewRawPrefixRule("/admin", accesscontrol.RuleConfig{Attributes: []string{"ROLE_ADMIN"}})}),
        AccessDecisionManager:       &accessControlListenerTestAccessDecisionManager{decideAllErr: nil},
        EntryPoint:                  entryPoint,
        LoginPath:                   "/admin/login",
        LogoutPath:                  "/admin/logout",
        RoleHierarchySource:         SourceFirewall,
        AccessDecisionManagerSource: SourceFirewall,
        AccessControlSource:         SourceFirewall,
        EntryPointSource:            SourceFirewall,
    })

    securityContext := NewSecurityContext(firewall, NewAnonymousToken())
    SecurityContextSetOnRuntime(runtimeInstance, securityContext)

    registry := NewFirewallRegistry(NewCompiledConfiguration([]*CompiledFirewall{firewall}, nil))
    RegisterKernelAccessControlListener(kernel, registry)

    request := newSecurityTestRequest("GET", "/admin", nil, runtimeInstance)
    requestEvent := httpPkg.NewKernelRequestEvent(runtimeInstance, request)

    _, err := kernel.EventDispatcher().DispatchName(runtimeInstance, "kernel.request", requestEvent)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if nil == requestEvent.Response() {
        t.Fatalf("expected a fail-closed response when the entry point answered none, got nil (the request would reach its handler unauthorized)")
    }
}

/* the entry point is the application's, so a typed nil of a concrete response type is the shape a hand-written "no response" takes; carried through a bare nil check it is normalized back to nil by SetResponse and the unauthenticated request is served — the guard must read it through IsNilInterface and fall through to the fail-closed 401 */
func TestAccessControlListener_WhenTheEntryPointAnswersATypedNilResponse_FailsClosed(t *testing.T) {
    kernel := newTestKernel()
    runtimeInstance := newTestRuntime()

    entryPoint := &accessControlListenerTestEntryPoint{
        response: (*httpPkg.Response)(nil),
        err:      nil,
    }

    firewall := NewCompiledFirewall(CompiledFirewallConfig{
        Name:                        "main",
        Matcher:                     NewPathPrefixMatcher("/"),
        MatcherDescription:          "m",
        AccessControl:               NewAccessControl([]AccessControlRule{accesscontrol.NewRawPrefixRule("/admin", accesscontrol.RuleConfig{Attributes: []string{"ROLE_ADMIN"}})}),
        AccessDecisionManager:       &accessControlListenerTestAccessDecisionManager{decideAllErr: nil},
        EntryPoint:                  entryPoint,
        LoginPath:                   "/admin/login",
        LogoutPath:                  "/admin/logout",
        RoleHierarchySource:         SourceFirewall,
        AccessDecisionManagerSource: SourceFirewall,
        AccessControlSource:         SourceFirewall,
        EntryPointSource:            SourceFirewall,
    })

    securityContext := NewSecurityContext(firewall, NewAnonymousToken())
    SecurityContextSetOnRuntime(runtimeInstance, securityContext)

    registry := NewFirewallRegistry(NewCompiledConfiguration([]*CompiledFirewall{firewall}, nil))
    RegisterKernelAccessControlListener(kernel, registry)

    request := newSecurityTestRequest("GET", "/admin", nil, runtimeInstance)
    requestEvent := httpPkg.NewKernelRequestEvent(runtimeInstance, request)

    _, err := kernel.EventDispatcher().DispatchName(runtimeInstance, "kernel.request", requestEvent)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if nil == requestEvent.Response() || 401 != requestEvent.Response().StatusCode() {
        t.Fatalf("expected the typed-nil entry point response to fail closed with 401, got %#v", requestEvent.Response())
    }
}

/* the handler is the application's, so a typed nil of a concrete response type is the shape a hand-written "no response" takes; through a bare nil check it reads as a live response, SetResponse normalizes it to nil, and the DENIED request is served as granted — the guard must read it through IsNilInterface and answer through the fail-closed denial path instead */
func TestAccessControlListener_ADeniedHandlerAnsweringATypedNilIsRefusedNotServed(t *testing.T) {
    kernel := newTestKernel()
    runtimeInstance := newTestRuntime()

    firewall := NewCompiledFirewall(CompiledFirewallConfig{
        Name:                        "main",
        Matcher:                     NewPathPrefixMatcher("/"),
        MatcherDescription:          "m",
        AccessControl:               NewAccessControl([]AccessControlRule{accesscontrol.NewRawPrefixRule("/admin", accesscontrol.RuleConfig{Attributes: []string{"ROLE_ADMIN"}})}),
        AccessDecisionManager:       &accessControlListenerTestAccessDecisionManager{decideAllErr: errors.New("denied")},
        AccessDeniedHandler:         &accessControlListenerTestAccessDeniedHandler{response: (*httpPkg.Response)(nil), err: nil},
        LoginPath:                   "/admin/login",
        LogoutPath:                  "/admin/logout",
        RoleHierarchySource:         SourceFirewall,
        AccessDecisionManagerSource: SourceFirewall,
        AccessControlSource:         SourceFirewall,
        AccessDeniedHandlerSource:   SourceFirewall,
    })

    securityContext := NewSecurityContext(firewall, NewAuthenticatedToken("user", []string{"ROLE_USER"}, nil))
    SecurityContextSetOnRuntime(runtimeInstance, securityContext)

    registry := NewFirewallRegistry(NewCompiledConfiguration([]*CompiledFirewall{firewall}, nil))
    RegisterKernelAccessControlListener(kernel, registry)

    request := newSecurityTestRequest("GET", "/admin", nil, runtimeInstance)
    requestEvent := httpPkg.NewKernelRequestEvent(runtimeInstance, request)

    _, err := kernel.EventDispatcher().DispatchName(runtimeInstance, "kernel.request", requestEvent)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if nil == requestEvent.Response() {
        t.Fatalf("expected the typed-nil handler answer to be refused with a fail-closed response, got nil (the denied request would be served)")
    }
}

func TestAccessControlListener_WhenExceptionProducesNoResponse_FailsClosed(t *testing.T) {
    kernel := newTestKernel()
    runtimeInstance := newTestRuntime()

    firewall := NewCompiledFirewall(CompiledFirewallConfig{
        Name:                        "main",
        Matcher:                     NewPathPrefixMatcher("/"),
        MatcherDescription:          "m",
        AccessControl:               NewAccessControl([]AccessControlRule{accesscontrol.NewRawPrefixRule("/admin", accesscontrol.RuleConfig{Attributes: []string{"ROLE_ADMIN"}})}),
        AccessDecisionManager:       &accessControlListenerTestAccessDecisionManager{decideAllErr: errors.New("denied")},
        LoginPath:                   "/admin/login",
        LogoutPath:                  "/admin/logout",
        RoleHierarchySource:         SourceFirewall,
        AccessDecisionManagerSource: SourceFirewall,
        AccessControlSource:         SourceFirewall,
    })

    securityContext := NewSecurityContext(firewall, NewAuthenticatedToken("user", []string{"ROLE_USER"}, nil))
    SecurityContextSetOnRuntime(runtimeInstance, securityContext)

    registry := NewFirewallRegistry(NewCompiledConfiguration([]*CompiledFirewall{firewall}, nil))
    RegisterKernelAccessControlListener(kernel, registry)

    /* no kernel.exception listener produces a response, so exceptionEvent.Response() is nil: the listener must write a fail-closed response rather than the nil the kernel reads as "no decision" */
    request := newSecurityTestRequest("GET", "/admin", nil, runtimeInstance)
    requestEvent := httpPkg.NewKernelRequestEvent(runtimeInstance, request)

    _, err := kernel.EventDispatcher().DispatchName(runtimeInstance, "kernel.request", requestEvent)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if nil == requestEvent.Response() {
        t.Fatalf("expected a fail-closed response when the exception dispatch produced none, got nil")
    }
}

/* A nil pointer of a request type is a non-nil interface, so a bare comparison would carry it past
the gate and into the path read below, which dereferences it — inside a kernel listener, where no
recover covers it. The listener must leave such an event alone, not crash the request. */
func TestAccessControlListener_ATypedNilRequestIsLeftAlone(t *testing.T) {
    kernel := newTestKernel()
    runtimeInstance := newTestRuntime()

    registry := NewFirewallRegistry(
        NewCompiledConfiguration(nil, NewAccessControl([]AccessControlRule{accesscontrol.NewRawPrefixRule("/admin", accesscontrol.RuleConfig{Attributes: []string{"ROLE_ADMIN"}})})),
    )

    RegisterKernelAccessControlListener(kernel, registry)

    var unassignedRequest *httpPkg.Request

    requestEvent := httpPkg.NewKernelRequestEvent(runtimeInstance, unassignedRequest)

    _, err := kernel.EventDispatcher().DispatchName(
        runtimeInstance,
        "kernel.request",
        requestEvent,
    )
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if nil != requestEvent.Response() {
        t.Fatalf("expected no response for a request the listener cannot read")
    }
}

/* the listener asks for ALL of a rule's attributes, and a stub answering one field for both decisions, over rules carrying a single attribute, cannot tell that apart from ANY. Here the two decisions disagree — all refuses, any accepts — and the rule carries two attributes of which the token holds one, which is exactly the input on which the semantics differ. */
func TestAccessControlListener_TheDecisionIsDecideAllOverTheWholeAttributeSet(t *testing.T) {
    kernel := newTestKernel()
    runtimeInstance := newTestRuntime()

    decisionManager := &accessControlListenerTestAccessDecisionManager{
        decideAllErr: errors.New("access denied"),
        decideAnyErr: nil,
    }

    firewall := NewCompiledFirewall(CompiledFirewallConfig{
        Name:                        "main",
        Matcher:                     NewPathPrefixMatcher("/"),
        MatcherDescription:          "m",
        AccessControl:               NewAccessControl([]AccessControlRule{accesscontrol.NewRawPrefixRule("/admin", accesscontrol.RuleConfig{Attributes: []string{"ROLE_ADMIN", "ROLE_AUDITOR"}})}),
        AccessDecisionManager:       decisionManager,
        LoginPath:                   "/admin/login",
        LogoutPath:                  "/admin/logout",
        RoleHierarchySource:         SourceFirewall,
        AccessDecisionManagerSource: SourceFirewall,
        AccessControlSource:         SourceFirewall,
    })

    securityContext := NewSecurityContext(firewall, NewAuthenticatedToken("user", []string{"ROLE_ADMIN"}, nil))
    SecurityContextSetOnRuntime(runtimeInstance, securityContext)

    registry := NewFirewallRegistry(
        NewCompiledConfiguration([]*CompiledFirewall{firewall}, nil),
    )

    grantedCount := 0
    kernel.EventDispatcher().AddListener(
        securitycontract.EventSecurityAuthorizationGranted,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            grantedCount++
            return nil
        },
        0,
    )

    RegisterKernelAccessControlListener(kernel, registry)

    request := newSecurityTestRequest("GET", "/admin", nil, runtimeInstance)
    requestEvent := httpPkg.NewKernelRequestEvent(runtimeInstance, request)

    if _, err := kernel.EventDispatcher().DispatchName(runtimeInstance, "kernel.request", requestEvent); nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if "DecideAll" != decisionManager.calledMethod {
        t.Fatalf("expected the listener to ask for every attribute, got %q", decisionManager.calledMethod)
    }

    if 2 != len(decisionManager.calledAttributes) {
        t.Fatalf("expected the rule's whole attribute set to reach the decision, got %v", decisionManager.calledAttributes)
    }

    if 0 != grantedCount {
        t.Fatalf("expected the refusal of the all-decision to stand, not the acceptance of the any-decision")
    }
}

/* a dispatcher that cannot take the required-listener mark disarms the fail-closed guarantee for the whole process, so it is named on the emergency channel */
func TestRegisterKernelAccessControlListener_ADispatcherWithoutTheCapabilityIsNamed(t *testing.T) {
    readEnd, writeEnd, pipeErr := os.Pipe()
    if nil != pipeErr {
        t.Fatalf("unexpected pipe error: %v", pipeErr)
    }

    logging.CloseEmergencyLogger()

    originalStderr := os.Stderr
    os.Stderr = writeEnd
    defer func() {
        os.Stderr = originalStderr
        logging.CloseEmergencyLogger()
    }()

    kernel := newTestKernel()
    kernel.eventDispatcher = &capabilitylessDispatcher{EventDispatcher: kernel.eventDispatcher}

    RegisterKernelAccessControlListener(
        kernel,
        NewFirewallRegistry(NewCompiledConfiguration(nil, NewAccessControl([]AccessControlRule{accesscontrol.NewRawPrefixRule("/admin", accesscontrol.RuleConfig{Attributes: []string{"ROLE_ADMIN"}})}))),
    )

    _ = writeEnd.Close()
    os.Stderr = originalStderr

    output, readErr := io.ReadAll(readEnd)
    if nil != readErr {
        t.Fatalf("unexpected read error: %v", readErr)
    }

    if false == strings.Contains(string(output), "cannot mark the access control listener required") {
        t.Fatalf("expected the degradation to be reported, got %q", string(output))
    }

    if false == strings.Contains(string(output), "capabilitylessDispatcher") {
        t.Fatalf("expected the dispatcher to be named, got %q", string(output))
    }
}

/* capabilitylessDispatcher is a dispatcher of the application's own: it forwards every dispatch through the published contract and carries no MarkListenerRequired, which is exactly what an implementation written against the contract looks like */
type capabilitylessDispatcher struct {
    eventcontract.EventDispatcher
}

func TestAccessControlListener_AFailedDeniedHandlerKeepsItsErrorBesideTheDecision(t *testing.T) {
    runtimeInstance := newTestRuntime()

    handlerErr := errors.New("denied page renderer down")

    firewall := NewCompiledFirewall(CompiledFirewallConfig{
        Name:                        "main",
        Matcher:                     NewPathPrefixMatcher("/"),
        MatcherDescription:          "m",
        AccessControl:               NewAccessControl([]AccessControlRule{accesscontrol.NewRawPrefixRule("/admin", accesscontrol.RuleConfig{Attributes: []string{"ROLE_ADMIN"}})}),
        AccessDecisionManager:       NewAccessDecisionManager(securitycontract.DecisionStrategyAffirmative, []securitycontract.Voter{NewRoleVoter()}),
        AccessDeniedHandler:         &accessControlListenerTestAccessDeniedHandler{response: nil, err: handlerErr},
        LoginPath:                   "/admin/login",
        LogoutPath:                  "/admin/logout",
        RoleHierarchySource:         SourceFirewall,
        AccessDecisionManagerSource: SourceFirewall,
        AccessControlSource:         SourceFirewall,
        AccessDeniedHandlerSource:   SourceFirewall,
    })

    kernel := newTestKernel()

    var exceptionErr error
    kernel.EventDispatcher().AddListener(
        "kernel.exception",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            if exceptionEvent, ok := eventValue.Payload().(*httpPkg.KernelExceptionEvent); true == ok {
                exceptionErr = exceptionEvent.Err()
            }

            return nil
        },
        100,
    )
    httpPkg.RegisterKernelExceptionListener(kernel.EventDispatcher(), false)

    SecurityContextSetOnRuntime(runtimeInstance, NewSecurityContext(firewall, NewAuthenticatedToken("user", []string{"ROLE_USER"}, nil)))
    RegisterKernelAccessControlListener(kernel, NewFirewallRegistry(NewCompiledConfiguration([]*CompiledFirewall{firewall}, nil)))

    requestEvent := httpPkg.NewKernelRequestEvent(runtimeInstance, newSecurityTestRequest("GET", "/admin", nil, runtimeInstance))
    if _, dispatchErr := kernel.EventDispatcher().DispatchName(runtimeInstance, "kernel.request", requestEvent); nil != dispatchErr {
        t.Fatalf("unexpected error: %v", dispatchErr)
    }

    if false == errors.Is(exceptionErr, handlerErr) {
        t.Fatalf("expected the handler's own error kept beside the decision, got %v", exceptionErr)
    }

    if nil == requestEvent.Response() || 403 != requestEvent.Response().StatusCode() {
        t.Fatalf("expected the denial still answered 403")
    }
}
