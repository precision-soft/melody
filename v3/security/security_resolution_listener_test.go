package security

import (
    "context"
    "errors"
    "testing"

    "github.com/precision-soft/melody/v3/container"
    eventcontract "github.com/precision-soft/melody/v3/event/contract"
    "github.com/precision-soft/melody/v3/exception"
    httpPkg "github.com/precision-soft/melody/v3/http"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

type resolutionListenerTestTokenSource struct {
    resolveToken securitycontract.Token
    resolveErr   error
}

func (instance *resolutionListenerTestTokenSource) Name() string { return "test" }

func (instance *resolutionListenerTestTokenSource) Resolve(runtimeInstance runtimecontract.Runtime, request httpcontract.Request) (securitycontract.Token, error) {
    return instance.resolveToken, instance.resolveErr
}

type resolutionListenerTestPanickingTokenSource struct{}

func (instance *resolutionListenerTestPanickingTokenSource) Name() string { return "panicking" }

func (instance *resolutionListenerTestPanickingTokenSource) Resolve(runtimeInstance runtimecontract.Runtime, request httpcontract.Request) (securitycontract.Token, error) {
    panic("database connection failed")
}

type resolutionListenerTestMatcher struct {
    matches bool
}

func (instance *resolutionListenerTestMatcher) Matches(request httpcontract.Request) bool {
    return instance.matches
}

type resolutionListenerTestRule struct {
    err error
}

func (instance *resolutionListenerTestRule) Applies(request httpcontract.Request) bool { return true }

func (instance *resolutionListenerTestRule) Check(request httpcontract.Request) error {
    return instance.err
}

func TestSecurityResolutionListener_SetsSecurityContextOnRuntime_OnSuccess(t *testing.T) {
    kernel := newTestKernel()
    runtimeInstance := newTestRuntime()

    token := NewAuthenticatedToken("user", []string{"ROLE_USER"})

    firewall := NewCompiledFirewall(
        "main",
        &resolutionListenerTestMatcher{matches: true},
        "matcher:main",
        []securitycontract.Rule{},
        &resolutionListenerTestTokenSource{
            resolveToken: token,
            resolveErr:   nil,
        },
        NewAccessControl(
            NewAccessControlRule("/admin", "ROLE_ADMIN"),
        ),
        NewAccessDecisionManager(
            securitycontract.DecisionStrategyAffirmative,
            NewRoleHierarchyVoter(
                NewRoleHierarchy(map[string][]string{}),
                NewRoleVoter(),
            ),
        ),
        NewRoleHierarchy(map[string][]string{}),
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

    registry := NewFirewallRegistry(
        NewCompiledConfiguration([]*CompiledFirewall{firewall}, nil),
    )

    registerTestKernelExceptionListener(kernel)
    RegisterKernelSecurityResolutionListener(kernel, registry)

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

    securityContext, exists := SecurityContextFromRuntime(runtimeInstance)
    if false == exists {
        t.Fatalf("expected security context to be set on runtime")
    }
    if nil == securityContext {
        t.Fatalf("expected security context")
    }
    if "main" != securityContext.Firewall().Name() {
        t.Fatalf("unexpected firewall name")
    }
    if nil == securityContext.Firewall() {
        t.Fatalf("expected compiled firewall on security context")
    }
    if "matcher:main" != securityContext.MatchedFirewallMatcher() {
        t.Fatalf("unexpected matcher description")
    }
    if false == securityContext.Token().IsAuthenticated() {
        t.Fatalf("expected authenticated token")
    }
}

func TestSecurityResolutionListener_WhenFirewallRuleFails_SetsSecurityContextWithAnonymousToken(t *testing.T) {
    kernel := newTestKernel()
    runtimeInstance := newTestRuntime()

    firewall := NewCompiledFirewall(
        "main",
        &resolutionListenerTestMatcher{matches: true},
        "matcher:main",
        []securitycontract.Rule{
            &resolutionListenerTestRule{err: errors.New("denied")},
        },
        &resolutionListenerTestTokenSource{
            resolveToken: NewAnonymousToken(),
            resolveErr:   nil,
        },
        nil,
        nil,
        nil,
        nil,
        nil,
        "/admin/login",
        "/admin/logout",
        nil,
        nil,
        SourceNone,
        SourceNone,
        SourceNone,
        SourceNone,
        SourceNone,
    )

    registry := NewFirewallRegistry(
        NewCompiledConfiguration([]*CompiledFirewall{firewall}, nil),
    )

    registerTestKernelExceptionListener(kernel)
    RegisterKernelSecurityResolutionListener(kernel, registry)

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

    if nil == requestEvent.Response() {
        t.Fatalf("expected response to be set on request event")
    }

    securityContext, exists := SecurityContextFromRuntime(runtimeInstance)
    if false == exists {
        t.Fatalf("expected security context to be set on runtime even when firewall rule fails")
    }
    if nil == securityContext {
        t.Fatalf("expected security context")
    }
    if true == securityContext.Token().IsAuthenticated() {
        t.Fatalf("expected anonymous token when firewall rule fails")
    }
}

func TestSecurityResolutionListener_WhenTokenSourceErrors_SetsSecurityContextWithAnonymousToken(t *testing.T) {
    kernel := newTestKernel()
    runtimeInstance := newTestRuntime()

    firewall := NewCompiledFirewall(
        "main",
        &resolutionListenerTestMatcher{matches: true},
        "matcher:main",
        []securitycontract.Rule{},
        &resolutionListenerTestTokenSource{
            resolveToken: nil,
            resolveErr:   errors.New("token error"),
        },
        nil,
        nil,
        nil,
        nil,
        nil,
        "/admin/login",
        "/admin/logout",
        nil,
        nil,
        SourceNone,
        SourceNone,
        SourceNone,
        SourceNone,
        SourceNone,
    )

    registry := NewFirewallRegistry(
        NewCompiledConfiguration([]*CompiledFirewall{firewall}, nil),
    )

    registerTestKernelExceptionListener(kernel)
    RegisterKernelSecurityResolutionListener(kernel, registry)

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

    if nil == requestEvent.Response() {
        t.Fatalf("expected response to be set on request event")
    }

    securityContext, exists := SecurityContextFromRuntime(runtimeInstance)
    if false == exists {
        t.Fatalf("expected security context to be set on runtime even when token source errors")
    }
    if nil == securityContext {
        t.Fatalf("expected security context")
    }
    if true == securityContext.Token().IsAuthenticated() {
        t.Fatalf("expected anonymous token when token source errors")
    }
}

func TestSecurityResolutionListener_WhenTokenSourcePanics_SetsSecurityContextWithAnonymousToken(t *testing.T) {
    kernel := newTestKernel()
    runtimeInstance := newTestRuntime()

    firewall := NewCompiledFirewall(
        "main",
        &resolutionListenerTestMatcher{matches: true},
        "matcher:main",
        []securitycontract.Rule{},
        &resolutionListenerTestPanickingTokenSource{},
        nil,
        nil,
        nil,
        nil,
        nil,
        "/admin/login",
        "/admin/logout",
        nil,
        nil,
        SourceNone,
        SourceNone,
        SourceNone,
        SourceNone,
        SourceNone,
    )

    registry := NewFirewallRegistry(
        NewCompiledConfiguration([]*CompiledFirewall{firewall}, nil),
    )

    registerTestKernelExceptionListener(kernel)
    RegisterKernelSecurityResolutionListener(kernel, registry)

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

    if nil == requestEvent.Response() {
        t.Fatalf("expected response to be set on request event")
    }

    securityContext, exists := SecurityContextFromRuntime(runtimeInstance)
    if false == exists {
        t.Fatalf("expected security context to be set on runtime even when token source panics")
    }
    if nil == securityContext {
        t.Fatalf("expected security context")
    }
    if true == securityContext.Token().IsAuthenticated() {
        t.Fatalf("expected anonymous token when token source panics")
    }
}

func TestSecurityResolutionListener_WhenTokenSourceReturnsNilToken_SetsAnonymousToken(t *testing.T) {
    kernel := newTestKernel()
    runtimeInstance := newTestRuntime()

    firewall := NewCompiledFirewall(
        "main",
        &resolutionListenerTestMatcher{matches: true},
        "matcher:main",
        []securitycontract.Rule{},
        &resolutionListenerTestTokenSource{
            resolveToken: nil,
            resolveErr:   nil,
        },
        nil,
        nil,
        nil,
        nil,
        nil,
        "/admin/login",
        "/admin/logout",
        nil,
        nil,
        SourceNone,
        SourceNone,
        SourceNone,
        SourceNone,
        SourceNone,
    )

    registry := NewFirewallRegistry(
        NewCompiledConfiguration([]*CompiledFirewall{firewall}, nil),
    )

    registerTestKernelExceptionListener(kernel)
    RegisterKernelSecurityResolutionListener(kernel, registry)

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

    securityContext, exists := SecurityContextFromRuntime(runtimeInstance)
    if false == exists {
        t.Fatalf("expected security context to be set on runtime")
    }
    if nil == securityContext {
        t.Fatalf("expected security context")
    }
    if nil == securityContext.Token() {
        t.Fatalf("expected token to not be nil")
    }
    if true == securityContext.Token().IsAuthenticated() {
        t.Fatalf("expected anonymous token when token source returns nil")
    }
}

type resolutionListenerCaptureLogger struct {
    errorCalls   int
    warningCalls int
    lastMessage  string
    lastContext  map[string]any
}

func (instance *resolutionListenerCaptureLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
    if loggingcontract.LevelError == level {
        instance.errorCalls++
        instance.lastMessage = message
        instance.lastContext = context
    }
    if loggingcontract.LevelWarning == level {
        instance.warningCalls++
        instance.lastMessage = message
        instance.lastContext = context
    }
}

func (instance *resolutionListenerCaptureLogger) Debug(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelDebug, message, context)
}

func (instance *resolutionListenerCaptureLogger) Info(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelInfo, message, context)
}

