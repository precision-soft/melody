package security

import (
    "context"
    "errors"
    nethttp "net/http"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    eventcontract "github.com/precision-soft/melody/v4/event/contract"
    "github.com/precision-soft/melody/v4/exception"
    httpPkg "github.com/precision-soft/melody/v4/http"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    "github.com/precision-soft/melody/v4/security/accesscontrol"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

type resolutionListenerTestTokenSource struct {
    resolveToken securitycontract.Token
    resolveErr   error
}

func (instance *resolutionListenerTestTokenSource) Name() string { return "test" }

func (instance *resolutionListenerTestTokenSource) Stateless() bool { return false }

func (instance *resolutionListenerTestTokenSource) Resolve(runtimeInstance containercontract.Runtime, request httpcontract.Request) (securitycontract.Token, error) {
    return instance.resolveToken, instance.resolveErr
}

type resolutionListenerTestPanickingTokenSource struct{}

func (instance *resolutionListenerTestPanickingTokenSource) Name() string { return "panicking" }

func (instance *resolutionListenerTestPanickingTokenSource) Stateless() bool { return false }

func (instance *resolutionListenerTestPanickingTokenSource) Resolve(runtimeInstance containercontract.Runtime, request httpcontract.Request) (securitycontract.Token, error) {
    panic("database connection failed")
}

type resolutionListenerTestMatcher struct {
    matches bool
}

func (instance *resolutionListenerTestMatcher) Matches(request httpcontract.Request) bool {
    return instance.matches
}

type resolutionListenerTestRule struct {
    err      error
    runtimes []containercontract.Runtime
}

func (instance *resolutionListenerTestRule) Applies(request httpcontract.Request) bool { return true }

func (instance *resolutionListenerTestRule) Check(runtimeInstance containercontract.Runtime, request httpcontract.Request) error {
    instance.runtimes = append(instance.runtimes, runtimeInstance)

    return instance.err
}

func TestSecurityResolutionListener_SetsSecurityContextOnRuntime_OnSuccess(t *testing.T) {
    kernel := newTestKernel()
    runtimeInstance := newTestRuntime()

    token := NewAuthenticatedToken("user", []string{"ROLE_USER"}, nil)

    firewall := NewCompiledFirewall(CompiledFirewallConfig{
        Name:               "main",
        Matcher:            &resolutionListenerTestMatcher{matches: true},
        MatcherDescription: "matcher:main",
        TokenSource: &resolutionListenerTestTokenSource{
            resolveToken: token,
            resolveErr:   nil,
        },
        AccessControl: NewAccessControl([]AccessControlRule{
            accesscontrol.NewRawPrefixRule("/admin", accesscontrol.RuleConfig{Attributes: []string{"ROLE_ADMIN"}}),
        }),
        AccessDecisionManager: NewAccessDecisionManager(
            securitycontract.DecisionStrategyAffirmative,
            []securitycontract.Voter{NewRoleHierarchyVoter(
                NewRoleHierarchy(map[string][]string{}),
                NewRoleVoter(),
            ),
            }, AccessDecisionManagerConfig{}),
        RoleHierarchy:               NewRoleHierarchy(map[string][]string{}),
        LoginPath:                   "/admin/login",
        LogoutPath:                  "/admin/logout",
        RoleHierarchySource:         SourceFirewall,
        AccessDecisionManagerSource: SourceFirewall,
        AccessControlSource:         SourceFirewall,
    })

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

    firewall := NewCompiledFirewall(CompiledFirewallConfig{
        Name:               "main",
        Matcher:            &resolutionListenerTestMatcher{matches: true},
        MatcherDescription: "matcher:main",
        Rules: []securitycontract.Rule{
            &resolutionListenerTestRule{err: errors.New("denied")},
        },
        TokenSource: &resolutionListenerTestTokenSource{
            resolveToken: NewAnonymousToken(),
            resolveErr:   nil,
        },
        LoginPath:  "/admin/login",
        LogoutPath: "/admin/logout",
    })

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

    if nil == requestEvent.Refusal() {
        t.Fatalf("expected a refusal on the request event")
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

    firewall := NewCompiledFirewall(CompiledFirewallConfig{
        Name:               "main",
        Matcher:            &resolutionListenerTestMatcher{matches: true},
        MatcherDescription: "matcher:main",
        TokenSource: &resolutionListenerTestTokenSource{
            resolveToken: nil,
            resolveErr:   errors.New("token error"),
        },
        LoginPath:  "/admin/login",
        LogoutPath: "/admin/logout",
    })

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

    if nil == requestEvent.Refusal() {
        t.Fatalf("expected a refusal on the request event")
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

    firewall := NewCompiledFirewall(CompiledFirewallConfig{
        Name:               "main",
        Matcher:            &resolutionListenerTestMatcher{matches: true},
        MatcherDescription: "matcher:main",
        TokenSource:        &resolutionListenerTestPanickingTokenSource{},
        LoginPath:          "/admin/login",
        LogoutPath:         "/admin/logout",
    })

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

    if nil == requestEvent.Refusal() {
        t.Fatalf("expected a refusal on the request event")
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

    firewall := NewCompiledFirewall(CompiledFirewallConfig{
        Name:               "main",
        Matcher:            &resolutionListenerTestMatcher{matches: true},
        MatcherDescription: "matcher:main",
        TokenSource: &resolutionListenerTestTokenSource{
            resolveToken: nil,
            resolveErr:   nil,
        },
        LoginPath:  "/admin/login",
        LogoutPath: "/admin/logout",
    })

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

func (instance *resolutionListenerCaptureLogger) Enabled(level loggingcontract.Level) bool {
    return true
}

func (instance *resolutionListenerCaptureLogger) CauseChainDepth() int {
    return 8
}

var _ loggingcontract.Logger = (*resolutionListenerCaptureLogger)(nil)

func newResolutionListenerTestRuntimeWithLogger(logger loggingcontract.Logger) containercontract.Runtime {
    scope := newTestScope()

    overrideErr := scope.OverrideInstance(logging.ServiceLogger, logger, container.OverrideOptions{})
    if nil != overrideErr {
        panic(overrideErr)
    }

    return runtime.NewRequest(context.Background(), scope)
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

    return dispatchResolutionFailureWithSourceAtPath(t, tokenSourceErr, "/admin")
}

func dispatchResolutionFailureWithSourceAtPath(t *testing.T, tokenSourceErr error, path string) *resolutionListenerCaptureLogger {
    t.Helper()

    kernel := newTestKernel()
    capture := &resolutionListenerCaptureLogger{}
    runtimeInstance := newResolutionListenerTestRuntimeWithLogger(capture)

    firewall := NewCompiledFirewall(CompiledFirewallConfig{
        Name:               "main",
        Matcher:            &resolutionListenerTestMatcher{matches: true},
        MatcherDescription: "matcher:main",
        TokenSource: &resolutionListenerTestTokenSource{
            resolveToken: nil,
            resolveErr:   tokenSourceErr,
        },
        AccessControl: NewAccessControl([]AccessControlRule{
            accesscontrol.NewRawPrefixRule("/admin", accesscontrol.RuleConfig{Attributes: []string{"ROLE_ADMIN"}}),
        }),
        AccessDecisionManager: NewAccessDecisionManager(
            securitycontract.DecisionStrategyAffirmative,
            []securitycontract.Voter{NewRoleHierarchyVoter(
                NewRoleHierarchy(map[string][]string{}),
                NewRoleVoter(),
            ),
            }, AccessDecisionManagerConfig{}),
        RoleHierarchy:               NewRoleHierarchy(map[string][]string{}),
        LoginPath:                   "/admin/login",
        LogoutPath:                  "/admin/logout",
        RoleHierarchySource:         SourceFirewall,
        AccessDecisionManagerSource: SourceFirewall,
        AccessControlSource:         SourceFirewall,
    })

    registry := NewFirewallRegistry(
        NewCompiledConfiguration([]*CompiledFirewall{firewall}, nil),
    )

    RegisterKernelSecurityResolutionListener(kernel, registry)

    request := newSecurityTestRequest("GET", path, nil, runtimeInstance)
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

func TestSecurityResolutionListener_TheResolutionFailureRecordBoundsTheMethodAndThePath(t *testing.T) {
    capture := dispatchResolutionFailureWithSourceAtPath(t, errors.New("token backend down"), "/admin/"+strings.Repeat("a", 600*1024))

    path, _ := capture.lastContext["path"].(string)
    if 600 < len(path) || false == strings.Contains(path, "...(truncated ") {
        t.Fatalf("expected the record's path bounded, got %d bytes", len(path))
    }
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

    firewall := NewCompiledFirewall(CompiledFirewallConfig{
        Name:               "main",
        Matcher:            &resolutionListenerTestMatcher{matches: true},
        MatcherDescription: "matcher:main",
        TokenSource: &resolutionListenerTestTokenSource{
            resolveToken: nil,
            resolveErr:   errors.New("token backend down"),
        },
        AccessControl: NewAccessControl([]AccessControlRule{
            accesscontrol.NewRawPrefixRule("/admin", accesscontrol.RuleConfig{Attributes: []string{"ROLE_ADMIN"}}),
        }),
        AccessDecisionManager: NewAccessDecisionManager(
            securitycontract.DecisionStrategyAffirmative,
            []securitycontract.Voter{NewRoleHierarchyVoter(
                NewRoleHierarchy(map[string][]string{}),
                NewRoleVoter(),
            ),
            }, AccessDecisionManagerConfig{}),
        RoleHierarchy:               NewRoleHierarchy(map[string][]string{}),
        LoginPath:                   "/admin/login",
        LogoutPath:                  "/admin/logout",
        RoleHierarchySource:         SourceFirewall,
        AccessDecisionManagerSource: SourceFirewall,
        AccessControlSource:         SourceFirewall,
    })

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

    if nil == requestEvent.Refusal() {
        t.Fatalf("expected the refusal to carry the resolution failure")
    }

    if false == exception.IsAlreadyLogged(requestEvent.Refusal()) {
        t.Fatalf("expected the resolution failure to carry the logged mark when it reaches the refusal")
    }
}

/* the twin above hands back an UNWRAPPED nil, which a bare comparison catches just as well — so the guard this listener carries, which reads the interface rather than the word nil, has no test of its own. A token source is the application's code, and a nil pointer of its own token type arrives here as a non-nil interface: read as live, it is published into the security context every voter then reads, and the first Roles() call dereferences it. */
func TestSecurityResolutionListener_WhenTokenSourceReturnsATypedNilToken_SetsAnonymousToken(t *testing.T) {
    kernel := newTestKernel()
    runtimeInstance := newTestRuntime()

    var unassignedToken *AuthenticatedToken

    firewall := NewCompiledFirewall(CompiledFirewallConfig{
        Name:               "main",
        Matcher:            &resolutionListenerTestMatcher{matches: true},
        MatcherDescription: "matcher:main",
        TokenSource: &resolutionListenerTestTokenSource{
            resolveToken: unassignedToken,
            resolveErr:   nil,
        },
        LoginPath:  "/admin/login",
        LogoutPath: "/admin/logout",
    })

    registry := NewFirewallRegistry(
        NewCompiledConfiguration([]*CompiledFirewall{firewall}, nil),
    )

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

/* "GET http://host" reaches the kernel with an empty path: the firewall on "/" resolves it and its token source runs, where the path read as nothing selected no firewall and the request went through with no security context */
func TestSecurityResolutionListener_AnAbsoluteFormTargetIsResolvedByTheRootFirewall(t *testing.T) {
    kernel := newTestKernel()
    runtimeInstance := newTestRuntime()

    token := NewAuthenticatedToken("user", []string{"ROLE_USER"}, nil)

    firewall := NewCompiledFirewall(CompiledFirewallConfig{
        Name:               "main",
        Matcher:            NewPathPrefixMatcher("/"),
        MatcherDescription: "matcher:main",
        TokenSource: &resolutionListenerTestTokenSource{
            resolveToken: token,
            resolveErr:   nil,
        },
        AccessControl: NewAccessControl([]AccessControlRule{
            accesscontrol.NewRawPrefixRule("/admin", accesscontrol.RuleConfig{Attributes: []string{"ROLE_ADMIN"}}),
        }),
        AccessDecisionManager: NewAccessDecisionManager(
            securitycontract.DecisionStrategyAffirmative,
            []securitycontract.Voter{NewRoleHierarchyVoter(
                NewRoleHierarchy(map[string][]string{}),
                NewRoleVoter(),
            ),
            }, AccessDecisionManagerConfig{}),
        RoleHierarchy:               NewRoleHierarchy(map[string][]string{}),
        LoginPath:                   "/admin/login",
        LogoutPath:                  "/admin/logout",
        RoleHierarchySource:         SourceFirewall,
        AccessDecisionManagerSource: SourceFirewall,
        AccessControlSource:         SourceFirewall,
    })

    registry := NewFirewallRegistry(
        NewCompiledConfiguration([]*CompiledFirewall{firewall}, nil),
    )

    RegisterKernelSecurityResolutionListener(kernel, registry)

    request := newSecurityTestRequest("GET", "", nil, runtimeInstance)
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

func TestSecurityResolutionListener_AnAsteriskFormTargetIsResolvedByTheRootFirewall(t *testing.T) {
    for _, path := range []string{"*", "/x", ""} {
        kernel := newTestKernel()
        runtimeInstance := newTestRuntime()

        token := NewAuthenticatedToken("user", []string{"ROLE_USER"}, nil)

        firewall := NewCompiledFirewall(CompiledFirewallConfig{
            Name:               "main",
            Matcher:            NewPathPrefixMatcher("/"),
            MatcherDescription: "matcher:main",
            TokenSource: &resolutionListenerTestTokenSource{
                resolveToken: token,
                resolveErr:   nil,
            },
            AccessControl: NewAccessControl([]AccessControlRule{
                accesscontrol.NewRawPrefixRule("/admin", accesscontrol.RuleConfig{Attributes: []string{"ROLE_ADMIN"}}),
            }),
            AccessDecisionManager: NewAccessDecisionManager(
                securitycontract.DecisionStrategyAffirmative,
                []securitycontract.Voter{NewRoleHierarchyVoter(
                    NewRoleHierarchy(map[string][]string{}),
                    NewRoleVoter(),
                ),
                }, AccessDecisionManagerConfig{}),
            RoleHierarchy:               NewRoleHierarchy(map[string][]string{}),
            LoginPath:                   "/admin/login",
            LogoutPath:                  "/admin/logout",
            RoleHierarchySource:         SourceFirewall,
            AccessDecisionManagerSource: SourceFirewall,
            AccessControlSource:         SourceFirewall,
        })

        registry := NewFirewallRegistry(
            NewCompiledConfiguration([]*CompiledFirewall{firewall}, nil),
        )

            RegisterKernelSecurityResolutionListener(kernel, registry)

        request := newSecurityTestRequest("GET", "", nil, runtimeInstance)
        request.HttpRequest().URL.Path = path
        requestEvent := httpPkg.NewKernelRequestEvent(runtimeInstance, request)

        _, err := kernel.EventDispatcher().DispatchName(
            runtimeInstance,
            "kernel.request",
            requestEvent,
        )
        if nil != err {
            t.Fatalf("unexpected error for the path %q: %v", path, err)
        }

        securityContext, exists := SecurityContextFromRuntime(runtimeInstance)
        if false == exists || nil == securityContext {
            t.Fatalf("expected a security context for the path %q", path)
        }
        if nil == securityContext.Firewall() || "main" != securityContext.Firewall().Name() {
            t.Fatalf("expected the root firewall to resolve the path %q", path)
        }
        if false == securityContext.Token().IsAuthenticated() {
            t.Fatalf("expected the root firewall's token source to run for the path %q", path)
        }
    }
}

type resolutionListenerTestMarkedPanickingTokenSource struct{}

func (instance *resolutionListenerTestMarkedPanickingTokenSource) Name() string { return "marked-panicking" }

func (instance *resolutionListenerTestMarkedPanickingTokenSource) Stateless() bool { return false }

func (instance *resolutionListenerTestMarkedPanickingTokenSource) Resolve(runtimeInstance containercontract.Runtime, request httpcontract.Request) (securitycontract.Token, error) {
    panic(exception.MarkInfrastructureFailure(errors.New("redis is down")))
}

func dispatchResolutionThrough(t *testing.T, tokenSource securitycontract.TokenSource) (*httpPkg.KernelRequestEvent, securitycontract.Token, *recordingLogger) {
    t.Helper()

    kernel := newTestKernel()
    runtimeInstance, logger := runtimeWithRecordingLogger()

    firewall := NewCompiledFirewall(CompiledFirewallConfig{
        Name:               "main",
        Matcher:            &resolutionListenerTestMatcher{matches: true},
        MatcherDescription: "matcher:main",
        TokenSource:        tokenSource,
    })
    RegisterKernelSecurityResolutionListener(kernel, NewFirewallRegistry(NewCompiledConfiguration([]*CompiledFirewall{firewall}, nil)))

    requestEvent := httpPkg.NewKernelRequestEvent(runtimeInstance, newSecurityTestRequest("GET", "/admin", nil, runtimeInstance))
    if _, dispatchErr := kernel.EventDispatcher().DispatchName(runtimeInstance, "kernel.request", requestEvent); nil != dispatchErr {
        t.Fatalf("unexpected error: %v", dispatchErr)
    }

    securityContext, exists := SecurityContextFromRuntime(runtimeInstance)
    if false == exists || nil == securityContext {
        t.Fatal("expected a security context on the runtime")
    }

    return requestEvent, securityContext.Token(), logger
}

func TestSecurityResolutionListener_AnUnavailableTokenIsPublishedAndRecordedOnceAtError(t *testing.T) {
    token := NewUnavailableToken(exception.MarkInfrastructureFailure(errors.New("redis is down")))

    requestEvent, published, logger := dispatchResolutionThrough(t, &resolutionListenerTestTokenSource{resolveToken: token})

    if nil != requestEvent.Refusal() || securitycontract.Token(token) != published {
        t.Fatalf("expected the token published and no refusal, got %T, %v", published, requestEvent.Refusal())
    }

    records := logger.recordsAtLevel(loggingcontract.LevelError)
    if 1 != len(records) || 0 != len(logger.recordsAtLevel(loggingcontract.LevelWarning)) {
        t.Fatalf("expected one Error record and no Warning, got %v", logger.records)
    }

    context := records[0].context
    if "main" != context["firewallName"] || "test" != context["tokenSourceName"] || "GET" != context["method"] || "/admin" != context["path"] || "redis is down" != context["error"] {
        t.Fatalf("expected the record to name the firewall, the source, the method, the path and the cause, got %v", context)
    }

    _, anonymous, anonymousLogger := dispatchResolutionThrough(t, &resolutionListenerTestTokenSource{resolveToken: NewAnonymousToken()})
    expectAnonymousToken(t, anonymous)
    if 0 != len(anonymousLogger.recordsAtLevel(loggingcontract.LevelError)) {
        t.Fatalf("expected no record for an anonymous token, got %v", anonymousLogger.records)
    }
}

func TestSecurityResolutionListener_AMarkedResolveErrorIsReadAsUnavailable(t *testing.T) {
    storeErr := errors.New("redis is down")

    requestEvent, published, logger := dispatchResolutionThrough(t, &resolutionListenerTestTokenSource{resolveErr: exception.MarkInfrastructureFailure(storeErr)})

    if nil != requestEvent.Refusal() {
        t.Fatalf("expected no refusal for a marked error, got %v", requestEvent.Refusal())
    }

    if cause := expectUnavailableToken(t, published); false == errors.Is(cause, storeErr) {
        t.Fatalf("expected the source's error as the cause, got %v", cause)
    }

    if 1 != len(logger.recordsAtLevel(loggingcontract.LevelError)) {
        t.Fatalf("expected one Error record, got %v", logger.records)
    }

    _, besideAuthenticated, _ := dispatchResolutionThrough(t, &resolutionListenerTestTokenSource{
        resolveToken: NewAuthenticatedToken("admin", []string{"ROLE_ADMIN"}, nil),
        resolveErr:   exception.MarkInfrastructureFailure(storeErr),
    })
    expectUnavailableToken(t, besideAuthenticated)

    unmarkedEvent, unmarked, _ := dispatchResolutionThrough(t, &resolutionListenerTestTokenSource{resolveErr: storeErr})
    if nil == unmarkedEvent.Refusal() {
        t.Fatal("expected an unmarked error refused")
    }
    expectAnonymousToken(t, unmarked)

    panickedEvent, panicked, _ := dispatchResolutionThrough(t, &resolutionListenerTestMarkedPanickingTokenSource{})
    if nil == panickedEvent.Refusal() {
        t.Fatal("expected a source panicking with a marked error refused, a panic is never the unavailable token")
    }
    expectAnonymousToken(t, panicked)
}

func TestSecurityResolutionListener_AMarkedResolveErrorThatAnswersAStatusKeepsIt(t *testing.T) {
    for name, resolveErr := range map[string]error{
        "a marked cause under a 400":  exception.NewHttpError(nethttp.StatusBadRequest, exception.HttpErrorConfig{Message: "malformed", Cause: exception.MarkInfrastructureFailure(errors.New("redis is down"))}),
        "a 429 around a marked cause": exception.NewHttpError(nethttp.StatusTooManyRequests, exception.HttpErrorConfig{Message: "too many", Cause: exception.MarkInfrastructureFailure(errors.New("redis is down"))}),
    } {
        t.Run(name, func(t *testing.T) {
            requestEvent, published, _ := dispatchResolutionThrough(t, &resolutionListenerTestTokenSource{resolveErr: resolveErr})

            refusal := exception.AsHttpException(requestEvent.Refusal())
            if nil == refusal || exception.AsHttpException(resolveErr).StatusCode() != refusal.StatusCode() {
                t.Fatalf("expected the error's own status refused, got %v", requestEvent.Refusal())
            }

            expectAnonymousToken(t, published)
        })
    }
}

func TestSecurityResolutionListener_AStoresStatusUnderTheMarkIsReadAsUnavailable(t *testing.T) {
    requestEvent, published, _ := dispatchResolutionThrough(t, &resolutionListenerTestTokenSource{
        resolveErr: exception.MarkInfrastructureFailure(exception.NewHttpException(nethttp.StatusNotFound, "remote said no such user")),
    })

    if nil != requestEvent.Refusal() {
        t.Fatalf("expected no refusal for a status the mark wraps, got %v", requestEvent.Refusal())
    }

    expectUnavailableToken(t, published)
}

func TestSecurityResolutionListener_AnUnavailableCauseItsProducerFiledIsNotFiledAgain(t *testing.T) {
    filed := exception.NewError("redis is down", nil, nil)
    _ = exception.MarkLogged(filed)

    _, published, logger := dispatchResolutionThrough(t, &resolutionListenerTestTokenSource{resolveToken: NewUnavailableToken(exception.MarkInfrastructureFailure(filed))})

    expectUnavailableToken(t, published)
    if 0 != len(logger.recordsAtLevel(loggingcontract.LevelError)) {
        t.Fatalf("expected no second record for a cause already filed, got %v", logger.records)
    }
}

func TestSecurityResolutionListener_HandsItsRuntimeToTheFirewallRules(t *testing.T) {
    kernel := newTestKernel()
    runtimeInstance := newTestRuntime()

    rule := &resolutionListenerTestRule{}
    firewall := NewCompiledFirewall(CompiledFirewallConfig{
        Name:               "main",
        Matcher:            &resolutionListenerTestMatcher{matches: true},
        MatcherDescription: "matcher:main",
        Rules:              []securitycontract.Rule{rule},
        TokenSource:        &resolutionListenerTestTokenSource{resolveToken: NewAuthenticatedToken("user", []string{"ROLE_USER"}, nil)},
        LoginPath:          "/admin/login",
        LogoutPath:         "/admin/logout",
    })

    RegisterKernelSecurityResolutionListener(kernel, NewFirewallRegistry(NewCompiledConfiguration([]*CompiledFirewall{firewall}, nil)))

    request := newSecurityTestRequest("GET", "/admin", nil, newTestRuntime())
    if _, err := kernel.EventDispatcher().DispatchName(runtimeInstance, "kernel.request", httpPkg.NewKernelRequestEvent(runtimeInstance, request)); nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if 1 != len(rule.runtimes) || runtimeInstance != rule.runtimes[0] {
        t.Fatalf("expected the rule handed the listener's runtime, not the request's, got %v", rule.runtimes)
    }
}

func forgedRouteFixture(t *testing.T, accessControl *AccessControl, decisionManager *countingDecisionManager) (*csrfFixture, *int) {
    t.Helper()

    fixture := newCsrfFixture(t, func(config *CompiledFirewallConfig) {
        config.AccessControl = accessControl
        config.AccessDecisionManager = decisionManager
    })
    RegisterKernelAccessControlListener(fixture.kernel, fixture.registry)

    denied := 0
    fixture.kernel.eventDispatcher.AddListener(securitycontract.EventSecurityAuthorizationDenied, func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
        denied++

        return nil
    }, eventcontract.ListenerConfig{})

    return fixture, &denied
}

func TestSecurityResolutionListener_ForgedRoute_RefusedBeforeAccessControl(t *testing.T) {
    decisionManager := &countingDecisionManager{}
    fixture, denied := forgedRouteFixture(t, NewAccessControl([]AccessControlRule{accesscontrol.NewRawPrefixRule(csrfAccountPath, accesscontrol.RuleConfig{Attributes: []string{"ROLE_ADMIN"}})}), decisionManager)
    sessionId, _ := fixture.storedSession(t, csrfSignedIn)

    recorder := fixture.serve(t, csrfProbe{sessionId: sessionId, headers: map[string]string{"Sec-Fetch-Site": "cross-site"}})

    if nethttp.StatusForbidden != recorder.Code || 0 != *denied || 0 != decisionManager.calls {
        t.Fatalf("expected the forgery refused before any decision, got %d, %d denied, %d decisions", recorder.Code, *denied, decisionManager.calls)
    }
}

func TestSecurityResolutionListener_ForgedPublicRoute_Refused(t *testing.T) {
    fixture, _ := forgedRouteFixture(t, NewAccessControl([]AccessControlRule{accesscontrol.NewSegmentPrefixRule(csrfAccountPath, accesscontrol.RuleConfig{Attributes: []string{securitycontract.AttributePublicAccess}})}), &countingDecisionManager{})
    sessionId, _ := fixture.storedSession(t, csrfSignedIn)

    if recorder := fixture.serve(t, csrfProbe{sessionId: sessionId}); nethttp.StatusForbidden != recorder.Code || 0 != *fixture.served {
        t.Fatalf("expected a public route checked too, got %d", recorder.Code)
    }
}

func TestSecurityResolutionListener_ForgedRouteNoRuleMatches_Refused(t *testing.T) {
    fixture, _ := forgedRouteFixture(t, NewAccessControl([]AccessControlRule{accesscontrol.NewRawPrefixRule("/admin", accesscontrol.RuleConfig{Attributes: []string{"ROLE_ADMIN"}})}), &countingDecisionManager{})
    sessionId, _ := fixture.storedSession(t, csrfSignedIn)

    if recorder := fixture.serve(t, csrfProbe{sessionId: sessionId}); nethttp.StatusForbidden != recorder.Code || 0 != *fixture.served {
        t.Fatalf("expected a route no rule matches checked too, got %d", recorder.Code)
    }
}

func TestSecurityResolutionListener_DoorPathOnAMethodTheDoorDeclines_CheckedAsARoute(t *testing.T) {
    fixture := newCsrfFixture(t, nil)
    sessionId, _ := fixture.storedSession(t, csrfSignedIn)

    if recorder := fixture.serve(t, csrfProbe{method: nethttp.MethodPut, path: doorLoginPath, sessionId: sessionId}); nethttp.StatusForbidden != recorder.Code {
        t.Fatalf("expected a PUT on the login path checked as a route, got %d", recorder.Code)
    }
}

func TestSecurityResolutionListener_RuleCheckRefusal_ComesFirst(t *testing.T) {
    ruleErr := exception.NewHttpError(nethttp.StatusTooManyRequests, exception.HttpErrorConfig{Message: "rule refused"})
    fixture := newCsrfFixture(t, func(config *CompiledFirewallConfig) {
        config.Rules = []securitycontract.Rule{&resolutionListenerTestRule{err: ruleErr}}
    })
    sessionId, _ := fixture.storedSession(t, csrfSignedIn)

    if recorder := fixture.serve(t, csrfProbe{sessionId: sessionId, headers: map[string]string{"Sec-Fetch-Site": "cross-site"}}); nethttp.StatusTooManyRequests != recorder.Code {
        t.Fatalf("expected the rule's refusal first, got %d", recorder.Code)
    }
}
