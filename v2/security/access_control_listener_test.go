package security

import (
    "context"
    "errors"
    "io"
    "os"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v2/container"
    eventcontract "github.com/precision-soft/melody/v2/event/contract"
    httpPkg "github.com/precision-soft/melody/v2/http"
    httpcontract "github.com/precision-soft/melody/v2/http/contract"
    "github.com/precision-soft/melody/v2/logging"
    loggingcontract "github.com/precision-soft/melody/v2/logging/contract"
    "github.com/precision-soft/melody/v2/runtime"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v2/security/contract"
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
        NewAccessControlRawPrefixRule("", "ROLE_ANY"),
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

/* an entry point that produces no response must not let the request through: without the fail-closed fall-through the listener writes a nil response the kernel reads as "no decision" and the handler runs for an unauthenticated request */
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

/* when the kernel.exception dispatch produces no response (no exception listener registered, or propagation stopped) the listener must still write a fail-closed response rather than a nil the kernel serves the handler for */
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

/* an access denied handler that fails must keep the authorization decision as the cause so the denial status survives the cause chain; replacing it with the handler error turns a 403 into whatever the handler failure maps to */
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

/* the direct 401 leaves one warning naming its reason: it used to complete with no trace at all, while the byte-identical-looking 403 filed a warning — whether the token was absent, unauthenticated, or the security context missing entirely was indistinguishable from the journal. */
func TestAccessControlListener_ADirect401LeavesOneWarningNamingItsReason(t *testing.T) {
    kernel := newTestKernel()

    capture := &refusalCaptureLogger{Logger: logging.NewNopLogger()}
    scope := newTestScope()
    if overrideErr := scope.OverrideInstance(logging.ServiceLogger, capture); nil != overrideErr {
        t.Fatalf("could not install the capturing logger: %v", overrideErr)
    }
    runtimeInstance := runtime.New(context.Background(), scope, container.NewContainer())

    registry := NewFirewallRegistry(
        NewCompiledConfiguration(nil, NewAccessControl(NewAccessControlRule("/admin", "ROLE_ADMIN"))),
    )

    RegisterKernelAccessControlListener(kernel, registry)

    requestEvent := httpPkg.NewKernelRequestEvent(runtimeInstance, newSecurityTestRequest("GET", "/admin", nil, runtimeInstance))

    _, dispatchErr := kernel.EventDispatcher().DispatchName(runtimeInstance, "kernel.request", requestEvent)
    if nil != dispatchErr {
        t.Fatalf("unexpected error: %v", dispatchErr)
    }

    if nil == requestEvent.Response() || 401 != requestEvent.Response().StatusCode() {
        t.Fatal("expected the 401 response")
    }

    if 1 != len(capture.warningRecords) {
        t.Fatalf("expected exactly one warning record, got %v", capture.warningRecords)
    }

    if "authorization refused" != capture.warningRecords[0].message || "missing_security_context" != capture.warningRecords[0].context["reason"] {
        t.Fatalf("expected the refusal named with its reason, got %+v", capture.warningRecords[0])
    }
}

type refusalRecord struct {
    message string
    context loggingcontract.Context
}

type refusalCaptureLogger struct {
    loggingcontract.Logger
    warningRecords []refusalRecord
    errorRecords   []refusalRecord
}

func (instance *refusalCaptureLogger) Warning(message string, context loggingcontract.Context) {
    instance.warningRecords = append(instance.warningRecords, refusalRecord{message: message, context: context})
}

func (instance *refusalCaptureLogger) Error(message string, context loggingcontract.Context) {
    instance.errorRecords = append(instance.errorRecords, refusalRecord{message: message, context: context})
}

/* newRefusalCaptureRuntime installs a capturing logger on a scope, so a test can count what one refusal filed. */
func newRefusalCaptureRuntime(t *testing.T) (runtimecontract.Runtime, *refusalCaptureLogger) {
    t.Helper()

    capture := &refusalCaptureLogger{Logger: logging.NewNopLogger()}

    scope := newTestScope()
    if overrideErr := scope.OverrideInstance(logging.ServiceLogger, capture); nil != overrideErr {
        t.Fatalf("could not install the capturing logger: %v", overrideErr)
    }

    return runtime.New(context.Background(), scope, container.NewContainer()), capture
}

func newRefusalTestFirewall(accessDecisionManager securitycontract.AccessDecisionManager) *CompiledFirewall {
    return NewCompiledFirewall(
        "admin-area",
        nil,
        "m",
        nil,
        nil,
        NewAccessControl(NewAccessControlRule("/admin", "ROLE_ADMIN")),
        accessDecisionManager,
        nil,
        nil,
        nil,
        "",
        "",
        nil,
        nil,
        SourceFirewall,
        SourceFirewall,
        SourceFirewall,
        SourceNone,
        SourceNone,
    )
}

func dispatchRefusal(t *testing.T, runtimeInstance runtimecontract.Runtime, firewall *CompiledFirewall) *httpPkg.KernelRequestEvent {
    t.Helper()

    SecurityContextSetOnRuntime(runtimeInstance, NewSecurityContext(firewall, NewAuthenticatedToken("user", []string{"ROLE_USER"})))

    kernel := newTestKernel()
    httpPkg.RegisterKernelExceptionListener(kernel.EventDispatcher(), false)
    RegisterKernelAccessControlListener(kernel, NewFirewallRegistry(NewCompiledConfiguration([]*CompiledFirewall{firewall}, nil)))

    requestEvent := httpPkg.NewKernelRequestEvent(runtimeInstance, newSecurityTestRequest("GET", "/admin", nil, runtimeInstance))

    if _, dispatchErr := kernel.EventDispatcher().DispatchName(runtimeInstance, "kernel.request", requestEvent); nil != dispatchErr {
        t.Fatalf("unexpected error: %v", dispatchErr)
    }

    return requestEvent
}

/* a real denial leaves one warning naming the branch, the firewall and the matched rule; it used to leave only the exception listener's generic "unhandled exception" carrying the word forbidden and nothing else */
func TestAccessControlListener_A403LeavesOneWarningNamingItsBranch(t *testing.T) {
    runtimeInstance, capture := newRefusalCaptureRuntime(t)

    requestEvent := dispatchRefusal(t, runtimeInstance, newRefusalTestFirewall(NewAccessDecisionManager(securitycontract.DecisionStrategyAffirmative, NewRoleVoter())))

    if nil == requestEvent.Response() || 403 != requestEvent.Response().StatusCode() {
        t.Fatalf("expected the 403 response")
    }

    if 1 != len(capture.warningRecords) || 0 != len(capture.errorRecords) {
        t.Fatalf("expected exactly one warning and no error, got %v and %v", capture.warningRecords, capture.errorRecords)
    }

    record := capture.warningRecords[0]
    if "authorization refused" != record.message {
        t.Fatalf("expected the refusal message, got %q", record.message)
    }

    if RefusalReasonAffirmativeNoGrant != record.context["reason"] {
        t.Fatalf("expected the branch named, got %v", record.context["reason"])
    }

    if "admin-area" != record.context["firewallName"] || "/admin" != record.context["matchedRule"] {
        t.Fatalf("expected the firewall and the matched rule named, got %+v", record.context)
    }
}

/* a firewall naming an attribute no configured voter looks at is a wiring fault: it answers the same 403 fail-closed, and it is the one refusal filed at error */
func TestAccessControlListener_TheWiringFault403IsRecordedAtError(t *testing.T) {
    runtimeInstance, capture := newRefusalCaptureRuntime(t)

    requestEvent := dispatchRefusal(t, runtimeInstance, newRefusalTestFirewall(NewAccessDecisionManager(securitycontract.DecisionStrategyAffirmative)))

    if nil == requestEvent.Response() || 403 != requestEvent.Response().StatusCode() {
        t.Fatalf("expected the refusal to stay fail-closed at 403")
    }

    if 1 != len(capture.errorRecords) || 0 != len(capture.warningRecords) {
        t.Fatalf("expected exactly one error and no warning, got %v and %v", capture.errorRecords, capture.warningRecords)
    }

    if RefusalReasonNoVoterSupportsAttribute != capture.errorRecords[0].context["reason"] {
        t.Fatalf("expected the wiring fault named, got %v", capture.errorRecords[0].context["reason"])
    }

    refusedAttributes, isStringList := capture.errorRecords[0].context["attributes"].([]string)
    if false == isStringList || 1 != len(refusedAttributes) || "ROLE_ADMIN" != refusedAttributes[0] {
        t.Fatalf("expected the record to name the attribute nothing votes on, got %v", capture.errorRecords[0].context["attributes"])
    }
}

/* an access denied handler that answers the request returns before any kernel.exception dispatch, so its exit used to leave no trace of the refusal it had just answered */
func TestAccessControlListener_ADeniedHandlerThatAnswersStillLeavesOneRecord(t *testing.T) {
    runtimeInstance, capture := newRefusalCaptureRuntime(t)

    firewall := NewCompiledFirewall(
        "admin-area",
        nil,
        "m",
        nil,
        nil,
        NewAccessControl(NewAccessControlRule("/admin", "ROLE_ADMIN")),
        NewAccessDecisionManager(securitycontract.DecisionStrategyAffirmative, NewRoleVoter()),
        nil,
        nil,
        &accessControlListenerTestAccessDeniedHandler{response: httpPkg.JsonErrorResponse(403, "denied"), err: nil},
        "",
        "",
        nil,
        nil,
        SourceFirewall,
        SourceFirewall,
        SourceFirewall,
        SourceNone,
        SourceFirewall,
    )

    dispatchRefusal(t, runtimeInstance, firewall)

    if 1 != len(capture.warningRecords) {
        t.Fatalf("expected the answered refusal to leave one record, got %v", capture.warningRecords)
    }
}

/* a decision manager of the application's own may refuse with a plain error, which has nowhere for the already-logged mark to live: the listener files its record and hands the exception dispatch a marked carrier, so the exception listener attaches its coordinates instead of filing the same refusal a second time */
func TestAccessControlListener_ARefusalCarryingNoHttpExceptionStillFilesOneRecord(t *testing.T) {
    runtimeInstance, capture := newRefusalCaptureRuntime(t)

    dispatchRefusal(
        t,
        runtimeInstance,
        newRefusalTestFirewall(&accessControlListenerTestAccessDecisionManager{decideAllErr: errors.New("refused by the application's own manager")}),
    )

    if 1 != len(capture.warningRecords) || 0 != len(capture.errorRecords) {
        t.Fatalf("expected one record for one refusal, got %v and %v", capture.warningRecords, capture.errorRecords)
    }
}

/* a denied handler that FAILS leaves the one record it always left, but at error and naming its own failure: the record used to be filed above the handler call with the DECISION's reason and the mark set below suppressed the exception listener that carried the wrap, so a permanently broken refusal page reached no channel at all */
func TestAccessControlListener_AFailingDeniedHandlerIsNamedInTheOneRecord(t *testing.T) {
    runtimeInstance, capture := newRefusalCaptureRuntime(t)

    firewall := NewCompiledFirewall(
        "admin-area",
        nil,
        "m",
        nil,
        nil,
        NewAccessControl(NewAccessControlRule("/admin", "ROLE_ADMIN")),
        NewAccessDecisionManager(securitycontract.DecisionStrategyAffirmative, NewRoleVoter()),
        nil,
        nil,
        &accessControlListenerTestAccessDeniedHandler{response: nil, err: errors.New("the denied page template is missing")},
        "",
        "",
        nil,
        nil,
        SourceFirewall,
        SourceFirewall,
        SourceFirewall,
        SourceNone,
        SourceFirewall,
    )

    dispatchRefusal(t, runtimeInstance, firewall)

    if 1 != len(capture.errorRecords) || 0 != len(capture.warningRecords) {
        t.Fatalf("expected exactly one error and no warning, got %v and %v", capture.errorRecords, capture.warningRecords)
    }

    record := capture.errorRecords[0]

    if "failed" != record.context["deniedHandlerOutcome"] {
        t.Fatalf("expected the record to name what the handler did, got %+v", record.context)
    }

    handlerError, isString := record.context["handlerError"].(string)
    if false == isString || "the denied page template is missing" != handlerError {
        t.Fatalf("expected the handler's own failure in the record, got %v", record.context["handlerError"])
    }

    if RefusalReasonAffirmativeNoGrant != record.context["reason"] {
        t.Fatalf("expected the decision branch to survive beside the handler failure, got %v", record.context["reason"])
    }
}

/* a denied handler that answers nil without failing is the same broken page by another spelling, and earns the same record */
func TestAccessControlListener_ADeniedHandlerAnsweringNilIsNamedInTheOneRecord(t *testing.T) {
    runtimeInstance, capture := newRefusalCaptureRuntime(t)

    firewall := NewCompiledFirewall(
        "admin-area",
        nil,
        "m",
        nil,
        nil,
        NewAccessControl(NewAccessControlRule("/admin", "ROLE_ADMIN")),
        NewAccessDecisionManager(securitycontract.DecisionStrategyAffirmative, NewRoleVoter()),
        nil,
        nil,
        &accessControlListenerTestAccessDeniedHandler{response: nil, err: nil},
        "",
        "",
        nil,
        nil,
        SourceFirewall,
        SourceFirewall,
        SourceFirewall,
        SourceNone,
        SourceFirewall,
    )

    dispatchRefusal(t, runtimeInstance, firewall)

    if 1 != len(capture.errorRecords) || 0 != len(capture.warningRecords) {
        t.Fatalf("expected exactly one error and no warning, got %v and %v", capture.errorRecords, capture.warningRecords)
    }

    if "nil_response" != capture.errorRecords[0].context["deniedHandlerOutcome"] {
        t.Fatalf("expected the record to name what the handler did, got %+v", capture.errorRecords[0].context)
    }
}

/* a handler that answers the request keeps the refusal at the level the DECISION earned, and keeps carrying no handler outcome: the record moved below the call, and moving it must not change what a working handler files */
func TestAccessControlListener_AnAnsweringDeniedHandlerKeepsTheDecisionLevel(t *testing.T) {
    runtimeInstance, capture := newRefusalCaptureRuntime(t)

    firewall := NewCompiledFirewall(
        "admin-area",
        nil,
        "m",
        nil,
        nil,
        NewAccessControl(NewAccessControlRule("/admin", "ROLE_ADMIN")),
        NewAccessDecisionManager(securitycontract.DecisionStrategyAffirmative, NewRoleVoter()),
        nil,
        nil,
        &accessControlListenerTestAccessDeniedHandler{response: httpPkg.JsonErrorResponse(403, "denied"), err: nil},
        "",
        "",
        nil,
        nil,
        SourceFirewall,
        SourceFirewall,
        SourceFirewall,
        SourceNone,
        SourceFirewall,
    )

    dispatchRefusal(t, runtimeInstance, firewall)

    if 1 != len(capture.warningRecords) || 0 != len(capture.errorRecords) {
        t.Fatalf("expected one warning and no error, got %v and %v", capture.warningRecords, capture.errorRecords)
    }

    if _, exists := capture.warningRecords[0].context["deniedHandlerOutcome"]; true == exists {
        t.Fatalf("expected a working handler to leave no outcome key, got %+v", capture.warningRecords[0].context)
    }
}

type accessControlListenerTestTypedNilManager struct {
    voters []securitycontract.Voter
}

func (instance *accessControlListenerTestTypedNilManager) DecideAll(token securitycontract.Token, attributes []string, subject any) error {
    if 0 == len(instance.voters) {
        return nil
    }

    return nil
}

func (instance *accessControlListenerTestTypedNilManager) DecideAny(token securitycontract.Token, attributes []string, subject any) error {
    return instance.DecideAll(token, attributes, subject)
}

/* a firewall assembled through NewCompiledFirewall never passes the compile step that refuses a typed nil, so the listener judges the interface rather than comparing it: the plain comparison let a manager holding nothing reach DecideAll, where it dereferenced its own nil receiver and answered every request behind the firewall with a recovered panic */
func TestAccessControlListener_ATypedNilDecisionManagerAnswersTheMissingManagerBranch(t *testing.T) {
    runtimeInstance, _ := newRefusalCaptureRuntime(t)

    var typedNilManager *accessControlListenerTestTypedNilManager

    firewall := NewCompiledFirewall(
        "admin-area",
        nil,
        "m",
        nil,
        nil,
        NewAccessControl(NewAccessControlRule("/admin", "ROLE_ADMIN")),
        typedNilManager,
        nil,
        nil,
        nil,
        "",
        "",
        nil,
        nil,
        SourceFirewall,
        SourceFirewall,
        SourceFirewall,
        SourceNone,
        SourceNone,
    )

    requestEvent := dispatchRefusal(t, runtimeInstance, firewall)

    if nil == requestEvent.Response() {
        t.Fatalf("expected the missing-manager branch to answer the request")
    }

    if 500 != requestEvent.Response().StatusCode() {
        t.Fatalf("expected the missing-manager response, got %d", requestEvent.Response().StatusCode())
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
