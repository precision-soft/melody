package config

import (
    "fmt"

    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/precision-soft/melody/v3/internal"
    "github.com/precision-soft/melody/v3/security"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

/* Compile compiles a Configuration. External callers cannot populate its private fields and should use Builder.BuildAndCompile. An empty configuration returns nil, nil to represent absent security configuration. */
func Compile(configuration Configuration) (*security.CompiledConfiguration, error) {
    if 0 == len(configuration.firewalls) {

        if nil != configuration.global.accessControl {
            return security.NewCompiledConfiguration(nil, configuration.global.accessControl), nil
        }

        return nil, nil
    }

    compiledFirewalls := make([]*security.CompiledFirewall, 0)

    for _, firewall := range configuration.firewalls {
        if "" == firewall.name {
            return nil, exception.NewError("security firewall name may not be empty", nil, nil)
        }

        if true == internal.IsNilInterface(firewall.matcher) {
            return nil, exception.NewError(
                "security firewall matcher is nil",
                exceptioncontract.Context{
                    "firewallName": firewall.name,
                },
                nil,
            )
        }

        if true == internal.IsNilInterface(firewall.tokenSource) {
            return nil, exception.NewError(
                "security firewall token source is nil",
                exceptioncontract.Context{
                    "firewallName": firewall.name,
                },
                nil,
            )
        }

        if true == firewall.override.stateless {
            if "" != firewall.loginPath || "" != firewall.logoutPath || false == internal.IsNilInterface(firewall.loginHandler) || false == internal.IsNilInterface(firewall.logoutHandler) {
                return nil, exception.NewError(
                    "security stateless firewall may not define login or logout configuration",
                    exceptioncontract.Context{
                        "firewallName": firewall.name,
                    },
                    nil,
                )
            }
        } else {
            if "" == firewall.loginPath {
                return nil, exception.NewError(
                    "security firewall login path may not be empty",
                    exceptioncontract.Context{
                        "firewallName": firewall.name,
                    },
                    nil,
                )
            }

            if "" == firewall.logoutPath {
                return nil, exception.NewError(
                    "security firewall logout path may not be empty",
                    exceptioncontract.Context{
                        "firewallName": firewall.name,
                    },
                    nil,
                )
            }

            if true == internal.IsNilInterface(firewall.loginHandler) {
                return nil, exception.NewError(
                    "security firewall login handler is nil",
                    exceptioncontract.Context{
                        "firewallName": firewall.name,
                    },
                    nil,
                )
            }

            if true == internal.IsNilInterface(firewall.logoutHandler) {
                return nil, exception.NewError(
                    "security firewall logout handler is nil",
                    exceptioncontract.Context{
                        "firewallName": firewall.name,
                    },
                    nil,
                )
            }
        }

        effectiveRoleHierarchy := firewall.override.roleHierarchy
        roleHierarchySource := security.SourceFirewall
        if nil == effectiveRoleHierarchy {
            effectiveRoleHierarchy = configuration.global.roleHierarchy
            if nil != effectiveRoleHierarchy {
                roleHierarchySource = security.SourceGlobal
            } else {
                roleHierarchySource = security.SourceNone
            }
        }

        effectiveDecisionManager := firewall.override.accessDecisionManager
        decisionManagerSource := security.SourceFirewall
        if nil == effectiveDecisionManager {
            effectiveDecisionManager = configuration.global.accessDecisionManager
            if nil != effectiveDecisionManager {
                decisionManagerSource = security.SourceGlobal
            } else {
                decisionManagerSource = security.SourceNone
            }
        }

        if typedNilErr := refuseTypedNilDependency(firewall.name, "access decision manager", decisionManagerSource, effectiveDecisionManager); nil != typedNilErr {
            return nil, typedNilErr
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
                    effectiveDecisionManager = security.NewAccessDecisionManagerWithVoters(dm.Strategy(), upgradedVoters)
                }
            }
        }

        effectiveEntryPoint := firewall.override.entryPoint
        entryPointSource := security.SourceFirewall
        if nil == effectiveEntryPoint {
            effectiveEntryPoint = configuration.global.entryPoint
            if nil != effectiveEntryPoint {
                entryPointSource = security.SourceGlobal
            } else {
                entryPointSource = security.SourceNone
            }
        }

        if typedNilErr := refuseTypedNilDependency(firewall.name, "entry point", entryPointSource, effectiveEntryPoint); nil != typedNilErr {
            return nil, typedNilErr
        }

        effectiveDeniedHandler := firewall.override.accessDeniedHandler
        deniedHandlerSource := security.SourceFirewall
        if nil == effectiveDeniedHandler {
            effectiveDeniedHandler = configuration.global.accessDeniedHandler
            if nil != effectiveDeniedHandler {
                deniedHandlerSource = security.SourceGlobal
            } else {
                deniedHandlerSource = security.SourceNone
            }
        }

        if typedNilErr := refuseTypedNilDependency(firewall.name, "access denied handler", deniedHandlerSource, effectiveDeniedHandler); nil != typedNilErr {
            return nil, typedNilErr
        }

        globalAccessControl := configuration.global.accessControl
        localAccessControl := firewall.override.accessControl

        var effectiveAccessControl *security.AccessControl
        accessControlSource := security.SourceFirewall

        if AccessControlMergeOverrideOnly == firewall.override.mergeStrategy {
            effectiveAccessControl = localAccessControl
            if nil == effectiveAccessControl {
                effectiveAccessControl = security.NewAccessControl()
            }
            accessControlSource = security.SourceFirewall
        } else {
            inheritGlobal := firewall.override.inheritGlobalAccessControl
            if false == inheritGlobal {
                effectiveAccessControl = localAccessControl
                if nil == effectiveAccessControl {
                    effectiveAccessControl = security.NewAccessControl()
                }
                accessControlSource = security.SourceFirewall
            } else {
                effectiveAccessControl = mergeAccessControls(globalAccessControl, localAccessControl, firewall.override.mergeStrategy)
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
                firewall.name,
                firewall.matcher,
                matcherDescription,
                append([]securitycontract.Rule{}, firewall.rules...),
                firewall.tokenSource,
                effectiveAccessControl,
                effectiveDecisionManager,
                effectiveRoleHierarchy,
                effectiveEntryPoint,
                effectiveDeniedHandler,
                firewall.loginPath,
                firewall.logoutPath,
                firewall.loginHandler,
                firewall.logoutHandler,
                roleHierarchySource,
                decisionManagerSource,
                accessControlSource,
                entryPointSource,
                deniedHandlerSource,
            ),
        )
    }

    return security.NewCompiledConfiguration(
        compiledFirewalls,
        configuration.global.accessControl,
    ), nil
}

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

    return security.NewAccessControl(mergedRules...)
}
