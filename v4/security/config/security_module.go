package config

import (
    "fmt"

    "github.com/precision-soft/melody/v4/bound"
    melodyconfig "github.com/precision-soft/melody/v4/config"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
    "github.com/precision-soft/melody/v4/security"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

/* AccessControlMergeStrategy orders the merged rule list; it does not decide which rule answers a request. The matcher resolves by category first, so list position decides only the ties inside a category, and a rule that must beat a longer or more exact sibling needs a more specific path. */
type AccessControlMergeStrategy string

const (
    AccessControlMergeLocalFirst   AccessControlMergeStrategy = "localFirst"
    AccessControlMergeGlobalFirst  AccessControlMergeStrategy = "globalFirst"
    AccessControlMergeOverrideOnly AccessControlMergeStrategy = "overrideOnly"
)

/* GlobalConfig is what every firewall inherits unless its override says otherwise; every field may be nil, a typed nil is refused naming the field. */
type GlobalConfig struct {
    AccessControl         *security.AccessControl
    RoleHierarchy         *security.RoleHierarchy
    AccessDecisionManager securitycontract.AccessDecisionManager
    EntryPoint            securitycontract.EntryPoint
    AccessDeniedHandler   securitycontract.AccessDeniedHandler

    /* UnavailableRetryAfter is the Retry-After of the 503 an unavailable token earns on a protected route: absent is 5s, 1s to 1h in whole seconds, unlimited refused. */
    UnavailableRetryAfter bound.Duration
}

/* FirewallOverride is what one firewall declares against the global configuration; the zero value inherits the global access control, local rules first, and overrides nothing. A typed nil among the dependencies is refused naming the field. */
type FirewallOverride struct {
    /* GlobalAccessControlDisabled cuts this firewall off the global access control; a firewall that then declares no AccessControl is refused, security.NewAccessControl(nil) keeping it open on purpose. */
    GlobalAccessControlDisabled bool

    /* MergeStrategy orders the merged rule list: "" and AccessControlMergeLocalFirst put the firewall's rules before the global ones, AccessControlMergeGlobalFirst after them, AccessControlMergeOverrideOnly drops the global ones; anything else is refused naming the field. */
    MergeStrategy AccessControlMergeStrategy

    AccessControl         *security.AccessControl
    RoleHierarchy         *security.RoleHierarchy
    AccessDecisionManager securitycontract.AccessDecisionManager
    EntryPoint            securitycontract.EntryPoint
    AccessDeniedHandler   securitycontract.AccessDeniedHandler

    /* UnavailableRetryAfter overrides GlobalConfig.UnavailableRetryAfter for this firewall; absent inherits it. */
    UnavailableRetryAfter bound.Duration
}

/* FirewallConfig is what a firewall declares beyond its name, its matcher and its token source: its rules, the sign-in and sign-out doors it serves, and what it overrides of the global configuration. */
type FirewallConfig struct {
    Rules []securitycontract.Rule

    Login  LoginConfig
    Logout LogoutConfig

    Override FirewallOverride
}

func isValidAccessControlMergeStrategy(mergeStrategy AccessControlMergeStrategy) bool {
    return "" == mergeStrategy ||
        AccessControlMergeLocalFirst == mergeStrategy ||
        AccessControlMergeGlobalFirst == mergeStrategy ||
        AccessControlMergeOverrideOnly == mergeStrategy
}

type firewallDeclaration struct {
    name        string
    matcher     securitycontract.Matcher
    tokenSource securitycontract.TokenSource
    config      FirewallConfig
}

type Configuration struct {
    global    GlobalConfig
    firewalls []firewallDeclaration
}

type Builder struct {
    globalConfigured bool
    global           GlobalConfig
    firewalls        []firewallDeclaration
    bootWarnings     []BootWarning
}

func NewBuilder() *Builder {
    return &Builder{
        firewalls: make([]firewallDeclaration, 0),
    }
}

/* SetGlobal declares the global configuration once; a second call is refused. */
func (instance *Builder) SetGlobal(config GlobalConfig) *Builder {
    if true == instance.globalConfigured {
        exception.Panic(exception.NewError("security global configuration may only be defined once", nil, nil))
    }

    /* a typed nil means a dependency was declared and holds nothing; the compile step would read it as declared and hand the runtime a nil to dereference */
    refuseTypedNilGlobalDependency("access decision manager", "GlobalConfig.AccessDecisionManager", config.AccessDecisionManager)
    refuseTypedNilGlobalDependency("entry point", "GlobalConfig.EntryPoint", config.EntryPoint)
    refuseTypedNilGlobalDependency("access denied handler", "GlobalConfig.AccessDeniedHandler", config.AccessDeniedHandler)

    if retryAfterErr := validateGlobalUnavailableRetryAfter(config); nil != retryAfterErr {
        exception.Panic(exception.FromError(retryAfterErr))
    }

    instance.globalConfigured = true
    instance.global = config

    return instance
}

/* AddFirewall declares a firewall under its name, matched by the matcher, authenticating through the token source; an empty name, a nil matcher or token source, a nil rule, a duplicate name, an unknown merge strategy, a typed nil in the override and a partial login configuration are refused at the call, naming the firewall. */
func (instance *Builder) AddFirewall(
    name string,
    matcher securitycontract.Matcher,
    tokenSource securitycontract.TokenSource,
    config FirewallConfig,
) *Builder {
    declaration := firewallDeclaration{
        name:        name,
        matcher:     matcher,
        tokenSource: tokenSource,
        config:      config,
    }
    declaration.config.Rules = append([]securitycontract.Rule{}, config.Rules...)

    if identityErr := validateFirewallIdentity(declaration); nil != identityErr {
        exception.Panic(exception.FromError(identityErr))
    }

    for _, firewall := range instance.firewalls {
        if name == firewall.name {
            exception.Panic(
                exception.NewError(
                    "security firewall name is already declared",
                    exceptioncontract.Context{
                        "firewallName": name,
                    },
                    nil,
                ),
            )
        }
    }

    if declarationErr := validateFirewallDeclaration(declaration); nil != declarationErr {
        exception.Panic(exception.FromError(declarationErr))
    }

    refuseTypedNilOverrideDependency(name, "access decision manager", "FirewallConfig.Override.AccessDecisionManager", config.Override.AccessDecisionManager)
    refuseTypedNilOverrideDependency(name, "entry point", "FirewallConfig.Override.EntryPoint", config.Override.EntryPoint)
    refuseTypedNilOverrideDependency(name, "access denied handler", "FirewallConfig.Override.AccessDeniedHandler", config.Override.AccessDeniedHandler)

    if retryAfterErr := validateOverrideUnavailableRetryAfter(declaration); nil != retryAfterErr {
        exception.Panic(exception.FromError(retryAfterErr))
    }

    instance.firewalls = append(instance.firewalls, declaration)

    return instance
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

    instance.bootWarnings = compileBootWarnings(compiled)

    return compiled
}

/* BootWarnings answers the hazards the last BuildAndCompile found in the declaration, each one a configuration the framework admits and does not refuse; the application writes them into the configured journal once, at the boot of an http process or a command */
func (instance *Builder) BootWarnings() []BootWarning {
    return append([]BootWarning{}, instance.bootWarnings...)
}

func validateGlobalUnavailableRetryAfter(global GlobalConfig) error {
    _, retryAfterErr := melodyconfig.ResolveUnavailableRetryAfter(global.UnavailableRetryAfter, "GlobalConfig.UnavailableRetryAfter")

    return retryAfterErr
}

func validateOverrideUnavailableRetryAfter(declaration firewallDeclaration) error {
    _, retryAfterErr := melodyconfig.ResolveUnavailableRetryAfter(declaration.config.Override.UnavailableRetryAfter, "FirewallConfig.Override.UnavailableRetryAfter")
    if nil != retryAfterErr {
        refusal := exception.FromError(retryAfterErr)
        refusal.SetContextValue("firewallName", declaration.name)

        return refusal
    }

    return nil
}

func effectiveUnavailableRetryAfter(global GlobalConfig, declaration firewallDeclaration) bound.Duration {
    if true == declaration.config.Override.UnavailableRetryAfter.IsAbsent() {
        return global.UnavailableRetryAfter
    }

    return declaration.config.Override.UnavailableRetryAfter
}

func validateFirewallIdentity(declaration firewallDeclaration) error {
    if "" == declaration.name {
        return exception.NewError("security firewall name may not be empty", nil, nil)
    }

    if true == internal.IsNilInterface(declaration.matcher) {
        return exception.NewError(
            "security firewall matcher is nil",
            exceptioncontract.Context{
                "firewallName": declaration.name,
            },
            nil,
        )
    }

    for ruleIndex, rule := range declaration.config.Rules {
        if true == internal.IsNilInterface(rule) {
            return exception.NewError(
                "security firewall rule is nil",
                exceptioncontract.Context{
                    "firewallName": declaration.name,
                    "ruleIndex":    ruleIndex,
                },
                nil,
            )
        }
    }

    return nil
}

func validateFirewallDeclaration(declaration firewallDeclaration) error {
    if true == internal.IsNilInterface(declaration.tokenSource) {
        return exception.NewError(
            "security firewall token source is nil",
            exceptioncontract.Context{
                "firewallName": declaration.name,
            },
            nil,
        )
    }

    if false == isValidAccessControlMergeStrategy(declaration.config.Override.MergeStrategy) {
        return exception.NewError(
            "unknown security access control merge strategy",
            exceptioncontract.Context{
                "firewallName":  declaration.name,
                "field":         "FirewallConfig.Override.MergeStrategy",
                "mergeStrategy": string(declaration.config.Override.MergeStrategy),
            },
            nil,
        )
    }

    return validateFirewallDoors(declaration)
}

/* refuseTypedNilGlobalDependency refuses a dependency the caller declared that holds a typed nil; a plain nil is the ordinary "not declared". */
func refuseTypedNilGlobalDependency(dependencyName string, field string, dependency any) {
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
                "field":          field,
            },
            nil,
        ),
    )
}

/* a typed nil reads as declared, and the compile step would skip the global fallback for it */
func refuseTypedNilOverrideDependency(firewallName string, dependencyName string, field string, dependency any) {
    if nil == dependency {
        return
    }

    if false == internal.IsNilInterface(dependency) {
        return
    }

    exception.Panic(
        exception.NewError(
            "security firewall "+dependencyName+" is a typed nil",
            exceptioncontract.Context{
                "firewallName":   firewallName,
                "dependency":     dependencyName,
                "dependencyType": fmt.Sprintf("%T", dependency),
                "field":          field,
                "source":         string(security.SourceFirewall),
            },
            nil,
        ),
    )
}
