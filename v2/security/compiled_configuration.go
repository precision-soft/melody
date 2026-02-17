package security

import (
	"github.com/precision-soft/melody/v2/event"
	"github.com/precision-soft/melody/v2/exception"
	exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
	httpcontract "github.com/precision-soft/melody/v2/http/contract"
	runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
	securitycontract "github.com/precision-soft/melody/v2/security/contract"
)

func NewCompiledFirewall(
	name string,
	matcher securitycontract.Matcher,
	matcherDescription string,
	rules []securitycontract.Rule,
	tokenSource securitycontract.TokenSource,
	accessControl *AccessControl,
	accessDecisionManager securitycontract.AccessDecisionManager,
	roleHierarchy *RoleHierarchy,
	entryPoint securitycontract.EntryPoint,
	accessDeniedHandler securitycontract.AccessDeniedHandler,
	loginPath string,
	logoutPath string,
	loginHandler securitycontract.LoginHandler,
	logoutHandler securitycontract.LogoutHandler,
	roleHierarchySource Source,
	accessDecisionManagerSource Source,
	accessControlSource Source,
	entryPointSource Source,
	accessDeniedHandlerSource Source,
) *CompiledFirewall {
	return &CompiledFirewall{
		name:                        name,
		matcher:                     matcher,
		matcherDescription:          matcherDescription,
		rules:                       rules,
		tokenSource:                 tokenSource,
		accessControl:               accessControl,
		accessDecisionManager:       accessDecisionManager,
		roleHierarchy:               roleHierarchy,
		entryPoint:                  entryPoint,
		accessDeniedHandler:         accessDeniedHandler,
		loginPath:                   loginPath,
		logoutPath:                  logoutPath,
		loginHandler:                loginHandler,
		logoutHandler:               logoutHandler,
		roleHierarchySource:         roleHierarchySource,
		accessDecisionManagerSource: accessDecisionManagerSource,
		accessControlSource:         accessControlSource,
		entryPointSource:            entryPointSource,
		accessDeniedHandlerSource:   accessDeniedHandlerSource,
	}
}

type CompiledFirewall struct {
	name                        string
	matcher                     securitycontract.Matcher
	rules                       []securitycontract.Rule
	tokenSource                 securitycontract.TokenSource
	accessControl               *AccessControl
	accessDecisionManager       securitycontract.AccessDecisionManager
	roleHierarchy               *RoleHierarchy
	entryPoint                  securitycontract.EntryPoint
	accessDeniedHandler         securitycontract.AccessDeniedHandler
	matcherDescription          string
	loginPath                   string
	logoutPath                  string
	loginHandler                securitycontract.LoginHandler
	logoutHandler               securitycontract.LogoutHandler
	roleHierarchySource         Source
	accessDecisionManagerSource Source
	accessControlSource         Source
	entryPointSource            Source
	accessDeniedHandlerSource   Source
}

func (instance *CompiledFirewall) Name() string {
	return instance.name
}

func (instance *CompiledFirewall) Matcher() securitycontract.Matcher {
	return instance.matcher
}

func (instance *CompiledFirewall) MatcherDescription() string {
	return instance.matcherDescription
}

func (instance *CompiledFirewall) Rules() []securitycontract.Rule {
	return append([]securitycontract.Rule{}, instance.rules...)
}

func (instance *CompiledFirewall) TokenSource() securitycontract.TokenSource {
	return instance.tokenSource
}

func (instance *CompiledFirewall) AccessControl() *AccessControl {
	return instance.accessControl
}

func (instance *CompiledFirewall) AccessDecisionManager() securitycontract.AccessDecisionManager {
	return instance.accessDecisionManager
}

func (instance *CompiledFirewall) RoleHierarchy() *RoleHierarchy {
	return instance.roleHierarchy
}

func (instance *CompiledFirewall) EntryPoint() securitycontract.EntryPoint {
	return instance.entryPoint
}

func (instance *CompiledFirewall) AccessDeniedHandler() securitycontract.AccessDeniedHandler {
	return instance.accessDeniedHandler
}

func (instance *CompiledFirewall) LoginPath() string {
	return instance.loginPath
}

func (instance *CompiledFirewall) LogoutPath() string {
	return instance.logoutPath
}