func (instance *resolutionListenerCaptureLogger) Warning(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelWarning, message, context)
}

func (instance *resolutionListenerCaptureLogger) Error(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelError, message, context)
}

func (instance *resolutionListenerCaptureLogger) Emergency(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelEmergency, message, context)
}

var _ loggingcontract.Logger = (*resolutionListenerCaptureLogger)(nil)

func newResolutionListenerTestRuntimeWithLogger(logger loggingcontract.Logger) runtimecontract.Runtime {
    scope := newTestScope()
    serviceContainer := container.NewContainer()

    overrideErr := scope.OverrideInstance(logging.ServiceLogger, logger)
    if nil != overrideErr {
        panic(overrideErr)
    }

    return runtime.New(context.Background(), scope, serviceContainer)
}

func TestSecurityResolutionListener_ResolveFailureIsRecordedOnceWithTheRequestCoordinates(t *testing.T) {
    capture := dispatchResolutionFailureWithSource(t, errors.New("token backend down"))

    if 1 != capture.errorCalls {
        t.Fatalf("expected exactly one record for the resolution failure, got %d", capture.errorCalls)
    }

    if "security token source resolution failed" != capture.lastMessage {
        t.Fatalf("expected the security record to own the failure, got %q", capture.lastMessage)
    }

    if "GET" != capture.lastContext["method"] {
        t.Fatalf("expected the method on the record, got %v", capture.lastContext["method"])
    }

    if "/admin" != capture.lastContext["path"] {
        t.Fatalf("expected the path on the record, got %v", capture.lastContext["path"])
    }
}

