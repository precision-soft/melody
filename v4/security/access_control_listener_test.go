package security

import (
    "errors"
    "io"
    nethttp "net/http"
    "net/http/httptest"
    "os"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    eventcontract "github.com/precision-soft/melody/v4/event/contract"
    "github.com/precision-soft/melody/v4/exception"
    httpPkg "github.com/precision-soft/melody/v4/http"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
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

    if httpException := exception.AsHttpException(requestEvent.Refusal()); nil == httpException || 401 != httpException.StatusCode() {
        t.Fatalf("expected a 401 refusal, got %v", requestEvent.Refusal())
    }

    if nil != requestEvent.Response() {
        t.Fatalf("expected the refusal to leave the rendering to the kernel, got a response")
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

        if denied != (nil != requestEvent.Refusal()) {
            t.Fatalf("expected %q to carry a refusal: %v", path, denied)
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

    if httpException := exception.AsHttpException(requestEvent.Refusal()); nil == httpException || 401 != httpException.StatusCode() {
        t.Fatalf("expected a 401 refusal, got %v", requestEvent.Refusal())
    }

    if nil != requestEvent.Response() {
        t.Fatalf("expected the refusal to leave the rendering to the kernel, got a response")
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

func TestAccessControlListener_WhenDecisionDenied_EmitsAuthorizationDeniedAndRefuses(t *testing.T) {
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
    if nil == requestEvent.Refusal() {
        t.Fatalf("expected the denial raised as a refusal")
    }
}

func TestAccessControlListener_WhenEntryPointReturnsNoResponse_FailsClosed(t *testing.T) {
    kernel := newTestKernel()
    runtimeInstance := newTestRuntime()

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

    if httpException := exception.AsHttpException(requestEvent.Refusal()); nil == httpException || 401 != httpException.StatusCode() || "entry_point_answered_nothing" != httpException.Context()["reason"] {
        t.Fatalf("expected a 401 refusal when the entry point answered none, got %v (the request would reach its handler unauthorized)", requestEvent.Refusal())
    }
}

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

    if httpException := exception.AsHttpException(requestEvent.Refusal()); nil == httpException || 401 != httpException.StatusCode() {
        t.Fatalf("expected the typed-nil entry point response to fail closed with a 401 refusal, got %v", requestEvent.Refusal())
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

    if nil == requestEvent.Refusal() {
        t.Fatalf("expected the typed-nil handler answer to be refused, got no decision (the denied request would be served)")
    }
}

func TestAccessControlListener_AMissingDecisionManagerIsRefused(t *testing.T) {
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

    request := newSecurityTestRequest("GET", "/admin", nil, runtimeInstance)
    requestEvent := httpPkg.NewKernelRequestEvent(runtimeInstance, request)

    _, err := kernel.EventDispatcher().DispatchName(runtimeInstance, "kernel.request", requestEvent)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if nil == requestEvent.Refusal() {
        t.Fatalf("expected a refusal for the missing decision manager, got no decision")
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

    SecurityContextSetOnRuntime(runtimeInstance, NewSecurityContext(firewall, NewAuthenticatedToken("user", []string{"ROLE_USER"}, nil)))
    RegisterKernelAccessControlListener(kernel, NewFirewallRegistry(NewCompiledConfiguration([]*CompiledFirewall{firewall}, nil)))

    requestEvent := httpPkg.NewKernelRequestEvent(runtimeInstance, newSecurityTestRequest("GET", "/admin", nil, runtimeInstance))
    if _, dispatchErr := kernel.EventDispatcher().DispatchName(runtimeInstance, "kernel.request", requestEvent); nil != dispatchErr {
        t.Fatalf("unexpected error: %v", dispatchErr)
    }

    if false == errors.Is(requestEvent.Refusal(), handlerErr) {
        t.Fatalf("expected the handler's own error kept beside the decision, got %v", requestEvent.Refusal())
    }

    if httpException := exception.AsHttpException(requestEvent.Refusal()); nil == httpException || 403 != httpException.StatusCode() {
        t.Fatalf("expected the denial still a 403, got %v", requestEvent.Refusal())
    }
}

func newRefusalSiteFirewall(config CompiledFirewallConfig) *CompiledFirewall {
    config.Name = "main"
    config.Matcher = NewPathPrefixMatcher("/")
    config.MatcherDescription = "m"
    config.RoleHierarchySource = SourceFirewall
    config.AccessDecisionManagerSource = SourceFirewall
    config.AccessControlSource = SourceFirewall

    return NewCompiledFirewall(config)
}

func newRefusalSiteAccessControl() *AccessControl {
    return NewAccessControl([]AccessControlRule{accesscontrol.NewRawPrefixRule("/admin", accesscontrol.RuleConfig{Attributes: []string{"ROLE_ADMIN"}})})
}

func TestSecurityRefusals_ReachAnApplicationListenerAndKeepTheirStatus(t *testing.T) {
    failure := exception.Conflict("the site's own failure")
    authenticated := &resolutionListenerTestTokenSource{resolveToken: NewAuthenticatedToken("user", []string{"ROLE_USER"}, nil)}
    anonymous := &resolutionListenerTestTokenSource{resolveToken: NewAnonymousToken()}

    for name, testCase := range map[string]struct {
        firewall       *CompiledFirewall
        expectedErr    error
        expectedStatus int
    }{
        "firewall check": {
            firewall:       newRefusalSiteFirewall(CompiledFirewallConfig{Rules: []securitycontract.Rule{&resolutionListenerTestRule{err: failure}}, TokenSource: anonymous}),
            expectedErr:    failure,
            expectedStatus: nethttp.StatusConflict,
        },
        "token source": {
            firewall:       newRefusalSiteFirewall(CompiledFirewallConfig{TokenSource: &resolutionListenerTestTokenSource{resolveErr: failure}}),
            expectedErr:    failure,
            expectedStatus: nethttp.StatusConflict,
        },
        "entry point start": {
            firewall:       newRefusalSiteFirewall(CompiledFirewallConfig{TokenSource: anonymous, AccessControl: newRefusalSiteAccessControl(), EntryPoint: &accessControlListenerTestEntryPoint{err: failure}}),
            expectedErr:    failure,
            expectedStatus: nethttp.StatusConflict,
        },
        "entry point silent": {
            firewall:       newRefusalSiteFirewall(CompiledFirewallConfig{TokenSource: anonymous, AccessControl: newRefusalSiteAccessControl()}),
            expectedStatus: nethttp.StatusUnauthorized,
        },
        "missing decision manager": {
            firewall:       newRefusalSiteFirewall(CompiledFirewallConfig{TokenSource: authenticated, AccessControl: newRefusalSiteAccessControl()}),
            expectedStatus: nethttp.StatusInternalServerError,
        },
        "decision": {
            firewall:       newRefusalSiteFirewall(CompiledFirewallConfig{TokenSource: authenticated, AccessControl: newRefusalSiteAccessControl(), AccessDecisionManager: &accessControlListenerTestAccessDecisionManager{decideAllErr: failure}}),
            expectedErr:    failure,
            expectedStatus: nethttp.StatusConflict,
        },
    } {
        t.Run(name, func(t *testing.T) {
            newRegisteredKernel := func() *testKernel {
                kernel := newTestKernel()
                registry := NewFirewallRegistry(NewCompiledConfiguration([]*CompiledFirewall{testCase.firewall}, nil))
                RegisterKernelSecurityResolutionListener(kernel, registry)
                RegisterKernelAccessControlListener(kernel, registry)

                return kernel
            }

            unanswered := serveSecurityRequest(t, newRegisteredKernel(), "/admin")
            if testCase.expectedStatus != unanswered.Code {
                t.Fatalf("expected %d with no application listener, got %d: %s", testCase.expectedStatus, unanswered.Code, unanswered.Body.String())
            }

            kernel := newRegisteredKernel()

            var observedErr error
            kernel.EventDispatcher().AddListener(
                kernelcontract.EventKernelException,
                func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
                    exceptionEvent := eventValue.Payload().(*httpPkg.KernelExceptionEvent)
                    observedErr = exceptionEvent.Err()
                    exceptionEvent.SetResponse(httpPkg.TextResponse(nethttp.StatusTeapot, "application body"))

                    return nil
                },
                0,
            )

            answered := serveSecurityRequest(t, kernel, "/admin")
            if nethttp.StatusTeapot != answered.Code || "application body" != answered.Body.String() {
                t.Fatalf("expected the application's answer, got %d: %s", answered.Code, answered.Body.String())
            }

            if nil == observedErr {
                t.Fatalf("expected the listener to see the refusal")
            }

            if nil != testCase.expectedErr && false == errors.Is(observedErr, testCase.expectedErr) {
                t.Fatalf("expected the site's own failure, got %v", observedErr)
            }
        })
    }
}

func TestAccessControlListener_AnAnonymousRefusalReachesAnApplicationListener(t *testing.T) {
    kernel := newTestKernel()
    RegisterKernelAccessControlListener(kernel, NewFirewallRegistry(NewCompiledConfiguration(nil, newRefusalSiteAccessControl())))

    var observedErr error
    kernel.EventDispatcher().AddListener(
        kernelcontract.EventKernelException,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            observedErr = eventValue.Payload().(*httpPkg.KernelExceptionEvent).Err()

            return nil
        },
        0,
    )

    recorder := serveSecurityRequest(t, kernel, "/admin")

    if nethttp.StatusUnauthorized != recorder.Code {
        t.Fatalf("expected the 401, got %d", recorder.Code)
    }

    if httpException := exception.AsHttpException(observedErr); nil == httpException || "missing_security_context" != httpException.Context()["reason"] {
        t.Fatalf("expected the listener to see the refusal with its reason, got %v", observedErr)
    }
}

func TestAccessControlListener_StandsAsideAfterAResolutionRefusal(t *testing.T) {
    kernel := newTestKernel()
    runtimeInstance := newTestRuntime()

    decisionManager := &accessControlListenerTestAccessDecisionManager{}

    registry := NewFirewallRegistry(NewCompiledConfiguration([]*CompiledFirewall{
        newRefusalSiteFirewall(CompiledFirewallConfig{TokenSource: &resolutionListenerTestTokenSource{resolveErr: errors.New("token backend down")}, AccessControl: newRefusalSiteAccessControl(), AccessDecisionManager: decisionManager}),
    }, nil))
    RegisterKernelSecurityResolutionListener(kernel, registry)
    RegisterKernelAccessControlListener(kernel, registry)

    requestEvent := httpPkg.NewKernelRequestEvent(runtimeInstance, newSecurityTestRequest("GET", "/admin", nil, runtimeInstance))
    if _, dispatchErr := kernel.EventDispatcher().DispatchName(runtimeInstance, kernelcontract.EventKernelRequest, requestEvent); nil != dispatchErr {
        t.Fatalf("expected no required-listener refusal, got %v", dispatchErr)
    }

    if "" != decisionManager.calledMethod {
        t.Fatalf("expected the decision manager not consulted on a refused request, got %q", decisionManager.calledMethod)
    }

    if nil == requestEvent.Refusal() {
        t.Fatalf("expected the resolution refusal to stand")
    }
}

func TestSecurityListeners_StandAsideOnAnEarlierRefusal(t *testing.T) {
    kernel := newTestKernel()
    runtimeInstance := newTestRuntime()

    registry := NewFirewallRegistry(NewCompiledConfiguration([]*CompiledFirewall{
        newRefusalSiteFirewall(CompiledFirewallConfig{TokenSource: &resolutionListenerTestTokenSource{resolveErr: errors.New("token backend down")}}),
    }, newRefusalSiteAccessControl()))
    RegisterKernelSecurityResolutionListener(kernel, registry)
    RegisterKernelAccessControlListener(kernel, registry)

    deniedCount := 0
    kernel.EventDispatcher().AddListener(
        securitycontract.EventSecurityAuthorizationDenied,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            deniedCount++

            return nil
        },
        0,
    )

    earlier := exception.Forbidden("refused earlier")

    requestEvent := httpPkg.NewKernelRequestEvent(runtimeInstance, newSecurityTestRequest("GET", "/admin", nil, runtimeInstance))
    requestEvent.Refuse(earlier)

    if _, dispatchErr := kernel.EventDispatcher().DispatchName(runtimeInstance, kernelcontract.EventKernelRequest, requestEvent); nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    if _, exists := SecurityContextFromRuntime(runtimeInstance); true == exists {
        t.Fatalf("expected the resolution listener to stand aside, it published a security context")
    }

    if 0 != deniedCount {
        t.Fatalf("expected access control to stand aside, it denied %d times", deniedCount)
    }

    if earlier != requestEvent.Refusal() {
        t.Fatalf("expected the earlier refusal to stand, got %v", requestEvent.Refusal())
    }
}

type closeCountingBody struct {
    closed int
}

func (instance *closeCountingBody) Read(buffer []byte) (int, error) { return 0, io.EOF }

func (instance *closeCountingBody) Close() error {
    instance.closed++

    return nil
}

func TestAccessControlListener_AResponseBesideAFirewallAnswerersErrorIsClosed(t *testing.T) {
    newResponse := func(body *closeCountingBody) httpcontract.Response {
        response := httpPkg.EmptyResponse(nethttp.StatusTeapot)
        response.SetBodyReader(body)

        return response
    }

    entryPointBody := &closeCountingBody{}
    deniedHandlerBody := &closeCountingBody{}

    for name, testCase := range map[string]struct {
        firewall *CompiledFirewall
        token    securitycontract.Token
        body     *closeCountingBody
    }{
        "entry point": {
            firewall: newRefusalSiteFirewall(CompiledFirewallConfig{AccessControl: newRefusalSiteAccessControl(), EntryPoint: &accessControlListenerTestEntryPoint{response: newResponse(entryPointBody), err: errors.New("start failed")}}),
            token:    NewAnonymousToken(),
            body:     entryPointBody,
        },
        "denied handler": {
            firewall: newRefusalSiteFirewall(CompiledFirewallConfig{AccessControl: newRefusalSiteAccessControl(), AccessDecisionManager: &accessControlListenerTestAccessDecisionManager{decideAllErr: errors.New("denied")}, AccessDeniedHandler: &accessControlListenerTestAccessDeniedHandler{response: newResponse(deniedHandlerBody), err: errors.New("handler failed")}, AccessDeniedHandlerSource: SourceFirewall}),
            token:    NewAuthenticatedToken("user", []string{"ROLE_USER"}, nil),
            body:     deniedHandlerBody,
        },
    } {
        kernel := newTestKernel()
        runtimeInstance := newTestRuntime()

        SecurityContextSetOnRuntime(runtimeInstance, NewSecurityContext(testCase.firewall, testCase.token))
        RegisterKernelAccessControlListener(kernel, NewFirewallRegistry(NewCompiledConfiguration([]*CompiledFirewall{testCase.firewall}, nil)))

        requestEvent := httpPkg.NewKernelRequestEvent(runtimeInstance, newSecurityTestRequest("GET", "/admin", nil, runtimeInstance))
        if _, dispatchErr := kernel.EventDispatcher().DispatchName(runtimeInstance, kernelcontract.EventKernelRequest, requestEvent); nil != dispatchErr {
            t.Fatalf("%s: unexpected dispatch error: %v", name, dispatchErr)
        }

        if 1 != testCase.body.closed {
            t.Fatalf("%s: expected the discarded body closed once, got %d", name, testCase.body.closed)
        }

        if nil == requestEvent.Refusal() {
            t.Fatalf("%s: expected the refusal to stand", name)
        }
    }
}

type unavailableRouteFixture struct {
    kernel          *testKernel
    entryPoint      *accessControlListenerTestEntryPoint
    decisionManager *accessControlListenerTestAccessDecisionManager
    logger          *recordingLogger
    deniedReasons   []string
    deniedCauses    []error
    grantedCount    int
    observedErr     error
}

/* newUnavailableRouteFixture registers both security listeners over one firewall whose source answers token; /admin is protected, /public is PUBLIC_ACCESS and nothing else matches */
func newUnavailableRouteFixture(token securitycontract.Token, retryAfter bound.Duration) *unavailableRouteFixture {
    return newUnavailableRouteFixtureOver(&resolutionListenerTestTokenSource{resolveToken: token}, retryAfter)
}

func newUnavailableRouteFixtureOver(tokenSource securitycontract.TokenSource, retryAfter bound.Duration) *unavailableRouteFixture {
    fixture := &unavailableRouteFixture{
        kernel:          newTestKernel(),
        entryPoint:      &accessControlListenerTestEntryPoint{response: httpPkg.TextResponse(nethttp.StatusUnauthorized, "sign in")},
        decisionManager: &accessControlListenerTestAccessDecisionManager{},
        logger:          &recordingLogger{},
    }

    firewall := newRefusalSiteFirewall(CompiledFirewallConfig{
        TokenSource: tokenSource,
        AccessControl: NewAccessControl([]AccessControlRule{
            accesscontrol.NewRawPrefixRule("/admin", accesscontrol.RuleConfig{Attributes: []string{"ROLE_ADMIN"}}),
            accesscontrol.NewExactRule("/public", accesscontrol.RuleConfig{Attributes: []string{securitycontract.AttributePublicAccess}}),
        }),
        AccessDecisionManager: fixture.decisionManager,
        EntryPoint:            fixture.entryPoint,
        EntryPointSource:      SourceFirewall,
        UnavailableRetryAfter: retryAfter,
    })

    registry := NewFirewallRegistry(NewCompiledConfiguration([]*CompiledFirewall{firewall}, nil))
    RegisterKernelSecurityResolutionListener(fixture.kernel, registry)
    RegisterKernelAccessControlListener(fixture.kernel, registry)

    dispatcher := fixture.kernel.EventDispatcher()
    dispatcher.AddListener(
        securitycontract.EventSecurityAuthorizationDenied,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            reason := ""
            if deniedErr := eventValue.Payload().(*AuthorizationDeniedEvent).Err(); nil != deniedErr {
                reason, _ = exception.LogContext(deniedErr, exception.LogContextConfig{})["reason"].(string)
            }
            fixture.deniedReasons = append(fixture.deniedReasons, reason)
            fixture.deniedCauses = append(fixture.deniedCauses, eventValue.Payload().(*AuthorizationDeniedEvent).Err())

            return nil
        },
        0,
    )
    dispatcher.AddListener(
        securitycontract.EventSecurityAuthorizationGranted,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            fixture.grantedCount++

            return nil
        },
        0,
    )
    dispatcher.AddListener(
        kernelcontract.EventKernelException,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            fixture.observedErr = eventValue.Payload().(*httpPkg.KernelExceptionEvent).Err()

            return nil
        },
        10,
    )

    return fixture
}

