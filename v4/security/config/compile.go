package config

import (
    "fmt"
    "strings"

    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
    "github.com/precision-soft/melody/v4/security"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

/* Compile turns a Configuration into the compiled form the runtime reads. Configuration has only unexported fields and no constructor, so a caller outside this package can pass only the empty one, which compiles to a nil configuration and a nil error, meaning no security was declared. The public path from a declaration to the runtime is Builder.BuildAndCompile. A firewall no declared access control reaches, neither its own nor an inherited global one, is refused: it would authorize every path it claims for every token. */
func Compile(configuration Configuration) (*security.CompiledConfiguration, error) {
    if retryAfterErr := validateGlobalUnavailableRetryAfter(configuration.global); nil != retryAfterErr {
        return nil, retryAfterErr
    }

    if escapeErr := refusePercentEscapedAccessControlPaths("", "GlobalConfig.AccessControl", configuration.global.AccessControl); nil != escapeErr {
        return nil, escapeErr
    }

    if 0 == len(configuration.firewalls) {
        /* a global access control declared without any firewall still enforces: the access control listener falls back to it */
        if nil != configuration.global.AccessControl {
            return security.NewCompiledConfiguration(nil, configuration.global.AccessControl), nil
        }

        return nil, nil
    }

    compiledFirewalls := make([]*security.CompiledFirewall, 0)
    declaredFirewallNames := make(map[string]struct{}, len(configuration.firewalls))

    for _, firewall := range configuration.firewalls {
        if identityErr := validateFirewallIdentity(firewall); nil != identityErr {
            return nil, identityErr
        }

        if _, declared := declaredFirewallNames[firewall.name]; true == declared {
            return nil, exception.NewError(
                "security firewall name is already declared",
                exceptioncontract.Context{
                    "firewallName": firewall.name,
                },
                nil,
            )
        }
        declaredFirewallNames[firewall.name] = struct{}{}

        if declarationErr := validateFirewallDeclaration(firewall); nil != declarationErr {
            return nil, declarationErr
        }

        if escapeErr := refusePercentEscapedFirewallPaths(firewall); nil != escapeErr {
            return nil, escapeErr
        }

        if retryAfterErr := validateOverrideUnavailableRetryAfter(firewall); nil != retryAfterErr {
            return nil, retryAfterErr
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

        if binder, isBinder := firewall.tokenSource.(securitycontract.RoleHierarchyBinder); true == isBinder {
            /* a nil *RoleHierarchy inside the interface would read as a declared hierarchy */
            var boundRoleHierarchy securitycontract.RoleHierarchy
            if nil != effectiveRoleHierarchy {
                boundRoleHierarchy = effectiveRoleHierarchy
            }

            if bindErr := binder.BindRoleHierarchy(firewall.name, boundRoleHierarchy); nil != bindErr {
                return nil, bindErr
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
            return nil, typedNilErr
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
            return nil, typedNilErr
        }

        globalAccessControl := configuration.global.AccessControl
        localAccessControl := firewall.config.Override.AccessControl

        var effectiveAccessControl *security.AccessControl
        accessControlSource := security.SourceFirewall

        if AccessControlMergeOverrideOnly == firewall.config.Override.MergeStrategy || true == firewall.config.Override.GlobalAccessControlDisabled {
            if nil == localAccessControl {
                return nil, refuseUndeclaredAccessControl(firewall)
            }

            effectiveAccessControl = localAccessControl
        } else {
            if nil == globalAccessControl && nil == localAccessControl {
                return nil, refuseUndeclaredAccessControl(firewall)
            }

            effectiveAccessControl = mergeAccessControls(globalAccessControl, localAccessControl, firewall.config.Override.MergeStrategy)
            accessControlSource = security.SourceMerged
        }

        if managerErr := refuseProtectedRuleWithoutDecisionManager(firewall.name, effectiveAccessControl, effectiveDecisionManager); nil != managerErr {
            return nil, managerErr
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
                    LoginPath:                   firewall.config.Login.Path,
                    LogoutPath:                  firewall.config.Logout.Path,
                    LoginHandler:                firewall.config.Login.Handler,
                    LogoutHandler:               firewall.config.Logout.Handler,
                    LoginName:                   firewallDoorName(firewall.config.Login.Name, firewall.config.Login.Path, firewall.name, "login"),
                    LoginAttributes:             firewall.config.Login.Attributes,
                    LoginAuthenticator:          firewall.config.Login.Authenticator,
                    LoginMethods:                normalizeFirewallDoorMethods(firewall.config.Login.Methods),
                    LoginTargetPath:             firewall.config.Login.TargetPath,
                    LoginCrossSite:              firewall.config.Login.CrossSite,
                    LoginRateLimit:              firewallDoorRateLimit(firewall.config.Login),
                    LogoutName:                  firewallDoorName(firewall.config.Logout.Name, firewall.config.Logout.Path, firewall.name, "logout"),
                    LogoutAttributes:            firewall.config.Logout.Attributes,
                    LogoutMethods:               normalizeFirewallDoorMethods(firewall.config.Logout.Methods),
                    LogoutTargetPath:            firewall.config.Logout.TargetPath,
                    LogoutCrossSite:             firewall.config.Logout.CrossSite,
                    RoleHierarchySource:         roleHierarchySource,
                    AccessDecisionManagerSource: decisionManagerSource,
                    AccessControlSource:         accessControlSource,
                    EntryPointSource:            entryPointSource,
                    AccessDeniedHandlerSource:   deniedHandlerSource,
                    UnavailableRetryAfter:       effectiveUnavailableRetryAfter(configuration.global, firewall),
                },
            ),
        )
    }

    if shadowErr := refuseShadowedFirewalls(compiledFirewalls); nil != shadowErr {
        return nil, shadowErr
    }

    if reachErr := refuseUnreachableFirewallDoors(compiledFirewalls); nil != reachErr {
        return nil, reachErr
    }

    return security.NewCompiledConfiguration(
        compiledFirewalls,
        configuration.global.AccessControl,
    ), nil
}

func refuseUndeclaredAccessControl(firewall firewallDeclaration) error {
    context := exceptioncontract.Context{
        "firewallName":  firewall.name,
        "field":         "FirewallConfig.Override.AccessControl",
        "mergeStrategy": string(firewall.config.Override.MergeStrategy),
    }
    if true == firewall.config.Override.GlobalAccessControlDisabled {
        context["globalAccessControlDisabled"] = true
    }

    return exception.NewError(
        "security firewall declares no access control and inherits none, so it would authorize every path it claims for every token; declare FirewallConfig.Override.AccessControl, security.NewAccessControl(nil) to keep it open on purpose",
        context,
        nil,
    )
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

/* a rule that is not PUBLIC_ACCESS with no manager would answer every request it matches with the runtime refusal, a 500 */
func refuseProtectedRuleWithoutDecisionManager(firewallName string, accessControl *security.AccessControl, decisionManager securitycontract.AccessDecisionManager) error {
    if false == internal.IsNilInterface(decisionManager) || nil == accessControl {
        return nil
    }

    for ruleIndex, rule := range accessControl.Rules() {
        for _, attribute := range rule.Attributes() {
            if securitycontract.AttributePublicAccess == attribute {
                continue
            }

            return exception.NewError(
                "security firewall access control holds a rule that is not PUBLIC_ACCESS and no access decision manager reaches the firewall, so every request the rule matches would be refused without a decision; declare GlobalConfig.AccessDecisionManager or FirewallConfig.Override.AccessDecisionManager",
                exceptioncontract.Context{
                    "firewallName": firewallName,
                    "ruleIndex":    ruleIndex,
                    "rule":         ruleDeclaration(rule),
                    "attribute":    attribute,
                    "fields":       []string{"GlobalConfig.AccessDecisionManager", "FirewallConfig.Override.AccessDecisionManager"},
                },
                nil,
            )
        }
    }

    return nil
}

func ruleDeclaration(rule security.AccessControlRule) string {
    if "" != rule.Pattern() {
        return rule.Pattern()
    }

    return rule.PathPrefix()
}

/* the first matching firewall wins, so a later prefix an earlier one claims whole never runs its access control */
func refuseShadowedFirewalls(firewalls []*security.CompiledFirewall) error {
    for laterIndex, laterFirewall := range firewalls {
        laterPrefix, isLaterSegment, isLaterPrefix := firewallPrefix(laterFirewall.Matcher())
        if false == isLaterPrefix {
            continue
        }

        for _, earlierFirewall := range firewalls[:laterIndex] {
            earlierPrefix, isEarlierSegment, isEarlierPrefix := firewallPrefix(earlierFirewall.Matcher())
            if false == isEarlierPrefix {
                continue
            }

            if false == prefixClaimsWhole(earlierPrefix, isEarlierSegment, laterPrefix, isLaterSegment) {
                continue
            }

            return exception.NewError(
                "security firewall is never selected: an earlier firewall's prefix claims every path of its own and the first matching firewall wins; declare it first, or bound the earlier prefix at a segment boundary with security.NewSegmentPrefixMatcher",
                exceptioncontract.Context{
                    "firewallName":     laterFirewall.Name(),
                    "entry":            laterPrefix,
                    "shadowedBy":       earlierFirewall.Name(),
                    "shadowedByPrefix": earlierPrefix,
                },
                nil,
            )
        }
    }

    return nil
}

func firewallPrefix(matcher securitycontract.Matcher) (string, bool, bool) {
    switch typedMatcher := matcher.(type) {
    case *security.PathPrefixMatcher:
        if nil == typedMatcher {
            return "", false, false
        }

        return typedMatcher.Prefix(), false, true
    case *security.SegmentPrefixMatcher:
        if nil == typedMatcher {
            return "", false, false
        }

        return typedMatcher.Prefix(), true, true
    }

    return "", false, false
}

/* prefixClaimsWhole reports whether the earlier prefix claims every path the later one claims */
func prefixClaimsWhole(earlierPrefix string, isEarlierSegment bool, laterPrefix string, isLaterSegment bool) bool {
    if false == isEarlierSegment {
        if "" == earlierPrefix || "/" == earlierPrefix {
            return true
        }

        if true == isLaterSegment {
            /* "+/" also covers a raw earlier prefix written as the later spelling with its slash */
            return true == strings.HasPrefix(laterPrefix+"/", earlierPrefix)
        }

        return true == strings.HasPrefix(laterPrefix, earlierPrefix)
    }

    if "/" == earlierPrefix {
        return true
    }

    if true == isLaterSegment {
        return laterPrefix == earlierPrefix || true == strings.HasPrefix(laterPrefix, earlierPrefix+"/")
    }

    /* a raw later prefix stays inside the earlier tree only beneath its slash */
    return true == strings.HasPrefix(laterPrefix, earlierPrefix+"/")
}

func refusePercentEscapedFirewallPaths(firewall firewallDeclaration) error {
    if prefix, _, isPrefix := firewallPrefix(firewall.matcher); true == isPrefix {
        if escapeErr := refusePercentEscapedPath(firewall.name, "matcher", prefix); nil != escapeErr {
            return escapeErr
        }
    }

    return refusePercentEscapedAccessControlPaths(firewall.name, "FirewallConfig.Override.AccessControl", firewall.config.Override.AccessControl)
}

/* a regex rule carries its declaration as a pattern, so it is not read */
func refusePercentEscapedAccessControlPaths(firewallName string, field string, accessControl *security.AccessControl) error {
    if nil == accessControl {
        return nil
    }

    for _, rule := range accessControl.Rules() {
        if escapeErr := refusePercentEscapedPath(firewallName, field, rule.PathPrefix()); nil != escapeErr {
            return escapeErr
        }
    }

    return nil
}

/* a request path is matched decoded, so such a path claims only a resource whose name holds the percent sign */
func refusePercentEscapedPath(firewallName string, field string, declaredPath string) error {
    if false == internal.CarriesPercentEscapeOtherThanSeparator(declaredPath) {
        return nil
    }

    context := exceptioncontract.Context{
        "field": field,
        "entry": declaredPath,
    }
    if "" != firewallName {
        context["firewallName"] = firewallName
    }

    return exception.NewError(
        "security declared path carries a percent escape: request paths are matched decoded, with %2F for a separator inside a segment, so the path claims only a resource whose name holds the percent sign literally; write it decoded",
        context,
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
