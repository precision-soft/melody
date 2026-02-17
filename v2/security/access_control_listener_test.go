package security

import (
	"errors"
	"testing"

	eventcontract "github.com/precision-soft/melody/v2/event/contract"
	httpPkg "github.com/precision-soft/melody/v2/http"
	httpcontract "github.com/precision-soft/melody/v2/http/contract"
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
	err   error
	calls int
}

func (instance *accessControlListenerTestAccessDeniedHandler) Handle(runtimeInstance any, request httpcontract.Request, decisionErr error) (httpcontract.Response, error) {
	instance.calls++
	return nil, instance.err
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
