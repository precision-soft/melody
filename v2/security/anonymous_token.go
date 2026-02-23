package security

import (
    securitycontract "github.com/precision-soft/melody/v2/security/contract"
)

func NewAnonymousToken() *AnonymousToken {
    return &AnonymousToken{}
}

type AnonymousToken struct {
}

func (instance *AnonymousToken) IsAuthenticated() bool {
    return false
}

func (instance *AnonymousToken) UserIdentifier() string {
    return ""
}

func (instance *AnonymousToken) Roles() []string {
    return []string{}
}

var _ securitycontract.Token = (*AnonymousToken)(nil)
