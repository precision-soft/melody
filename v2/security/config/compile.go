package config

import (
    "github.com/precision-soft/melody/v2/exception"
    exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
    "github.com/precision-soft/melody/v2/security"
    securitycontract "github.com/precision-soft/melody/v2/security/contract"
)

func Compile(configuration Configuration) (*security.CompiledConfiguration, error) {
    if 0 == len(configuration.firewalls) {
        return nil, nil
    }

    compiledFirewalls := make([]*security.CompiledFirewall, 0)

    for _, firewall := range configuration.firewalls {
        if "" == firewall.name {
            return nil, exception.NewError("security firewall name may not be empty", nil, nil)
        }

        if nil == firewall.matcher {
            return nil, exception.NewError(
                "security firewall matcher is nil",
                exceptioncontract.Context{
                    "firewallName": firewall.name,
                },
                nil,
            )
        }

        if nil == firewall.tokenSource {
            return nil, exception.NewError(
                "security firewall token source is nil",
                exceptioncontract.Context{
                    "firewallName": firewall.name,
                },
                nil,
            )
        }

        if true == firewall.override.stateless {
            if "" != firewall.loginPath || "" != firewall.logoutPath || nil != firewall.loginHandler || nil != firewall.logoutHandler {
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

            if nil == firewall.loginHandler {
                return nil, exception.NewError(
                    "security firewall login handler is nil",
                    exceptioncontract.Context{
                        "firewallName": firewall.name,
                    },
                    nil,
                )
            }

            if nil == firewall.logoutHandler {
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
            }
        }

        effectiveDecisionManager := firewall.override.accessDecisionManager
        decisionManagerSource := security.SourceFirewall
        if nil == effectiveDecisionManager {
            effectiveDecisionManager = configuration.global.accessDecisionManager
            if nil != effectiveDecisionManager {
                decisionManagerSource = security.SourceGlobal
            }
        }

        if nil != effectiveRoleHierarchy && nil != effectiveDecisionManager {
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
                firewall.rules,
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
