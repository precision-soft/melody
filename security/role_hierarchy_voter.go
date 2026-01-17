package security

import (
	"github.com/precision-soft/melody/exception"
	securitycontract "github.com/precision-soft/melody/security/contract"
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
	if nil == token {
		return securitycontract.VoteDenied
	}

	expandedRoles := instance.roleHierarchy.ExpandRoles(token.Roles())

	expandedToken := NewAuthenticatedToken(token.UserIdentifier(), expandedRoles)
	if false == token.IsAuthenticated() {
		expandedToken = NewAuthenticatedToken("", expandedRoles)
	}

	return instance.delegate.Vote(expandedToken, attribute, subject)
}

var _ securitycontract.Voter = (*RoleHierarchyVoter)(nil)