func (instance *unavailableRouteFixture) serve(t *testing.T, path string) (*httptest.ResponseRecorder, bool, bool) {
    t.Helper()

    handled := false
    handlerSawUnavailable := false
    recorder := serveSecurityRequestThrough(
        t,
        instance.kernel,
        path,
        instance.logger,
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            handled = true
            if securityContext, exists := SecurityContextFromRuntime(runtimeInstance); true == exists {
                _, handlerSawUnavailable = AuthenticationUnavailableCause(securityContext.Token())
            }

            return httpPkg.TextResponse(nethttp.StatusOK, "served"), nil
        },
    )

    return recorder, handled, handlerSawUnavailable
}

var unavailableRouteStoreErr = errors.New("redis is down")

func newMarkedUnavailableToken() *UnavailableToken {
    return NewUnavailableToken(exception.MarkInfrastructureFailure(unavailableRouteStoreErr))
}

func TestAccessControlListener_TheFirewallRefusalKeepsTheFirewallDelayOverTheKernels(t *testing.T) {
    for name, testCase := range map[string]struct {
        retryAfter bound.Duration
        expected   string
    }{
        "the firewall's default": {retryAfter: bound.Duration{}, expected: "5"},
        "the firewall's own":     {retryAfter: bound.Of(9 * time.Second), expected: "9"},
    } {
        t.Run(name, func(t *testing.T) {
            fixture := newUnavailableRouteFixture(newMarkedUnavailableToken(), testCase.retryAfter)
            fixture.kernel.httpKernel.SetUnavailableRetryAfter(bound.Of(13 * time.Second))

            recorder, _, _ := fixture.serve(t, "/admin")

            if nethttp.StatusServiceUnavailable != recorder.Code || testCase.expected != recorder.Header().Get("Retry-After") {
                t.Fatalf("expected 503 with the firewall's Retry-After %s, not the kernel's 13, got %d %q", testCase.expected, recorder.Code, recorder.Header().Get("Retry-After"))
            }
        })
    }
}

