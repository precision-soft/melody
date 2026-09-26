package security

import (
    "github.com/precision-soft/melody/v2/exception"
    "github.com/precision-soft/melody/v2/internal"
    securitycontract "github.com/precision-soft/melody/v2/security/contract"
)

/* NewRoleHierarchyVoter takes any Voter as its delegate, since the wrapper calls only Supports and Vote, so an integrator's own voter sees the expanded roles too. */
func NewRoleHierarchyVoter(roleHierarchy *RoleHierarchy, delegate securitycontract.Voter) *RoleHierarchyVoter {
    if nil == roleHierarchy {
        exception.Panic(
            exception.NewError("the role hierarchy is nil for role hierarchy voter", nil, nil),
        )
    }

    /* IsNilInterface: a typed-nil delegate reads as non-nil and would dereference its nil receiver on the first vote */
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
    /* IsNilInterface: a typed nil token of the application's type answers IsAuthenticated true without its receiver, and Roles() below would dereference it */
    if true == internal.IsNilInterface(token) {
        return securitycontract.VoteDenied
    }

    if false == token.IsAuthenticated() {
        return securitycontract.VoteDenied
    }

    expandedRoles := instance.roleHierarchy.ExpandRoles(token.Roles())

    return instance.delegate.Vote(expandedRolesToken(token, expandedRoles), attribute, subject)
}

/* RolesReplacer is the optional capability a token implements to answer its own twin under a different role set. A delegate voter of the application's own may assert the concrete token to learn which tenant or owner a request speaks for, and a token rebuilt as melody's own would make it abstain where it would refuse. It is optional because Token is a published contract of a stable major; a token without it is rebuilt. */
type RolesReplacer interface {
    WithRoles(roles []string) securitycontract.Token
}

/* expandedRolesToken answers the token the delegate votes on: the original's own twin when it can make one, and melody's rebuild otherwise. A nil twin counts as no answer, since a delegate handed nil would deny every request the hierarchy widens. */
func expandedRolesToken(token securitycontract.Token, expandedRoles []string) securitycontract.Token {
    replacer, isReplacer := token.(RolesReplacer)
    if true == isReplacer && false == internal.IsNilInterface(replacer) {
        twin := replacer.WithRoles(expandedRoles)
        if false == internal.IsNilInterface(twin) {
            return twin
        }
    }

    return NewAuthenticatedToken(token.UserIdentifier(), expandedRoles)
}

var _ securitycontract.Voter = (*RoleHierarchyVoter)(nil)
