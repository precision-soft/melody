package security

import (
    "github.com/precision-soft/melody/v2/exception"
    "github.com/precision-soft/melody/v2/internal"
    securitycontract "github.com/precision-soft/melody/v2/security/contract"
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

    return instance.delegate.Vote(expandedRolesToken(token, expandedRoles), attribute, subject)
}

/* RolesReplacer is the optional capability a token implements to answer its own twin under a different role set. It exists because the delegate of a hierarchy voter is any Voter, and a voter of the application's own reads more than Roles(): it asserts the concrete token, or an interface of its own on it, to learn WHICH tenant or which owner the request speaks for. A token rebuilt as one of melody's own answers that assertion with a type the voter has never seen, so the voter abstains — and a voter that would have REFUSED, abstaining under the affirmative strategy beside a role voter that grants, hands out the access it exists to withhold. A token that answers its own twin keeps its dynamic type through the expansion and the delegate is none the wiser.

The capability is optional because Token is a published contract of a stable major and cannot grow a method. A token that does not carry it is rebuilt as before, which is what it already got. */
type RolesReplacer interface {
    WithRoles(roles []string) securitycontract.Token
}

/* expandedRolesToken answers the token the delegate votes on: the original's own twin when it can make one, and melody's rebuild when it cannot. A twin answered as nil is treated as no answer at all, because a delegate handed nil would deny every request the hierarchy was meant to widen. */
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
