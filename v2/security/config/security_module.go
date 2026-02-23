package config

import (
    "github.com/precision-soft/melody/v2/exception"
    exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
    "github.com/precision-soft/melody/v2/security"
    securitycontract "github.com/precision-soft/melody/v2/security/contract"
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
    stateless                  bool
    inheritGlobalAccessControl bool
    mergeStrategy              AccessControlMergeStrategy
    accessControl              *security.AccessControl
    roleHierarchy              *security.RoleHierarchy
    accessDecisionManager      securitycontract.AccessDecisionManager
    entryPoint                 securitycontract.EntryPoint
    accessDeniedHandler        securitycontract.AccessDeniedHandler
}

func (instance FirewallOverrideConfiguration) WithStateless(stateless bool) FirewallOverrideConfiguration {
    instance.stateless = stateless

    return instance
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
    return instance.addFirewall(
        name,
        matcher,
        rules,
        tokenSource,
        loginPath,
        logoutPath,
        loginHandler,
        logoutHandler,
        override,
    )
}

func (instance *Builder) AddStatelessFirewall(
    name string,
    matcher securitycontract.Matcher,
    rules []securitycontract.Rule,
    tokenSource securitycontract.TokenSource,
    override FirewallOverrideConfiguration,
) *Builder {
    override.stateless = true

    return instance.addFirewall(
        name,
        matcher,
        rules,
        tokenSource,
        "",
        "",
        nil,
        nil,
        override,
    )
}

func (instance *Builder) AddStatefulFirewall(
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
    override.stateless = false

    return instance.addFirewall(
        name,
        matcher,
        rules,
        tokenSource,
        loginPath,
        logoutPath,
        loginHandler,
        logoutHandler,
        override,
    )
}

func (instance *Builder) addFirewall(
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
    instance.validateFirewall(
        name,
        matcher,
        tokenSource,
        loginPath,
        logoutPath,
        loginHandler,
        logoutHandler,
        override,
    )

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

func (instance *Builder) validateFirewall(
    name string,
    matcher securitycontract.Matcher,
    tokenSource securitycontract.TokenSource,
    loginPath string,
    logoutPath string,
    loginHandler securitycontract.LoginHandler,
    logoutHandler securitycontract.LogoutHandler,
    override FirewallOverrideConfiguration,
) {
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

    if true == override.stateless {
        if "" != loginPath || "" != logoutPath || nil != loginHandler || nil != logoutHandler {
            exception.Panic(
                exception.NewError(
                    "security stateless firewall may not define login or logout configuration",
                    exceptioncontract.Context{
                        "firewallName": name,
                    },
                    nil,
                ),
            )
        }

        return
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
