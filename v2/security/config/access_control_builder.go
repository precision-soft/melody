package config

import (
    "github.com/precision-soft/melody/v2/security"
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
    instance.rules = append(instance.rules, security.NewAccessControlRule(pathPrefix))
    return instance
}

func (instance *AccessControlBuilder) Build() *security.AccessControl {
    return security.NewAccessControl(instance.rules...)
}