func TestAccessControlListener_AnUnavailableTokenOnAProtectedRouteIsRefusedWith503AndRetryAfter(t *testing.T) {
    for name, testCase := range map[string]struct {
        retryAfter bound.Duration
        expected   string
    }{
        "the default":  {retryAfter: bound.Duration{}, expected: "5"},
        "the firewall": {retryAfter: bound.Of(9 * time.Second), expected: "9"},
    } {
        t.Run(name, func(t *testing.T) {
            fixture := newUnavailableRouteFixture(newMarkedUnavailableToken(), testCase.retryAfter)

            recorder, handled, _ := fixture.serve(t, "/admin")

            if nethttp.StatusServiceUnavailable != recorder.Code || testCase.expected != recorder.Header().Get("Retry-After") {
                t.Fatalf("expected 503 with Retry-After %s, got %d %q: %s", testCase.expected, recorder.Code, recorder.Header().Get("Retry-After"), recorder.Body.String())
            }

            if true == handled || 0 != fixture.entryPoint.calls || "" != fixture.decisionManager.calledMethod {
                t.Fatalf("expected neither the handler, the entry point nor the decision manager, got handled=%v entry=%d decision=%q", handled, fixture.entryPoint.calls, fixture.decisionManager.calledMethod)
            }

            if 1 != len(fixture.deniedReasons) || "authentication_unavailable" != fixture.deniedReasons[0] {
                t.Fatalf("expected one denial with reason authentication_unavailable, got %v", fixture.deniedReasons)
            }

            if false == errors.Is(fixture.observedErr, ErrAuthenticationUnavailable) || false == errors.Is(fixture.observedErr, unavailableRouteStoreErr) {
                t.Fatalf("expected the refusal to carry ErrAuthenticationUnavailable and the cause, got %v", fixture.observedErr)
            }

            refusal := exception.AsHttpException(fixture.observedErr)
            if nil == refusal || "service unavailable" != refusal.Message() || "authentication_unavailable" != refusal.Context()["reason"] || "main" != refusal.Context()["firewallName"] {
                t.Fatalf("expected the refusal's message, reason and firewall, got %v", refusal)
            }

            if 1 != len(fixture.deniedCauses) || false == errors.Is(fixture.deniedCauses[0], unavailableRouteStoreErr) {
                t.Fatalf("expected the denial to carry the cause, got %v", fixture.deniedCauses)
            }

            errorRecords := fixture.logger.recordsAtLevel(loggingcontract.LevelError)
            if 1 != len(errorRecords) || 0 != len(fixture.logger.recordsAtLevel(loggingcontract.LevelWarning)) {
                t.Fatalf("expected exactly one Error record and no Warning for the request, got %v", fixture.logger.records)
            }

            if "main" != errorRecords[0].context["firewallName"] || "test" != errorRecords[0].context["tokenSourceName"] || "GET" != errorRecords[0].context["method"] || "/admin" != errorRecords[0].context["path"] {
                t.Fatalf("expected the record to name the firewall, the source, the method and the path, got %v", errorRecords[0].context)
            }
        })
    }
}

