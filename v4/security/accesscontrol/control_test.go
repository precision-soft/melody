package accesscontrol

import (
    "strings"
    "testing"
)

/* MatchRule answers the rule and the index MatchRuleIndex resolves, the exact rule winning over the prefix that also covers the path, and nothing with the index -1 for a path no rule claims. */
func TestControl_MatchRuleAnswersTheRuleMatchRuleIndexResolves(t *testing.T) {
    control := NewControl([]Rule{
        NewSegmentPrefixRule("/admin", RuleConfig{Attributes: []string{"ROLE_ADMIN"}}),
        NewExactRule("/admin/settings", RuleConfig{Attributes: []string{"ROLE_OWNER"}}),
    })

    rule, index, matched := control.MatchRule("/admin/settings")
    if false == matched {
        t.Fatal("expected the exact rule to claim the path")
    }

    expectedIndex, _ := control.MatchRuleIndex("/admin/settings")
    if expectedIndex != index || 1 != index {
        t.Fatalf("expected the index 1 MatchRuleIndex resolves, got %d against %d", index, expectedIndex)
    }

    if "/admin/settings" != rule.PathPrefix() {
        t.Fatalf("expected the exact rule, got %q", rule.PathPrefix())
    }

    attributes := rule.Attributes()
    if 1 != len(attributes) || "ROLE_OWNER" != attributes[0] {
        t.Fatalf("expected the exact rule's attributes, got %v", attributes)
    }

    prefixRule, prefixIndex, prefixMatched := control.MatchRule("/admin/users")
    if false == prefixMatched || 0 != prefixIndex || "/admin" != prefixRule.PathPrefix() {
        t.Fatalf("expected the prefix rule at index 0, got %q at %d (%t)", prefixRule.PathPrefix(), prefixIndex, prefixMatched)
    }

    _, unmatchedIndex, unmatched := control.MatchRule("/elsewhere")
    if true == unmatched || -1 != unmatchedIndex {
        t.Fatalf("expected no rule to claim an unclaimed path, got index %d (%t)", unmatchedIndex, unmatched)
    }
}

func TestNewControl_RefusesARegexRuleBesideARootPrefixRule(t *testing.T) {
    regexRule := NewRegexRule("^/admin", RuleConfig{Attributes: []string{"ROLE_ADMIN"}})

    for name, rootRule := range map[string]Rule{
        "segment prefix": NewSegmentPrefixRule("/", RuleConfig{Attributes: []string{"ROLE_USER"}}),
        "raw prefix":     NewRawPrefixRule("/", RuleConfig{Attributes: []string{"ROLE_USER"}}),
    } {
        for order, rules := range map[string][]Rule{
            "prefix first": {rootRule, regexRule},
            "regex first":  {regexRule, rootRule},
        } {
            t.Run(name+", "+order, func(t *testing.T) {
                defer func() {
                    recovered := recover()
                    if nil == recovered {
                        t.Fatalf("expected the regex rule beside the root prefix to be refused")
                    }

                    recoveredErr, isError := recovered.(error)
                    if false == isError || false == strings.Contains(recoveredErr.Error(), "never decides") {
                        t.Fatalf("expected the refusal to say the regex never decides, got %v", recovered)
                    }
                }()

                _ = NewControl(rules)
            })
        }
    }
}

func TestNewControl_AdmitsARegexRuleBesideTheOtherCatchAlls(t *testing.T) {
    regexRule := NewRegexRule("^/admin", RuleConfig{Attributes: []string{"ROLE_ADMIN"}})

    for name, neighbour := range map[string]Rule{
        "empty raw prefix": NewRawPrefixRule("", RuleConfig{Attributes: []string{"ROLE_USER"}}),
        "exact root":       NewExactRule("/", RuleConfig{Attributes: []string{"ROLE_USER"}}),
        "deeper prefix":    NewSegmentPrefixRule("/api", RuleConfig{Attributes: []string{"ROLE_USER"}}),
    } {
        t.Run(name, func(t *testing.T) {
            control := NewControl([]Rule{neighbour, regexRule})

            attributes, matched := control.Match("/admin/users")
            if false == matched || 1 != len(attributes) || "ROLE_ADMIN" != attributes[0] {
                t.Fatalf("expected the regex rule to decide /admin/users, got %v %v", attributes, matched)
            }
        })
    }
}
