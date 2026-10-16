package security

import (
    "github.com/precision-soft/melody/v3/internal"
    "github.com/precision-soft/melody/v3/exception"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

func NewRoleHierarchyVoter(roleHierarchy *RoleHierarchy, delegate *RoleVoter) *RoleHierarchyVoter {
    if nil == roleHierarchy {
        exception.Panic(
            exception.NewError("the role hierarchy is nil for role hierarchy voter", nil, nil),
        )
    }

    if nil == delegate {
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
    delegate      *RoleVoter
}

func (instance *RoleHierarchyVoter) Supports(attribute string, subject any) bool {
    return instance.delegate.Supports(attribute, subject)
}

func (instance *RoleHierarchyVoter) Vote(token securitycontract.Token, attribute string, subject any) securitycontract.VoteResult {
    /* IsNilInterface: a typed nil token of the application's type answers IsAuthenticated true without its receiver, and Roles() below would dereference it */
    if true == internal.IsNilInterface(token) {
        return securitycontract.VoteDenied
    }

    if false == token.IsAuthenticated() {
        return securitycontract.VoteDenied
    }

    expandedRoles := instance.roleHierarchy.ExpandRoles(token.Roles())

    return instance.delegate.Vote(NewAuthenticatedToken(token.UserIdentifier(), expandedRoles), attribute, subject)
}

var _ securitycontract.Voter = (*RoleHierarchyVoter)(nil)
