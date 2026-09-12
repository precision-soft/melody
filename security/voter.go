package security

import (
    "github.com/precision-soft/melody/internal"

    securitycontract "github.com/precision-soft/melody/security/contract"
)

func NewRoleVoter() *RoleVoter {
    return &RoleVoter{}
}

type RoleVoter struct {
}

func (instance *RoleVoter) Supports(attribute string, subject any) bool {
    return true
}

func (instance *RoleVoter) Vote(token securitycontract.Token, attribute string, subject any) securitycontract.VoteResult {
    if "" == attribute {
        return securitycontract.VoteAbstain
    }

    /* the token comes from the application's token source, and a nil pointer of its own token type
    reaches here as a non-nil interface: a bare comparison takes it for a live token, IsAuthenticated
    answers true without touching the receiver, and the Roles() call below dereferences the nil */
    if true == internal.IsNilInterface(token) {
        return securitycontract.VoteDenied
    }

    if false == token.IsAuthenticated() {
        return securitycontract.VoteDenied
    }

    for _, role := range token.Roles() {
        if role == attribute {
            return securitycontract.VoteGranted
        }
    }

    return securitycontract.VoteDenied
}

var _ securitycontract.Voter = (*RoleVoter)(nil)
