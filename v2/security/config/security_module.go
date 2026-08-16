package config

import (
    "fmt"

    "github.com/precision-soft/melody/v2/exception"
    exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
    "github.com/precision-soft/melody/v2/internal"
    "github.com/precision-soft/melody/v2/security"
    securitycontract "github.com/precision-soft/melody/v2/security/contract"
)

/* AccessControlMergeStrategy orders the merged rule LIST — it does not decide which rule answers a request. The matcher resolves by category first (an exact path beats every prefix, a longer prefix beats a shorter one, every prefix beats every regex, the empty-prefix fallback answers last), so a local /admin prefix rule is still beaten by a global /admin/reports rule under localFirst. List position decides only what the categories leave tied: which of two equal-length prefixes, which regex, which exact duplicate and which fallback wins. A rule that must beat a longer or more exact sibling needs a more specific path, not an earlier position. */
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

/* FirewallOverrideConfiguration starts from the constructor defaults wherever it is built: the fields are unexported, so outside this package the only writes are the With setters, and a setter called on the exact zero value first reads the receiver as NewFirewallOverrideConfiguration before applying its own field. Without that reading, a zero value plus WithInheritGlobalAccessControl(false) carried an empty merge strategy, which the builder reads as an unconfigured override and repairs by writing the inheritance back to true — so the one field the caller set was the one field that never arrived. A firewall that inherits nothing and declares no rules of its own enforces nothing behind it: the compiled access control is empty, no rule matches, and every request reaches its handler. */
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

func (instance FirewallOverrideConfiguration) normalizeZeroReceiver() FirewallOverrideConfiguration {
    if (FirewallOverrideConfiguration{}) == instance {
        return NewFirewallOverrideConfiguration()
    }

    return instance
}

func (instance FirewallOverrideConfiguration) WithStateless(stateless bool) FirewallOverrideConfiguration {
    instance = instance.normalizeZeroReceiver()
    instance.stateless = stateless

    return instance
}

func (instance FirewallOverrideConfiguration) WithAccessControl(accessControl *security.AccessControl) FirewallOverrideConfiguration {
    instance = instance.normalizeZeroReceiver()
    instance.accessControl = accessControl

    return instance
}

func (instance FirewallOverrideConfiguration) WithRoleHierarchy(roleHierarchy *security.RoleHierarchy) FirewallOverrideConfiguration {
    instance = instance.normalizeZeroReceiver()
    instance.roleHierarchy = roleHierarchy

    return instance
}

func (instance FirewallOverrideConfiguration) WithAccessDecisionManager(accessDecisionManager securitycontract.AccessDecisionManager) FirewallOverrideConfiguration {
    instance = instance.normalizeZeroReceiver()
    instance.accessDecisionManager = accessDecisionManager

    return instance
}

func (instance FirewallOverrideConfiguration) WithEntryPoint(entryPoint securitycontract.EntryPoint) FirewallOverrideConfiguration {
    instance = instance.normalizeZeroReceiver()
    instance.entryPoint = entryPoint

    return instance
}

func (instance FirewallOverrideConfiguration) WithAccessDeniedHandler(accessDeniedHandler securitycontract.AccessDeniedHandler) FirewallOverrideConfiguration {
    instance = instance.normalizeZeroReceiver()
    instance.accessDeniedHandler = accessDeniedHandler

    return instance
}

/* WithMergeStrategy refuses a value that is none of the three by name: the merge reads the strategy by equality, so an unrecognised one behaved as localFirst in silence and an authorization policy the caller believed they had chosen was never applied. The empty string is refused with the rest, because it is the value the builder reads as an unconfigured override. */
func (instance FirewallOverrideConfiguration) WithMergeStrategy(mergeStrategy AccessControlMergeStrategy) FirewallOverrideConfiguration {
    if false == isValidAccessControlMergeStrategy(mergeStrategy) {
        exception.Panic(
            exception.NewError(
                "unknown security access control merge strategy",
                exceptioncontract.Context{
                    "mergeStrategy": string(mergeStrategy),
                },
                nil,
            ),
        )
    }

    instance = instance.normalizeZeroReceiver()
    instance.mergeStrategy = mergeStrategy

    return instance
}

/* WithInheritGlobalAccessControl turns the global policy off for this firewall. A firewall that inherits nothing and declares no access control of its own enforces nothing: pair it with WithAccessControl. */
func (instance FirewallOverrideConfiguration) WithInheritGlobalAccessControl(inheritGlobalAccessControl bool) FirewallOverrideConfiguration {
    instance = instance.normalizeZeroReceiver()
    instance.inheritGlobalAccessControl = inheritGlobalAccessControl

    return instance
}

func isValidAccessControlMergeStrategy(mergeStrategy AccessControlMergeStrategy) bool {
    return AccessControlMergeLocalFirst == mergeStrategy ||
        AccessControlMergeGlobalFirst == mergeStrategy ||
        AccessControlMergeOverrideOnly == mergeStrategy
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

    /* the three interface dependencies are refused as typed nils here the way the firewall's own are refused below: a plain nil means the global configuration declares none, which is ordinary, while a typed nil means one was declared and holds nothing — and the compile step reads it as declared, skips the fallback, and hands the runtime a value that dereferences on the first request behind the firewall */
    refuseTypedNilGlobalDependency("access decision manager", accessDecisionManager)
    refuseTypedNilGlobalDependency("entry point", entryPoint)
    refuseTypedNilGlobalDependency("access denied handler", accessDeniedHandler)

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

func (instance *Builder) BuildAndCompile() *security.CompiledConfiguration {
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
        /* the zero value of the exported override struct must inherit the global access control the same way NewFirewallOverrideConfiguration does: an override that reaches here unconfigured carries no local access control, and without inheritance the firewall compiles an empty non-nil access control that never falls back to the global policy, opening every route behind the firewall */
        override.mergeStrategy = AccessControlMergeLocalFirst
        override.inheritGlobalAccessControl = true
    }

    instance.firewalls = append(
        instance.firewalls,
        FirewallConfiguration{
            name:          name,
            matcher:       matcher,
            rules:         append([]securitycontract.Rule{}, rules...),
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

    if true == internal.IsNilInterface(matcher) {
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

    if true == internal.IsNilInterface(tokenSource) {
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
        if "" != loginPath || "" != logoutPath || false == internal.IsNilInterface(loginHandler) || false == internal.IsNilInterface(logoutHandler) {
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

    if true == internal.IsNilInterface(loginHandler) {
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

    if true == internal.IsNilInterface(logoutHandler) {
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

/* refuseTypedNilGlobalDependency refuses at the door what Compile refuses at the end: a dependency the caller declared and that holds a typed nil. A plain nil is the ordinary "not declared" and travels; the typed nil is the one that reads as declared everywhere downstream. */
func refuseTypedNilGlobalDependency(dependencyName string, dependency any) {
    if nil == dependency {
        return
    }

    if false == internal.IsNilInterface(dependency) {
        return
    }

    exception.Panic(
        exception.NewError(
            "security global "+dependencyName+" is a typed nil",
            exceptioncontract.Context{
                "dependency":     dependencyName,
                "dependencyType": fmt.Sprintf("%T", dependency),
            },
            nil,
        ),
    )
}

func NewFirewallOverrideConfiguration() FirewallOverrideConfiguration {
    return FirewallOverrideConfiguration{
        inheritGlobalAccessControl: true,
        mergeStrategy:              AccessControlMergeLocalFirst,
    }
}
