package security

import (
    "github.com/precision-soft/melody/v3/security/accesscontrol"
)

/* AccessControlRule is an alias for accesscontrol.Rule, which owns the type since the access control vocabulary moved to its own package. Existing code that names security.AccessControlRule keeps compiling and keeps holding the same type. */
type AccessControlRule = accesscontrol.Rule

/* AccessControl is an alias for accesscontrol.Control. */
type AccessControl = accesscontrol.Control

/* NewAccessControl collects the rules a request is resolved against. */
func NewAccessControl(rules ...AccessControlRule) *AccessControl {
    return accesscontrol.NewControl(rules...)
}

/* Deprecated: use github.com/precision-soft/melody/v3/security/accesscontrol.NewRawPrefixRule instead, which names the reach in the constructor and takes its attributes in a config struct. This constructor is removed in v4. */
func NewAccessControlRule(pathPrefix string, attributes ...string) AccessControlRule {
    return accesscontrol.NewRawPrefixRule(pathPrefix, accesscontrol.RuleConfig{Attributes: attributes})
}

/* Deprecated: use github.com/precision-soft/melody/v3/security/accesscontrol.NewSegmentPrefixRule instead. This constructor is removed in v4. */
func NewAccessControlRuleWithSegmentPrefix(pathPrefix string, attributes ...string) AccessControlRule {
    return accesscontrol.NewSegmentPrefixRule(pathPrefix, accesscontrol.RuleConfig{Attributes: attributes})
}

/* Deprecated: use github.com/precision-soft/melody/v3/security/accesscontrol.NewExactRule instead. This constructor is removed in v4. */
func NewAccessControlExactRule(path string, attributes ...string) AccessControlRule {
    return accesscontrol.NewExactRule(path, accesscontrol.RuleConfig{Attributes: attributes})
}

/* Deprecated: use github.com/precision-soft/melody/v3/security/accesscontrol.NewRegexRule instead. This constructor is removed in v4. */
func NewAccessControlRegexRule(pattern string, attributes ...string) AccessControlRule {
    return accesscontrol.NewRegexRule(pattern, accesscontrol.RuleConfig{Attributes: attributes})
}
