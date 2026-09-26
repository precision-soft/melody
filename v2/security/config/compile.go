package config

import (
    "fmt"

    "github.com/precision-soft/melody/v2/exception"
    exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
    "github.com/precision-soft/melody/v2/internal"
    "github.com/precision-soft/melody/v2/security"
)

/* Compile turns a Configuration into the compiled form the runtime reads. Configuration has only unexported fields and no constructor, so a caller outside this package can pass only the empty one, which compiles to a nil configuration and a nil error, meaning no security was declared. The public path from a declaration to the runtime is Builder.BuildAndCompile. */
func Compile(configuration Configuration) (*security.CompiledConfiguration, error) {
    if 0 == len(configuration.firewalls) {
        /* a global access control declared without any firewall still enforces: the access control listener falls back to it */
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

        /* the hierarchy reaches the decision manager through the optional capability, not an assertion on the concrete type, so a manager of the integrator's own receives it; one handed a hierarchy it cannot apply is refused here by name, or security.IsGranted would grant what the enforcement path refuses */
        if nil != effectiveRoleHierarchy && false == internal.IsNilInterface(effectiveDecisionManager) {
            hierarchyAware, isHierarchyAware := effectiveDecisionManager.(security.RoleHierarchyAware)
            if false == isHierarchyAware {
                return nil, exception.NewError(
                    "security access decision manager cannot apply the declared role hierarchy",
                    exceptioncontract.Context{
                        "firewallName":          firewall.name,
                        "roleHierarchySource":   roleHierarchySource,
                        "decisionManagerSource": decisionManagerSource,
                        "capability":            "security.RoleHierarchyAware",
                    },
                    nil,
                )
            }

            upgradedDecisionManager := hierarchyAware.WithRoleHierarchy(effectiveRoleHierarchy)
            if true == internal.IsNilInterface(upgradedDecisionManager) {
                return nil, exception.NewError(
                    "security access decision manager answered no manager for the declared role hierarchy",
                    exceptioncontract.Context{
                        "firewallName":          firewall.name,
                        "roleHierarchySource":   roleHierarchySource,
                        "decisionManagerSource": decisionManagerSource,
                    },
                    nil,
                )
            }

            effectiveDecisionManager = upgradedDecisionManager
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

    return security.NewAccessControl(mergedRules...)
}
