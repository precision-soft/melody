package security

import (
    "github.com/precision-soft/melody/v2/exception"
    "github.com/precision-soft/melody/v2/internal"
    securitycontract "github.com/precision-soft/melody/v2/security/contract"
)

type Source string

const (
    SourceNone     Source = "none"
    SourceGlobal   Source = "global"
    SourceFirewall Source = "firewall"
    SourceMerged   Source = "merged"
)

func NewSecurityContext(
    firewall *CompiledFirewall,
    token securitycontract.Token,
) *SecurityContext {
    if true == internal.IsNilInterface(firewall) {
        exception.Panic(
            exception.NewError("the firewall is required for the security context", nil, nil),
        )
    }

    roleHierarchySource, accessDecisionManagerSource, accessControlSource, entryPointSource, accessDeniedHandlerSource := firewall.Sources()

    return &SecurityContext{
        firewall:                    firewall,
        token:                       token,
        roleHierarchySource:         roleHierarchySource,
        accessDecisionManagerSource: accessDecisionManagerSource,
        accessControlSource:         accessControlSource,
        entryPointSource:            entryPointSource,
        accessDeniedHandlerSource:   accessDeniedHandlerSource,
        matchedFirewallMatcher:      firewall.MatcherDescription(),
    }
}

type SecurityContext struct {
    firewall                    *CompiledFirewall
    token                       securitycontract.Token
    matchedRule                 *MatchedAccessControlRule
    roleHierarchySource         Source
    accessDecisionManagerSource Source
    accessControlSource         Source
    entryPointSource            Source
    accessDeniedHandlerSource   Source
    matchedFirewallMatcher      string
}

func (instance *SecurityContext) Firewall() *CompiledFirewall {
    return instance.firewall
}

func (instance *SecurityContext) Token() securitycontract.Token {
    return instance.token
}

func (instance *SecurityContext) MatchedRule() *MatchedAccessControlRule {
    return instance.matchedRule
}

func (instance *SecurityContext) SetMatchedRule(matchedRule *MatchedAccessControlRule) {
    instance.matchedRule = matchedRule
}

func (instance *SecurityContext) RoleHierarchySource() Source {
    return instance.roleHierarchySource
}

func (instance *SecurityContext) AccessDecisionManagerSource() Source {
    return instance.accessDecisionManagerSource
}

func (instance *SecurityContext) AccessControlSource() Source {
    return instance.accessControlSource
}

func (instance *SecurityContext) EntryPointSource() Source {
    return instance.entryPointSource
}

func (instance *SecurityContext) AccessDeniedHandlerSource() Source {
    return instance.accessDeniedHandlerSource
}

func (instance *SecurityContext) MatchedFirewallMatcher() string {
    return instance.matchedFirewallMatcher
}

func (instance *SecurityContext) IsGranted(role string) bool {
    token := instance.Token()
    if nil == token {
        return false
    }

    compiledFirewall := instance.Firewall()
    if nil == compiledFirewall {
        return hasRole(token.Roles(), role)
    }

    roleHierarchy := compiledFirewall.RoleHierarchy()
    if nil == roleHierarchy {
        return hasRole(token.Roles(), role)
    }

    effectiveRoles := roleHierarchy.ExpandRoles(token.Roles())

    return hasRole(effectiveRoles, role)
}

func hasRole(roles []string, role string) bool {
    if "" == role {
        return false
    }

    for _, currentRole := range roles {
        if role == currentRole {
            return true
        }
    }

    return false
}
