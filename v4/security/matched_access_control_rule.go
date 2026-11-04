package security

import (
    "github.com/precision-soft/melody/v4/security/accesscontrol"
)

func NewMatchedAccessControlRule(
    rule accesscontrol.Rule,
    source Source,
    ruleIndex int,
    firewall string,
) *MatchedAccessControlRule {
    return &MatchedAccessControlRule{
        rule:      rule,
        source:    source,
        ruleIndex: ruleIndex,
        firewall:  firewall,
    }
}

type MatchedAccessControlRule struct {
    rule      accesscontrol.Rule
    source    Source
    ruleIndex int
    firewall  string
}

/* Rule answers the rule that decided the request as the control compiled it, its path folded and its attributes trimmed. */
func (instance *MatchedAccessControlRule) Rule() accesscontrol.Rule {
    return instance.rule
}

/* PathPrefix answers the path of an exact or prefix rule, and the empty string for a regex rule, which Pattern names instead. */
func (instance *MatchedAccessControlRule) PathPrefix() string {
    return instance.rule.PathPrefix()
}

/* Pattern answers the regex of a regex rule, and the empty string for every other mode. */
func (instance *MatchedAccessControlRule) Pattern() string {
    return instance.rule.Pattern()
}

/* Matching answers the mode of the rule, which tells an exact rule from a segment or raw prefix over the same path. */
func (instance *MatchedAccessControlRule) Matching() accesscontrol.Matching {
    return instance.rule.Matching()
}

/* Attributes answers a copy of the attributes a matching request must satisfy. */
func (instance *MatchedAccessControlRule) Attributes() []string {
    return instance.rule.Attributes()
}

func (instance *MatchedAccessControlRule) Source() Source {
    return instance.source
}

func (instance *MatchedAccessControlRule) RuleIndex() int {
    return instance.ruleIndex
}

func (instance *MatchedAccessControlRule) Firewall() string {
    return instance.firewall
}
