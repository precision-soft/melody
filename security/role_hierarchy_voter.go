package security

import (
    "github.com/precision-soft/melody/exception"
    "github.com/precision-soft/melody/internal"
    securitycontract "github.com/precision-soft/melody/security/contract"
)

/* NewRoleHierarchyVoter takes any Voter as its delegate, not only the built-in RoleVoter: the wrapper calls nothing but Supports and Vote, so the narrower parameter bought nothing and cost an integrator's own voter — multi-tenant, ownership — the ability to see the expanded roles at all. The only way out was copying this file, which meant every foreign voter reimplementing the expansion rule. */
func NewRoleHierarchyVoter(roleHierarchy *RoleHierarchy, delegate securitycontract.Voter) *RoleHierarchyVoter {
    if nil == roleHierarchy {
        exception.Panic(
            exception.NewError("the role hierarchy is nil for role hierarchy voter", nil, nil),
        )
    }

    /* the typed-nil check is what the plain comparison cannot do now that the parameter is an interface: a (*tenantVoter)(nil) passed here reads as non-nil and dereferences its nil receiver on the first vote of the request path, which is the wrong place to learn about a wiring fault */
    if true == internal.IsNilInterface(delegate) {
        exception.Panic(
            exception.NewError("the delegate is nil for role hierarchy voter", nil, nil),
        )
    }

    return &RoleHierarchyVoter{
        roleHierarchy: roleHierarchy,
        delegate:      delegate,
    }
}

type RoleHierarchyVoter struct {
    roleHierarchy *RoleHierarchy
    delegate      securitycontract.Voter
}

func (instance *RoleHierarchyVoter) Supports(attribute string, subject any) bool {
    return instance.delegate.Supports(attribute, subject)
}

func (instance *RoleHierarchyVoter) Vote(token securitycontract.Token, attribute string, subject any) securitycontract.VoteResult {
    if nil == token {
        return securitycontract.VoteDenied
    }

    if false == token.IsAuthenticated() {
        return securitycontract.VoteDenied
    }

    expandedRoles := instance.roleHierarchy.ExpandRoles(token.Roles())

    expandedToken := NewAuthenticatedToken(token.UserIdentifier(), expandedRoles)

    return instance.delegate.Vote(expandedToken, attribute, subject)
}

var _ securitycontract.Voter = (*RoleHierarchyVoter)(nil)
