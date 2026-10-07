package accesscontrol

import (
    "testing"
)

/* MatchRule answers the rule and the index MatchRuleIndex resolves, the exact rule winning over the prefix that also covers the path, and nothing with the index -1 for a path no rule claims. */
func TestControl_MatchRuleAnswersTheRuleMatchRuleIndexResolves(t *testing.T) {
    control := NewControl(
        NewSegmentPrefixRule("/admin", RuleConfig{Attributes: []string{"ROLE_ADMIN"}}),
        NewExactRule("/admin/settings", RuleConfig{Attributes: []string{"ROLE_OWNER"}}),
    )

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
