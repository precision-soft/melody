package config

import (
	"github.com/precision-soft/melody/exception"
	exceptioncontract "github.com/precision-soft/melody/exception/contract"
	"github.com/precision-soft/melody/security"
	securitycontract "github.com/precision-soft/melody/security/contract"
)

type AccessControlMergeStrategy string

const (
	AccessControlMergeLocalFirst   AccessControlMergeStrategy = "localFirst"
	AccessControlMergeGlobalFirst  AccessControlMergeStrategy = "globalFirst"
	AccessControlMergeOverrideOnly AccessControlMergeStrategy = "overrideOnly"
)

type GlobalConfiguration struct {
	accessControl         *security.AccessControl
	roleHierarchy         *security.RoleHierarchy
	accessDecisionManager securitycontract.AccessDecisionManager
	entryPoint            securitycontract.EntryPoint
	accessDeniedHandler   securitycontract.AccessDeniedHandler
}

type FirewallOverrideConfiguration struct {
	inheritGlobalAccessControl bool
	mergeStrategy              AccessControlMergeStrategy
	accessControl              *security.AccessControl
	roleHierarchy              *security.RoleHierarchy
	accessDecisionManager      securitycontract.AccessDecisionManager
	entryPoint                 securitycontract.EntryPoint
	accessDeniedHandler        securitycontract.AccessDeniedHandler
}

type FirewallConfiguration struct {
	name          string
	matcher       securitycontract.Matcher
	rules         []securitycontract.Rule
	tokenSource   securitycontract.TokenSource
	loginPath     string
	logoutPath    string
	loginHandler  securitycontract.LoginHandler
	logoutHandler securitycontract.LogoutHandler
	override      FirewallOverrideConfiguration
}

type Configuration struct {
	global    GlobalConfiguration
	firewalls []FirewallConfiguration
}

type Builder struct {
	globalConfigured bool
	global           GlobalConfiguration
	firewalls        []FirewallConfiguration
}

func NewBuilder() *Builder {
	return &Builder{
		firewalls: make([]FirewallConfiguration, 0),
	}
}

func (instance *Builder) SetGlobal(
	accessControl *security.AccessControl,
	roleHierarchy *security.RoleHierarchy,
	accessDecisionManager securitycontract.AccessDecisionManager,
	entryPoint securitycontract.EntryPoint,
	accessDeniedHandler securitycontract.AccessDeniedHandler,
) *Builder {
	if true == instance.globalConfigured {
		exception.Panic(exception.NewError("security global configuration may only be defined once", nil, nil))
	}

	instance.globalConfigured = true
	instance.global.accessControl = accessControl
	instance.global.roleHierarchy = roleHierarchy
	instance.global.accessDecisionManager = accessDecisionManager
	instance.global.entryPoint = entryPoint
	instance.global.accessDeniedHandler = accessDeniedHandler

	return instance
}

func (instance *Builder) AddFirewall(
	name string,
	matcher securitycontract.Matcher,
	rules []securitycontract.Rule,
	tokenSource securitycontract.TokenSource,
	loginPath string,
	logoutPath string,
	loginHandler securitycontract.LoginHandler,
	logoutHandler securitycontract.LogoutHandler,
	override FirewallOverrideConfiguration,
) *Builder {
	if "" == name {
		exception.Panic(exception.NewError("security firewall name may not be empty", nil, nil))
	}

	if nil == matcher {
		exception.Panic(
			exception.NewError(
				"security firewall matcher is nil",
				exceptioncontract.Context{
					"firewallName": name,
				},
				nil,
			),
		)
	}

	if nil == tokenSource {
		exception.Panic(
			exception.NewError(
				"security firewall token source is nil",
				exceptioncontract.Context{
					"firewallName": name,
				},
				nil,
			),
		)
	}

	if "" == loginPath {
		exception.Panic(
			exception.NewError(
				"security firewall login path may not be empty",
				exceptioncontract.Context{
					"firewallName": name,
				},
				nil,
			),
		)
	}

	if "" == logoutPath {
		exception.Panic(
			exception.NewError(
				"security firewall logout path may not be empty",
				exceptioncontract.Context{
					"firewallName": name,
				},
				nil,
			),
		)
	}

	if nil == loginHandler {
		exception.Panic(
			exception.NewError(
				"security firewall login handler is nil",
				exceptioncontract.Context{
					"firewallName": name,
				},
				nil,
			),
		)
	}

	if nil == logoutHandler {
		exception.Panic(
			exception.NewError(
				"security firewall logout handler is nil",
				exceptioncontract.Context{
					"firewallName": name,
				},
				nil,
			),
		)
	}

	if "" == string(override.mergeStrategy) {
		override.mergeStrategy = AccessControlMergeLocalFirst
	}

	instance.firewalls = append(
		instance.firewalls,
		FirewallConfiguration{
			name:          name,
			matcher:       matcher,
			rules:         rules,
			tokenSource:   tokenSource,
			loginPath:     loginPath,
			logoutPath:    logoutPath,
			loginHandler:  loginHandler,
			logoutHandler: logoutHandler,
			override:      override,
		},
	)

	return instance
}

func (instance *Builder) BuildAndCompile() *security.CompiledConfiguration {
	if 0 == len(instance.firewalls) {
		return nil
	}

	compiled, err := Compile(
		Configuration{
			global:    instance.global,
			firewalls: instance.firewalls,
		},
	)
	if nil != err {
		exception.Panic(exception.FromError(err))
	}

	return compiled
}

func NewFirewallOverrideConfiguration() FirewallOverrideConfiguration {
	return FirewallOverrideConfiguration{
		inheritGlobalAccessControl: true,
		mergeStrategy:              AccessControlMergeLocalFirst,
	}
}