func TestAccessControlListener_AnUnavailableTokenOnAPublicRouteIsServed(t *testing.T) {
    for _, path := range []string{"/public", "/elsewhere"} {
        t.Run(path, func(t *testing.T) {
            fixture := newUnavailableRouteFixture(newMarkedUnavailableToken(), bound.Duration{})

            recorder, handled, handlerSawUnavailable := fixture.serve(t, path)

            if nethttp.StatusOK != recorder.Code || false == handled || "" != recorder.Header().Get("Retry-After") {
                t.Fatalf("expected the route served, got %d handled=%v: %s", recorder.Code, handled, recorder.Body.String())
            }

            if false == handlerSawUnavailable {
                t.Fatal("expected the handler to read the unavailable token")
            }

            if 0 != len(fixture.deniedReasons) {
                t.Fatalf("expected no denial, got %v", fixture.deniedReasons)
            }

            if "/public" == path && 1 != fixture.grantedCount {
                t.Fatalf("expected the public rule granted once, got %d", fixture.grantedCount)
            }
        })
    }
}

type unavailableButAuthenticatedToken struct {
    *AuthenticatedToken
}

func (instance *unavailableButAuthenticatedToken) AuthenticationUnavailableCause() error {
    return exception.MarkInfrastructureFailure(errors.New("redis is down"))
}

