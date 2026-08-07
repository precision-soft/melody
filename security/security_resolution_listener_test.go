package security

import (
    "context"
    "errors"
    "testing"

    "github.com/precision-soft/melody/container"
    eventcontract "github.com/precision-soft/melody/event/contract"
    "github.com/precision-soft/melody/exception"
    httpPkg "github.com/precision-soft/melody/http"
    httpcontract "github.com/precision-soft/melody/http/contract"
    "github.com/precision-soft/melody/logging"
    loggingcontract "github.com/precision-soft/melody/logging/contract"
    "github.com/precision-soft/melody/runtime"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
    securitycontract "github.com/precision-soft/melody/security/contract"
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

/* a nil token source panics on Resolve and the recovery handler reads the source name; reading it off the nil source panics a second time after recover and the diagnostic escapes unrecovered, so the recovery must read the name defensively and still fail closed with an anonymous context */
func TestSecurityResolutionListener_WhenTokenSourceIsNil_RecoversWithoutSecondPanic(t *testing.T) {
    kernel := newTestKernel()
    runtimeInstance := newTestRuntime()

    firewall := NewCompiledFirewall(
        "main",
        &resolutionListenerTestMatcher{matches: true},
        "matcher:main",
        []securitycontract.Rule{},
        nil,
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
    if false == exists || nil == securityContext {
        t.Fatalf("expected security context to be set even when the token source is nil")
    }
    if true == securityContext.Token().IsAuthenticated() {
        t.Fatalf("expected anonymous token when the token source is nil")
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

/* The token source is the application's, so a nil pointer of its own token type reaches the listener as a
non-nil interface: taken for a live token it is published into the security context, and the first voter to
call Roles on it panics on the request path. */
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

    if _, isAnonymous := securityContext.Token().(*AnonymousToken); false == isAnonymous {
        t.Fatalf("expected the anonymous token, got %T", securityContext.Token())
    }
}

type resolutionListenerCaptureLogger struct {
    errorCalls  int
    lastMessage string
    lastContext map[string]any
}

func (instance *resolutionListenerCaptureLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
    if loggingcontract.LevelError == level {
        instance.errorCalls++
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

    if nil == requestEvent.Response() {
        t.Fatalf("expected the refusal response to be set")
    }

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
