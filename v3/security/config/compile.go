package config

import (
    "fmt"

    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/precision-soft/melody/v3/internal"
    "github.com/precision-soft/melody/v3/security"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

/* Compile turns a Configuration into the compiled form the runtime reads. The argument is the thing to know about this door: Configuration carries only unexported fields and no constructor, and Builder never hands one out, so no caller outside this package can build a non-empty one — a composition root calling Compile from outside gets the empty configuration's answer, which is a nil compiled configuration and a nil error, meaning "no security was declared". That is the ordinary case and not a hidden failure: the application installs security through the module hook, which is the only writer of the field the runtime reads, and a nil there simply means no module registered any. The public path from a declaration to the runtime is Builder.BuildAndCompile. */
func Compile(configuration Configuration) (*security.CompiledConfiguration, error) {
    if 0 == len(configuration.firewalls) {
        /* a global access control declared without any firewall still enforces: the resolution listener matches no firewall and sets no context, the access control listener falls back to this global control and denies unauthenticated access, which is the behaviour the runtime is built and tested for. Dropping it here would silently disable every declared global rule. */
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

/* refuseTypedNilDependency refuses a dependency that reads as declared and holds a typed nil. The three interfaces an override carries — the decision manager, the entry point, the denied handler — are the ones the plain comparison above cannot judge: `var manager *myManager` handed to the setter is not nil as an interface, so the fallback to the global one is skipped, the firewall compiles green, and the first request behind it dereferences a nil receiver inside the listener. The matcher, the token source and the login and logout handlers are refused by name in this same loop; the three that are not include the one that decides access, so the silence fell on the security-critical dependency and on no other.

   The refusal names the source, because a typed nil that arrived through the global configuration and one that arrived through this firewall's own override are two different mistakes in two different files. */
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