func (instance *CompiledFirewall) Login(
	runtimeInstance runtimecontract.Runtime,
	request httpcontract.Request,
	input securitycontract.LoginInput,
) (*securitycontract.LoginResult, error) {
	if nil == instance.loginHandler {
		return nil, exception.NewError(
			"firewall login handler is nil",
			exceptioncontract.Context{
				"firewallName": instance.name,
			},
			nil,
		)
	}

	result, err := instance.loginHandler.Login(runtimeInstance, request, input)
	if nil != err {
		dispatchErr := instance.dispatchLoginFailure(runtimeInstance, request, err)
		if nil != dispatchErr {
			return nil, dispatchErr
		}

		return nil, err
	}

	dispatchErr := instance.dispatchLoginSuccess(runtimeInstance, request, result.Token)
	if nil != dispatchErr {
		return nil, dispatchErr
	}

	return result, nil
}

func (instance *CompiledFirewall) Logout(
	runtimeInstance runtimecontract.Runtime,
	request httpcontract.Request,
	input securitycontract.LogoutInput,
) (*securitycontract.LogoutResult, error) {
	if nil == instance.logoutHandler {
		return nil, exception.NewError(
			"firewall logout handler is nil",
			exceptioncontract.Context{
				"firewallName": instance.name,
			},
			nil,
		)
	}

	result, err := instance.logoutHandler.Logout(runtimeInstance, request, input)
	if nil != err {
		dispatchErr := instance.dispatchLogoutFailure(runtimeInstance, request, err)
		if nil != dispatchErr {
			return nil, dispatchErr
		}

		return nil, err
	}

	dispatchErr := instance.dispatchLogoutSuccess(runtimeInstance, request)
	if nil != dispatchErr {
		return nil, dispatchErr
	}

	return result, nil
}

func (instance *CompiledFirewall) dispatchLoginSuccess(
	runtimeInstance runtimecontract.Runtime,
	request httpcontract.Request,
	token securitycontract.Token,
) error {
	eventDispatcher := event.EventDispatcherMustFromContainer(runtimeInstance.Container())

	_, err := eventDispatcher.DispatchName(
		runtimeInstance,
		securitycontract.EventSecurityLoginSuccess,
		NewLoginSuccessEvent(request, token),
	)

	return err
}

func (instance *CompiledFirewall) dispatchLoginFailure(
	runtimeInstance runtimecontract.Runtime,
	request httpcontract.Request,
	failureErr error,
) error {
	eventDispatcher := event.EventDispatcherMustFromContainer(runtimeInstance.Container())

	_, err := eventDispatcher.DispatchName(
		runtimeInstance,
		securitycontract.EventSecurityLoginFailure,
		NewLoginFailureEvent(request, failureErr),
	)

	return err
}

func (instance *CompiledFirewall) dispatchLogoutSuccess(
	runtimeInstance runtimecontract.Runtime,
	request httpcontract.Request,
) error {
	eventDispatcher := event.EventDispatcherMustFromContainer(runtimeInstance.Container())

	_, err := eventDispatcher.DispatchName(
		runtimeInstance,
		securitycontract.EventSecurityLogoutSuccess,
		NewLogoutSuccessEvent(request),
	)

	return err
}

func (instance *CompiledFirewall) dispatchLogoutFailure(
	runtimeInstance runtimecontract.Runtime,
	request httpcontract.Request,
	failureErr error,
) error {
	eventDispatcher := event.EventDispatcherMustFromContainer(runtimeInstance.Container())

	_, err := eventDispatcher.DispatchName(
		runtimeInstance,
		securitycontract.EventSecurityLogoutFailure,
		NewLogoutFailureEvent(request, failureErr),
	)

	return err
}

func (instance *CompiledFirewall) Sources() (Source, Source, Source, Source, Source) {
	return instance.roleHierarchySource, instance.accessDecisionManagerSource, instance.accessControlSource, instance.entryPointSource, instance.accessDeniedHandlerSource
}

var _ securitycontract.Firewall = (*CompiledFirewall)(nil)

type CompiledConfiguration struct {
	firewalls []*CompiledFirewall

	globalAccessControl *AccessControl
}

func NewCompiledConfiguration(firewalls []*CompiledFirewall, globalAccessControl *AccessControl) *CompiledConfiguration {
	return &CompiledConfiguration{
		firewalls:           firewalls,
		globalAccessControl: globalAccessControl,
	}
}

func (instance *CompiledConfiguration) Firewalls() []*CompiledFirewall {
	return append([]*CompiledFirewall{}, instance.firewalls...)
}

func (instance *CompiledConfiguration) GlobalAccessControl() *AccessControl {
	return instance.globalAccessControl
}