func dispatchResolutionFailureWithSource(t *testing.T, tokenSourceErr error) *resolutionListenerCaptureLogger {
    t.Helper()

    kernel := newTestKernel()
    capture := &resolutionListenerCaptureLogger{}
    runtimeInstance := newResolutionListenerTestRuntimeWithLogger(capture)

    firewall := NewCompiledFirewall(
        "main",
        &resolutionListenerTestMatcher{matches: true},
        "matcher:main",
        []securitycontract.Rule{},
        &resolutionListenerTestTokenSource{
            resolveToken: nil,
            resolveErr:   tokenSourceErr,
        },
        NewAccessControl(
            NewAccessControlRule("/admin", "ROLE_ADMIN"),
        ),
        NewAccessDecisionManager(
            securitycontract.DecisionStrategyAffirmative,
            NewRoleHierarchyVoter(
                NewRoleHierarchy(map[string][]string{}),
                NewRoleVoter(),
            ),
        ),
        NewRoleHierarchy(map[string][]string{}),
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

    registry := NewFirewallRegistry(
        NewCompiledConfiguration([]*CompiledFirewall{firewall}, nil),
    )

    httpPkg.RegisterKernelExceptionListener(kernel.EventDispatcher(), false)
    RegisterKernelSecurityResolutionListener(kernel, registry)

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

    return capture
}

func TestSecurityResolutionListener_AClientTokenRefusalIsRecordedAtWarning(t *testing.T) {
    capture := dispatchResolutionFailureWithSource(t, exception.Unauthorized("token expired"))

    if 1 != capture.warningCalls {
        t.Fatalf("expected the sub-500 refusal recorded once at warning, got %d warnings", capture.warningCalls)
    }

    if 0 != capture.errorCalls {
        t.Fatalf("expected no error record for a client token refusal, got %d", capture.errorCalls)
    }

    if "security token source resolution failed" != capture.lastMessage {
        t.Fatalf("expected the security record to own the refusal, got %q", capture.lastMessage)
    }
}

func TestSecurityResolutionListener_ATokenBackendFailureStaysAtError(t *testing.T) {
    capture := dispatchResolutionFailureWithSource(t, errors.New("token backend down"))

    if 1 != capture.errorCalls {
        t.Fatalf("expected a genuine backend failure recorded once at error, got %d errors", capture.errorCalls)
    }

    if 0 != capture.warningCalls {
        t.Fatalf("expected no warning record for a backend failure, got %d", capture.warningCalls)
    }
}

func TestSecurityResolutionListener_A500ExceptionStaysAtError(t *testing.T) {
    capture := dispatchResolutionFailureWithSource(t, exception.NewHttpException(500, "token store unavailable"))

    if 1 != capture.errorCalls {
        t.Fatalf("expected a 500 refusal recorded once at error, got %d errors", capture.errorCalls)
    }

    if 0 != capture.warningCalls {
        t.Fatalf("expected no warning record for a 500 refusal, got %d", capture.warningCalls)
    }
}

func TestSecurityResolutionListener_MarksTheRecordItWrites(t *testing.T) {
    kernel := newTestKernel()
    capture := &resolutionListenerCaptureLogger{}
    runtimeInstance := newResolutionListenerTestRuntimeWithLogger(capture)

    var observedErr error

    kernel.EventDispatcher().AddListener(
        "kernel.exception",
        func(listenerRuntime runtimecontract.Runtime, eventValue eventcontract.Event) error {
            exceptionEvent, ok := eventValue.Payload().(*httpPkg.KernelExceptionEvent)
            if true == ok && nil != exceptionEvent {
                observedErr = exceptionEvent.Err()
            }

            return nil
        },
        0,
    )

    firewall := NewCompiledFirewall(
        "main",
        &resolutionListenerTestMatcher{matches: true},
        "matcher:main",
        []securitycontract.Rule{},
        &resolutionListenerTestTokenSource{
            resolveToken: nil,
            resolveErr:   errors.New("token backend down"),
        },
        NewAccessControl(
            NewAccessControlRule("/admin", "ROLE_ADMIN"),
        ),
        NewAccessDecisionManager(
            securitycontract.DecisionStrategyAffirmative,
            NewRoleHierarchyVoter(
                NewRoleHierarchy(map[string][]string{}),
                NewRoleVoter(),
            ),
        ),
        NewRoleHierarchy(map[string][]string{}),
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

    registry := NewFirewallRegistry(
        NewCompiledConfiguration([]*CompiledFirewall{firewall}, nil),
    )

    RegisterKernelSecurityResolutionListener(kernel, registry)

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

    if nil == observedErr {
        t.Fatalf("expected the exception event to carry the resolution failure")
    }

    if false == exception.IsAlreadyLogged(observedErr) {
        t.Fatalf("expected the resolution failure to carry the logged mark when it reaches the exception event")
    }
}

/* the twin above hands back an UNWRAPPED nil, which a bare comparison catches just as well — so the guard this listener carries, which reads the interface rather than the word nil, has no test of its own. A token source is the application's code, and a nil pointer of its own token type arrives here as a non-nil interface: read as live, it is published into the security context every voter then reads, and the first Roles() call dereferences it. */
func TestSecurityResolutionListener_WhenTokenSourceReturnsATypedNilToken_SetsAnonymousToken(t *testing.T) {
    kernel := newTestKernel()
    runtimeInstance := newTestRuntime()

    var unassignedToken *AuthenticatedToken

    firewall := NewCompiledFirewall(
        "main",
        &resolutionListenerTestMatcher{matches: true},
        "matcher:main",
        []securitycontract.Rule{},
        &resolutionListenerTestTokenSource{
            resolveToken: unassignedToken,
            resolveErr:   nil,
        },
        nil,
        nil,
        nil,
        nil,
        nil,
        "/admin/login",
        "/admin/logout",
        nil,
        nil,
        SourceNone,
        SourceNone,
        SourceNone,
        SourceNone,
        SourceNone,
    )

    registry := NewFirewallRegistry(
        NewCompiledConfiguration([]*CompiledFirewall{firewall}, nil),
    )

    registerTestKernelExceptionListener(kernel)
    RegisterKernelSecurityResolutionListener(kernel, registry)

    request := newSecurityTestRequest("GET", "/admin", nil, runtimeInstance)
    requestEvent := httpPkg.NewKernelRequestEvent(runtimeInstance, request)

    if _, err := kernel.EventDispatcher().DispatchName(runtimeInstance, "kernel.request", requestEvent); nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    securityContext, exists := SecurityContextFromRuntime(runtimeInstance)
    if false == exists {
        t.Fatalf("expected security context to be set on runtime")
    }

    /* the anonymous token answers Roles without panicking, which the typed nil would not: this is the assertion the bare-nil twin cannot make */
    if 0 != len(securityContext.Token().Roles()) {
        t.Fatalf("expected the anonymous token to carry no roles")
    }

    if true == securityContext.Token().IsAuthenticated() {
        t.Fatalf("expected an anonymous token when the source hands back a typed nil")
    }
}
