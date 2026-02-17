package security

import (
	securitycontract "github.com/precision-soft/melody/v2/security/contract"
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

	if nil == token {
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
