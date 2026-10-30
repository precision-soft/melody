package config

import (
    "fmt"

    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
    "github.com/precision-soft/melody/v4/security"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

/* Compile turns a Configuration into the compiled form the runtime reads. Configuration has only unexported fields and no constructor, so a caller outside this package can pass only the empty one, which compiles to a nil configuration and a nil error, meaning no security was declared. The public path from a declaration to the runtime is Builder.BuildAndCompile. */
func Compile(configuration Configuration) (*security.CompiledConfiguration, error) {
    compiled, _, err := compile(configuration)

    return compiled, err
}

/* compile is Compile that also answers the firewalls whose effective access control is the empty one it built itself, for the boot warning of Builder.BootWarnings */
func compile(configuration Configuration) (*security.CompiledConfiguration, []string, error) {
    if 0 == len(configuration.firewalls) {
        /* a global access control declared without any firewall still enforces: the access control listener falls back to it */
        if nil != configuration.global.AccessControl {
            return security.NewCompiledConfiguration(nil, configuration.global.AccessControl), nil, nil
        }

        return nil, nil, nil
    }

    compiledFirewalls := make([]*security.CompiledFirewall, 0)
    emptyAccessControlFirewallNames := make([]string, 0)
    declaredFirewallNames := make(map[string]struct{}, len(configuration.firewalls))

    for _, firewall := range configuration.firewalls {
        if identityErr := validateFirewallIdentity(firewall); nil != identityErr {
            return nil, nil, identityErr
        }

        if _, declared := declaredFirewallNames[firewall.name]; true == declared {
            return nil, nil, exception.NewError(
                "security firewall name is already declared",
                exceptioncontract.Context{
                    "firewallName": firewall.name,
                },
                nil,
            )
        }
        declaredFirewallNames[firewall.name] = struct{}{}

        if declarationErr := validateFirewallDeclaration(firewall); nil != declarationErr {
            return nil, nil, declarationErr
        }

        effectiveRoleHierarchy := firewall.config.Override.RoleHierarchy
        roleHierarchySource := security.SourceFirewall
        if nil == effectiveRoleHierarchy {
            effectiveRoleHierarchy = configuration.global.RoleHierarchy
            if nil != effectiveRoleHierarchy {
                roleHierarchySource = security.SourceGlobal
            } else {
                roleHierarchySource = security.SourceNone
            }
        }

        effectiveDecisionManager := firewall.config.Override.AccessDecisionManager
        decisionManagerSource := security.SourceFirewall
        if nil == effectiveDecisionManager {
            effectiveDecisionManager = configuration.global.AccessDecisionManager
            if nil != effectiveDecisionManager {
                decisionManagerSource = security.SourceGlobal
            } else {
                decisionManagerSource = security.SourceNone
            }
        }

        if typedNilErr := refuseTypedNilDependency(firewall.name, "access decision manager", decisionManagerSource, effectiveDecisionManager); nil != typedNilErr {
            return nil, nil, typedNilErr
        }

        if nil != effectiveRoleHierarchy && false == internal.IsNilInterface(effectiveDecisionManager) {
            if dm, ok := effectiveDecisionManager.(*security.AccessDecisionManager); true == ok {
                upgradedVoters := make([]securitycontract.Voter, 0, len(dm.Voters()))
                upgraded := false

                for _, voter := range dm.Voters() {
                    if rv, isRoleVoter := voter.(*security.RoleVoter); true == isRoleVoter {
                        upgradedVoters = append(upgradedVoters, security.NewRoleHierarchyVoter(effectiveRoleHierarchy, rv))
                        upgraded = true
                    } else {
                        upgradedVoters = append(upgradedVoters, voter)
                    }
                }

                if true == upgraded {
                    effectiveDecisionManager = security.NewAccessDecisionManager(dm.Strategy(), upgradedVoters)
                }
            }
        }

        effectiveEntryPoint := firewall.config.Override.EntryPoint
        entryPointSource := security.SourceFirewall
        if nil == effectiveEntryPoint {
            effectiveEntryPoint = configuration.global.EntryPoint
            if nil != effectiveEntryPoint {
                entryPointSource = security.SourceGlobal
            } else {
                entryPointSource = security.SourceNone
            }
        }

        if typedNilErr := refuseTypedNilDependency(firewall.name, "entry point", entryPointSource, effectiveEntryPoint); nil != typedNilErr {
            return nil, nil, typedNilErr
        }

        effectiveDeniedHandler := firewall.config.Override.AccessDeniedHandler
        deniedHandlerSource := security.SourceFirewall
        if nil == effectiveDeniedHandler {
            effectiveDeniedHandler = configuration.global.AccessDeniedHandler
            if nil != effectiveDeniedHandler {
                deniedHandlerSource = security.SourceGlobal
            } else {
                deniedHandlerSource = security.SourceNone
            }
        }

        if typedNilErr := refuseTypedNilDependency(firewall.name, "access denied handler", deniedHandlerSource, effectiveDeniedHandler); nil != typedNilErr {
            return nil, nil, typedNilErr
        }

        globalAccessControl := configuration.global.AccessControl
        localAccessControl := firewall.config.Override.AccessControl

        var effectiveAccessControl *security.AccessControl
        accessControlSource := security.SourceFirewall

        if AccessControlMergeOverrideOnly == firewall.config.Override.MergeStrategy {
            effectiveAccessControl = localAccessControl
            if nil == effectiveAccessControl {
                effectiveAccessControl = security.NewAccessControl(nil)
                emptyAccessControlFirewallNames = append(emptyAccessControlFirewallNames, firewall.name)
            }
            accessControlSource = security.SourceFirewall
        } else {
            if true == firewall.config.Override.GlobalAccessControlDisabled {
                effectiveAccessControl = localAccessControl
                if nil == effectiveAccessControl {
                    effectiveAccessControl = security.NewAccessControl(nil)
                    emptyAccessControlFirewallNames = append(emptyAccessControlFirewallNames, firewall.name)
                }
                accessControlSource = security.SourceFirewall
            } else {
                effectiveAccessControl = mergeAccessControls(globalAccessControl, localAccessControl, firewall.config.Override.MergeStrategy)
                accessControlSource = security.SourceMerged
            }
        }

        matcherDescription := ""
        if describer, ok := firewall.matcher.(interface{ String() string }); true == ok {
            matcherDescription = describer.String()
        }

        compiledFirewalls = append(
            compiledFirewalls,
            security.NewCompiledFirewall(
                security.CompiledFirewallConfig{
                    Name:                        firewall.name,
                    Matcher:                     firewall.matcher,
                    MatcherDescription:          matcherDescription,
                    Rules:                       append([]securitycontract.Rule{}, firewall.config.Rules...),
                    TokenSource:                 firewall.tokenSource,
                    AccessControl:               effectiveAccessControl,
                    AccessDecisionManager:       effectiveDecisionManager,
                    RoleHierarchy:               effectiveRoleHierarchy,
                    EntryPoint:                  effectiveEntryPoint,
                    AccessDeniedHandler:         effectiveDeniedHandler,
                    LoginPath:                   firewall.config.LoginPath,
                    LogoutPath:                  firewall.config.LogoutPath,
                    LoginHandler:                firewall.config.LoginHandler,
                    LogoutHandler:               firewall.config.LogoutHandler,
                    RoleHierarchySource:         roleHierarchySource,
                    AccessDecisionManagerSource: decisionManagerSource,
                    AccessControlSource:         accessControlSource,
                    EntryPointSource:            entryPointSource,
                    AccessDeniedHandlerSource:   deniedHandlerSource,
                },
            ),
        )
    }

    return security.NewCompiledConfiguration(
        compiledFirewalls,
        configuration.global.AccessControl,
    ), emptyAccessControlFirewallNames, nil
}

/* refuseTypedNilDependency refuses a dependency that reads as declared and holds a typed nil: the fallback to the global one would be skipped and the first request behind the firewall would dereference it. The refusal names whether the value came from the global configuration or from the firewall's override. */
func refuseTypedNilDependency(firewallName string, dependencyName string, source security.Source, dependency any) error {
    if nil == dependency {
        return nil
    }

    if false == internal.IsNilInterface(dependency) {
        return nil
    }

    return exception.NewError(
        "security firewall "+dependencyName+" is a typed nil",
        exceptioncontract.Context{
            "firewallName":   firewallName,
            "dependency":     dependencyName,
            "dependencyType": fmt.Sprintf("%T", dependency),
            "source":         string(source),
        },
        nil,
    )
}

func mergeAccessControls(globalAccessControl *security.AccessControl, localAccessControl *security.AccessControl, strategy AccessControlMergeStrategy) *security.AccessControl {
    globalRules := make([]security.AccessControlRule, 0)
    localRules := make([]security.AccessControlRule, 0)

    if nil != globalAccessControl {
        globalRules = append(globalRules, globalAccessControl.Rules()...)
    }

    if nil != localAccessControl {
        localRules = append(localRules, localAccessControl.Rules()...)
    }

    mergedRules := make([]security.AccessControlRule, 0)

    if AccessControlMergeGlobalFirst == strategy {
        mergedRules = append(mergedRules, globalRules...)
        mergedRules = append(mergedRules, localRules...)
    } else {
        mergedRules = append(mergedRules, localRules...)
        mergedRules = append(mergedRules, globalRules...)
    }

    return security.NewAccessControl(mergedRules)
}
