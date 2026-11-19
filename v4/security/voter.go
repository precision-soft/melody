package security

import (
    "github.com/precision-soft/melody/v4/internal"

    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

func NewRoleVoter() *RoleVoter {
    return &RoleVoter{}
}

type RoleVoter struct {
}

func (instance *RoleVoter) Supports(attribute string, subject any) bool {
    return true
}

func (instance *RoleVoter) Vote(runtimeInstance runtimecontract.Runtime, token securitycontract.Token, attribute string, subject any) (securitycontract.VoteResult, error) {
    if "" == attribute {
        return securitycontract.VoteAbstain, nil
    }

    /* IsNilInterface: a typed nil token of the application's type answers IsAuthenticated true without its receiver, and Roles() below would dereference it */
    if true == internal.IsNilInterface(token) {
        return securitycontract.VoteDenied, nil
    }

    if false == token.IsAuthenticated() {
        return securitycontract.VoteDenied, nil
    }

    for _, role := range token.Roles() {
        if role == attribute {
            return securitycontract.VoteGranted, nil
        }
    }

    return securitycontract.VoteDenied, nil
}

var _ securitycontract.Voter = (*RoleVoter)(nil)
