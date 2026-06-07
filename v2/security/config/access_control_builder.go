package config

import (
    "github.com/precision-soft/melody/v2/security"
    securitycontract "github.com/precision-soft/melody/v2/security/contract"
)

func NewAccessControlBuilder() *AccessControlBuilder {
    return &AccessControlBuilder{rules: make([]security.AccessControlRule, 0)}
}

type AccessControlBuilder struct {
    rules []security.AccessControlRule
}

func (instance *AccessControlBuilder) Require(pathPrefix string, attributes ...string) *AccessControlBuilder {
    instance.rules = append(instance.rules, security.NewAccessControlRule(pathPrefix, attributes...))
    return instance
}

func (instance *AccessControlBuilder) AllowAnonymous(pathPrefix string) *AccessControlBuilder {
    /** The rule must carry the public-access attribute so the access-control listener grants the request without a token; an empty attribute set would instead fall through to the require-authentication path and deny anonymous users. */
    instance.rules = append(instance.rules, security.NewAccessControlRule(pathPrefix, securitycontract.AttributePublicAccess))
    return instance
}

func (instance *AccessControlBuilder) Build() *security.AccessControl {
    return security.NewAccessControl(instance.rules...)
}
