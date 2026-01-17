package security

import (
	"errors"
	"testing"

	eventcontract "github.com/precision-soft/melody/event/contract"
	"github.com/precision-soft/melody/http"
	httpPkg "github.com/precision-soft/melody/http"
	httpcontract "github.com/precision-soft/melody/http/contract"
	kernelcontract "github.com/precision-soft/melody/kernel/contract"
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
}

func TestSecurityResolutionListener_WhenFirewallRuleFails_SetsExceptionResponse(t *testing.T) {
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
}

func TestSecurityResolutionListener_WhenTokenSourceErrors_SetsExceptionResponse(t *testing.T) {
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
}

func registerTestKernelExceptionListener(kernelInstance *testKernel) {
	kernelInstance.EventDispatcher().AddListener(
		kernelcontract.EventKernelException,
		func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
			exceptionEvent, ok := eventValue.Payload().(*http.KernelExceptionEvent)
			if false == ok || nil == exceptionEvent {
				return nil
			}

			if nil != exceptionEvent.Response() {
				return nil
			}

			exceptionEvent.SetResponse(
				http.JsonErrorResponse(
					500,
					"internal_server_error",
				),
			)

			return nil
		},
		0,
	)
}
