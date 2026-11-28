package security

import (
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
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

func (instance *RoleHierarchyVoter) Vote(runtimeInstance containercontract.Runtime, token securitycontract.Token, attribute string, subject any) (securitycontract.VoteResult, error) {
    /* IsNilInterface: a typed nil token of the application's type answers IsAuthenticated true without its receiver, and Roles() below would dereference it */
    if true == internal.IsNilInterface(token) {
        return securitycontract.VoteDenied, nil
    }

    if false == token.IsAuthenticated() {
        return securitycontract.VoteDenied, nil
    }

    expandedRoles := instance.roleHierarchy.ExpandRoles(token.Roles())

    return instance.delegate.Vote(runtimeInstance, NewAuthenticatedToken(token.UserIdentifier(), expandedRoles, nil), attribute, subject)
}

var _ securitycontract.Voter = (*RoleHierarchyVoter)(nil)