func TestAccessControlListener_ATokenBothUnavailableAndAuthenticatedIsRefused(t *testing.T) {
    fixture := newUnavailableRouteFixture(&unavailableButAuthenticatedToken{AuthenticatedToken: NewAuthenticatedToken("admin", []string{"ROLE_ADMIN"}, nil)}, bound.Duration{})

    recorder, handled, _ := fixture.serve(t, "/admin")

    if nethttp.StatusServiceUnavailable != recorder.Code || "5" != recorder.Header().Get("Retry-After") || true == handled || "" != fixture.decisionManager.calledMethod {
        t.Fatalf("expected 503 with Retry-After 5 before the decision, got %d %q handled=%v decision=%q", recorder.Code, recorder.Header().Get("Retry-After"), handled, fixture.decisionManager.calledMethod)
    }

    if 1 != len(fixture.logger.recordsAtLevel(loggingcontract.LevelError)) {
        t.Fatalf("expected the one Error record for a token of the application's own type, got %v", fixture.logger.records)
    }
}

func TestAccessControlListener_AnAnonymousTokenKeepsItsEntryPoint(t *testing.T) {
    fixture := newUnavailableRouteFixture(NewAnonymousToken(), bound.Duration{})

    recorder, _, _ := fixture.serve(t, "/admin")

    if nethttp.StatusUnauthorized != recorder.Code || "sign in" != recorder.Body.String() || 1 != fixture.entryPoint.calls || "" != recorder.Header().Get("Retry-After") {
        t.Fatalf("expected the entry point's 401 without Retry-After, got %d %q entry=%d: %s", recorder.Code, recorder.Header().Get("Retry-After"), fixture.entryPoint.calls, recorder.Body.String())
    }

    if 1 != len(fixture.deniedReasons) || "token_not_authenticated" != fixture.deniedReasons[0] {
        t.Fatalf("expected the not-authenticated denial, got %v", fixture.deniedReasons)
    }
}
