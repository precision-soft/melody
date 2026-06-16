package security

import (
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
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

func (instance *AnonymousToken) Scope() map[string]any {
    return map[string]any{}
}

func (instance *AnonymousToken) Attributes() map[string]any {
    return map[string]any{}
}

var _ securitycontract.Token = (*AnonymousToken)(nil)
