package security

import (
    "errors"
    "io"
    "os"
    "strings"
    "testing"

    eventcontract "github.com/precision-soft/melody/v3/event/contract"
    "github.com/precision-soft/melody/v3/exception"
    httpPkg "github.com/precision-soft/melody/v3/http"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/logging"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
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
    control := NewAccessControl(
        NewAccessControlRule("/admin", "ROLE_ADMIN"),
        NewAccessControlRule("/admin/settings", "ROLE_SETTINGS"),
    )

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
    control := NewAccessControl(
        NewAccessControlRule("/", "ROLE_ROOT"),
    )

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
    control := NewAccessControl(
        NewAccessControlRule("", "ROLE_ANY"),
        NewAccessControlRule("/admin", "ROLE_ADMIN"),
    )

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
    accessControl := NewAccessControl(
        NewAccessControlRegexRule("^/health(/|$)", securitycontract.AttributePublicAccess),
        NewAccessControlRegexRule("^/admin", "ROLE_ADMIN"),
    )

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

func TestMatchAccessControlRule_ExactRuleDoesNotMatchPrefixPaths(t *testing.T) {
    accessControl := NewAccessControl(
        NewAccessControlExactRule("/admin", "ROLE_ADMIN"),
    )

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
    accessControl := NewAccessControl(
        NewAccessControlRuleWithSegmentPrefix("/admin", "ROLE_ADMIN"),
    )

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
        NewCompiledConfiguration(nil, NewAccessControl(NewAccessControlRule("/admin", "ROLE_ADMIN"))),
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

/* the security context is PUT on the runtime here, carrying a nil token. Without that the listener never reaches the token check at all — it answers from the missing-context branch above, which is a different refusal for a different reason, and the whole nil-token block could be deleted with this test still green. The reason is what tells the two apart, so it is what this asserts. */
func TestAccessControlListener_WhenSecurityContextHasNilToken_EmitsAuthorizationDeniedAndSets401(t *testing.T) {
    kernel := newTestKernel()
    runtimeInstance := newTestRuntime()

    firewall := NewCompiledFirewall(
        "main",
        nil,
        "m",
        nil,
        nil,
        NewAccessControl(NewAccessControlRule("/admin", "ROLE_ADMIN")),
        &accessControlListenerTestAccessDecisionManager{decideAllErr: nil},
        nil,
        nil,
        nil,
        "/admin/login",
        "/admin/logout",
        nil,
        nil,
        SourceFirewall,
        SourceFirewall,
        SourceFirewall,
        SourceFirewall,
        SourceNone,
    )

    registry := NewFirewallRegistry(
        NewCompiledConfiguration(nil, NewAccessControl(NewAccessControlRule("/admin", "ROLE_ADMIN"))),
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

    firewall := NewCompiledFirewall(
        "main",
        nil,
        "m",
        nil,
        nil,
        NewAccessControl(NewAccessControlRule("/admin", "ROLE_ADMIN")),
        &accessControlListenerTestAccessDecisionManager{decideAllErr: nil},
        nil,
        entryPoint,
        nil,
        "/admin/login",
        "/admin/logout",
        nil,
        nil,
        SourceFirewall,
        SourceFirewall,
        SourceFirewall,
        SourceFirewall,
        SourceNone,
    )

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

    firewall := NewCompiledFirewall(
        "main",
        nil,
        "m",
        nil,
        nil,
        NewAccessControl(NewAccessControlRule("/admin", "ROLE_ADMIN")),
        &accessControlListenerTestAccessDecisionManager{decideAllErr: nil},
        nil,
        nil,
        nil,
        "/admin/login",
        "/admin/logout",
        nil,
        nil,
        SourceFirewall,
        SourceFirewall,
        SourceFirewall,
        SourceNone,
        SourceNone,
    )

    securityContext := NewSecurityContext(firewall, NewAuthenticatedToken("user", []string{"ROLE_ADMIN"}))
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

func TestAccessControlListener_WhenDecisionDenied_EmitsAuthorizationDeniedAndSetsExceptionResponse(t *testing.T) {
    kernel := newTestKernel()
    runtimeInstance := newTestRuntime()

    firewall := NewCompiledFirewall(
        "main",
        nil,
        "m",
        nil,
        nil,
        NewAccessControl(NewAccessControlRule("/admin", "ROLE_ADMIN")),
        &accessControlListenerTestAccessDecisionManager{decideAllErr: errors.New("denied")},
        nil,
        nil,
        nil,
        "/admin/login",
        "/admin/logout",
        nil,
        nil,
        SourceFirewall,
        SourceFirewall,
        SourceFirewall,
        SourceNone,
        SourceNone,
    )

    securityContext := NewSecurityContext(firewall, NewAuthenticatedToken("user", []string{"ROLE_USER"}))
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

    firewall := NewCompiledFirewall(
        "main", nil, "m", nil, nil,
        NewAccessControl(NewAccessControlRule("/admin", "ROLE_ADMIN")),
        &accessControlListenerTestAccessDecisionManager{decideAllErr: nil},
        nil, entryPoint, nil,
        "/admin/login", "/admin/logout", nil, nil,
        SourceFirewall, SourceFirewall, SourceFirewall, SourceFirewall, SourceNone,
    )

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

    firewall := NewCompiledFirewall(
        "main", nil, "m", nil, nil,
        NewAccessControl(NewAccessControlRule("/admin", "ROLE_ADMIN")),
        &accessControlListenerTestAccessDecisionManager{decideAllErr: nil},
        nil, entryPoint, nil,
        "/admin/login", "/admin/logout", nil, nil,
        SourceFirewall, SourceFirewall, SourceFirewall, SourceFirewall, SourceNone,
    )

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

    firewall := NewCompiledFirewall(
        "main", nil, "m", nil, nil,
        NewAccessControl(NewAccessControlRule("/admin", "ROLE_ADMIN")),
        &accessControlListenerTestAccessDecisionManager{decideAllErr: errors.New("denied")},
        nil, nil,
        &accessControlListenerTestAccessDeniedHandler{response: (*httpPkg.Response)(nil), err: nil},
        "/admin/login", "/admin/logout", nil, nil,
        SourceFirewall, SourceFirewall, SourceFirewall, SourceNone, SourceFirewall,
    )

    securityContext := NewSecurityContext(firewall, NewAuthenticatedToken("user", []string{"ROLE_USER"}))
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

    firewall := NewCompiledFirewall(
        "main", nil, "m", nil, nil,
        NewAccessControl(NewAccessControlRule("/admin", "ROLE_ADMIN")),
        &accessControlListenerTestAccessDecisionManager{decideAllErr: errors.New("denied")},
        nil, nil, nil,
        "/admin/login", "/admin/logout", nil, nil,
        SourceFirewall, SourceFirewall, SourceFirewall, SourceNone, SourceNone,
    )

    securityContext := NewSecurityContext(firewall, NewAuthenticatedToken("user", []string{"ROLE_USER"}))
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

/* A nil pointer of a request type is a non-nil interface, so the bare comparison this replaces carried it
past the gate and into the path read below, which dereferences it — inside a kernel listener, where no
recover covers it. The listener must leave such an event alone, not crash the request. */
func TestAccessControlListener_ATypedNilRequestIsLeftAlone(t *testing.T) {
    kernel := newTestKernel()
    runtimeInstance := newTestRuntime()

    registry := NewFirewallRegistry(
        NewCompiledConfiguration(nil, NewAccessControl(NewAccessControlRule("/admin", "ROLE_ADMIN"))),
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

/* the listener asks for ALL of a rule's attributes, and nothing in this file could tell that apart from ANY: the stub answered one field for both decisions and every rule carried a single attribute, so DecideAny(token, nil, nil) would have satisfied the whole suite. Here the two decisions disagree — all refuses, any accepts — and the rule carries two attributes of which the token holds one, which is exactly the input on which the semantics differ. */
func TestAccessControlListener_TheDecisionIsDecideAllOverTheWholeAttributeSet(t *testing.T) {
    kernel := newTestKernel()
    runtimeInstance := newTestRuntime()

    decisionManager := &accessControlListenerTestAccessDecisionManager{
        decideAllErr: errors.New("access denied"),
        decideAnyErr: nil,
    }

    firewall := NewCompiledFirewall(
        "main",
        nil,
        "m",
        nil,
        nil,
        NewAccessControl(NewAccessControlRule("/admin", "ROLE_ADMIN", "ROLE_AUDITOR")),
        decisionManager,
        nil,
        nil,
        nil,
        "/admin/login",
        "/admin/logout",
        nil,
        nil,
        SourceFirewall,
        SourceFirewall,
        SourceFirewall,
        SourceNone,
        SourceNone,
    )

    securityContext := NewSecurityContext(firewall, NewAuthenticatedToken("user", []string{"ROLE_ADMIN"}))
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

/* TestRegisterKernelAccessControlListener_ADispatcherWithoutTheCapabilityIsNamed pins the branch that used to be silent. The required-listener mark is what makes a listener stopping propagation ahead of access control fail the dispatch closed instead of letting the request reach its handler unchecked; a dispatcher that cannot take the mark disarms that guarantee for the whole process, and the framework's own event adapter refuses the very same condition with a panic rather than swallowing it. The record goes to the emergency channel because this runs at boot, before the configured logger is resolvable. */
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
        NewFirewallRegistry(NewCompiledConfiguration(nil, NewAccessControl(NewAccessControlRule("/admin", "ROLE_ADMIN")))),
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
