package security

import (
    "errors"
    "testing"

    eventcontract "github.com/precision-soft/melody/event/contract"
    httpPkg "github.com/precision-soft/melody/http"
    httpcontract "github.com/precision-soft/melody/http/contract"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
    securitycontract "github.com/precision-soft/melody/security/contract"
)

type accessControlListenerTestAccessDecisionManager struct {
    decideAllErr error
}

func (instance *accessControlListenerTestAccessDecisionManager) DecideAll(token securitycontract.Token, attributes []string, subject any) error {
    return instance.decideAllErr
}

func (instance *accessControlListenerTestAccessDecisionManager) DecideAny(token securitycontract.Token, attributes []string, subject any) error {
    return instance.decideAllErr
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

var _ securitycontract.AccessDeniedHandler = (*accessControlListenerTestAccessDeniedHandler)(nil)

func TestMatchAccessControlRule_RegexRuleIsHonoredAndNotShadowedByEarlierRegex(t *testing.T) {
    accessControl := NewAccessControl(
        NewAccessControlRegexRule("^/health", securitycontract.AttributePublicAccess),
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

func TestAccessControlListener_WhenSecurityContextHasNilToken_EmitsAuthorizationDeniedAndSets401(t *testing.T) {
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

/* @info an entry point that produces no response must not let the request through: without the fail-closed fall-through the listener writes a nil response the kernel reads as "no decision" and the handler runs for an unauthenticated request */
func TestAccessControlListener_WhenEntryPointReturnsNoResponse_FailsClosed(t *testing.T) {
    kernel := newTestKernel()
    runtimeInstance := newTestRuntime()

    entryPoint := &accessControlListenerTestEntryPoint{response: nil, err: nil}

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

    SecurityContextSetOnRuntime(runtimeInstance, NewSecurityContext(firewall, NewAnonymousToken()))

    registry := NewFirewallRegistry(NewCompiledConfiguration([]*CompiledFirewall{firewall}, nil))

    RegisterKernelAccessControlListener(kernel, registry)

    request := newSecurityTestRequest("GET", "/admin", nil, runtimeInstance)
    requestEvent := httpPkg.NewKernelRequestEvent(runtimeInstance, request)

    _, err := kernel.EventDispatcher().DispatchName(runtimeInstance, "kernel.request", requestEvent)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if 1 != entryPoint.calls {
        t.Fatalf("expected the entry point to be called once")
    }
    if nil == requestEvent.Response() {
        t.Fatalf("expected a fail-closed response when the entry point produced none")
    }
}

/* @info when the kernel.exception dispatch produces no response (no exception listener registered, or propagation stopped) the listener must still write a fail-closed response rather than a nil the kernel serves the handler for */
func TestAccessControlListener_WhenExceptionProducesNoResponse_FailsClosed(t *testing.T) {
    kernel := newTestKernel()
    runtimeInstance := newTestRuntime()

    firewall := NewCompiledFirewall(
        "main",
        nil,
        "m",
        nil,
        nil,
        NewAccessControl(NewAccessControlRule("/admin", "ROLE_ADMIN")),
        nil,
        nil,
        nil,
        nil,
        "/admin/login",
        "/admin/logout",
        nil,
        nil,
        SourceFirewall,
        SourceNone,
        SourceFirewall,
        SourceNone,
        SourceNone,
    )

    SecurityContextSetOnRuntime(runtimeInstance, NewSecurityContext(firewall, NewAuthenticatedToken("user", []string{"ROLE_USER"})))

    registry := NewFirewallRegistry(NewCompiledConfiguration([]*CompiledFirewall{firewall}, nil))

    RegisterKernelAccessControlListener(kernel, registry)

    request := newSecurityTestRequest("GET", "/admin", nil, runtimeInstance)
    requestEvent := httpPkg.NewKernelRequestEvent(runtimeInstance, request)

    _, err := kernel.EventDispatcher().DispatchName(runtimeInstance, "kernel.request", requestEvent)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if nil == requestEvent.Response() {
        t.Fatalf("expected a fail-closed response when no exception listener produced one")
    }
}

/* @info an access denied handler that fails must keep the authorization decision as the cause so the denial status survives the cause chain; replacing it with the handler error turns a 403 into whatever the handler failure maps to */
func TestAccessControlListener_WhenDeniedHandlerFails_KeepsDecisionAsCause(t *testing.T) {
    kernel := newTestKernel()
    runtimeInstance := newTestRuntime()

    decisionSentinel := errors.New("forbidden decision")

    deniedHandler := &accessControlListenerTestAccessDeniedHandler{response: nil, err: errors.New("handler render failed")}

    firewall := NewCompiledFirewall(
        "main",
        nil,
        "m",
        nil,
        nil,
        NewAccessControl(NewAccessControlRule("/admin", "ROLE_ADMIN")),
        &accessControlListenerTestAccessDecisionManager{decideAllErr: decisionSentinel},
        nil,
        nil,
        deniedHandler,
        "/admin/login",
        "/admin/logout",
        nil,
        nil,
        SourceFirewall,
        SourceFirewall,
        SourceFirewall,
        SourceNone,
        SourceFirewall,
    )

    SecurityContextSetOnRuntime(runtimeInstance, NewSecurityContext(firewall, NewAuthenticatedToken("user", []string{"ROLE_USER"})))

    registry := NewFirewallRegistry(NewCompiledConfiguration([]*CompiledFirewall{firewall}, nil))

    var capturedErr error
    kernel.EventDispatcher().AddListener(
        "kernel.exception",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            exceptionEvent, ok := eventValue.Payload().(*httpPkg.KernelExceptionEvent)
            if true == ok && nil != exceptionEvent {
                capturedErr = exceptionEvent.Err()
            }
            return nil
        },
        0,
    )

    RegisterKernelAccessControlListener(kernel, registry)

    request := newSecurityTestRequest("GET", "/admin", nil, runtimeInstance)
    requestEvent := httpPkg.NewKernelRequestEvent(runtimeInstance, request)

    _, err := kernel.EventDispatcher().DispatchName(runtimeInstance, "kernel.request", requestEvent)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if 1 != deniedHandler.calls {
        t.Fatalf("expected the denied handler to be called once")
    }
    if false == errors.Is(capturedErr, decisionSentinel) {
        t.Fatalf("expected the authorization decision to survive as the cause, got %v", capturedErr)
    }
}
