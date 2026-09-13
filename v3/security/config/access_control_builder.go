package config

import (
    "github.com/precision-soft/melody/v3/security"
    "github.com/precision-soft/melody/v3/security/accesscontrol"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

func NewAccessControlBuilder() *AccessControlBuilder {
    return &AccessControlBuilder{rules: make([]security.AccessControlRule, 0)}
}

type AccessControlBuilder struct {
    rules []security.AccessControlRule
}

/* Require declares the attributes a path and everything beginning with it must satisfy. The reach is the raw prefix, so "/admin" also governs "/administrator" — a builder method takes one path and cannot name its own mode, and this is the reach it has always had. A rule that must stop at the segment boundary is declared with accesscontrol.NewSegmentPrefixRule and handed to NewAccessControl directly. */
func (instance *AccessControlBuilder) Require(pathPrefix string, attributes ...string) *AccessControlBuilder {
    instance.rules = append(
        instance.rules,
        accesscontrol.NewRawPrefixRule(pathPrefix, accesscontrol.RuleConfig{Attributes: attributes}),
    )
    return instance
}

/* AllowAnonymous opens a path and its descendants to an unauthenticated caller. The reach is bounded to the segment, unlike Require's: a public rule is the longest match wherever it reaches, so a raw one would open every path merely beginning with the spelling and shadow a denial that would have refused. */
func (instance *AccessControlBuilder) AllowAnonymous(pathPrefix string) *AccessControlBuilder {
    instance.rules = append(
        instance.rules,
        accesscontrol.NewSegmentPrefixRule(pathPrefix, accesscontrol.RuleConfig{
            Attributes: []string{securitycontract.AttributePublicAccess},
        }),
    )
    return instance
}

func (instance *AccessControlBuilder) Build() *security.AccessControl {
    return security.NewAccessControl(instance.rules...)
}
