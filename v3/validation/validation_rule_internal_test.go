package validation

import (
    "testing"
)

func TestSplitByTopLevelComma_LiteralCurlyDoesNotSwallowFollowingRule(t *testing.T) {
    parts := splitByTopLevelComma("regex=a{,min=5")
    if 2 != len(parts) {
        t.Fatalf("expected 2 parts (a literal '{' must not swallow the following rule), got %d: %#v", len(parts), parts)
    }
    if "regex=a{" != parts[0] || "min=5" != parts[1] {
        t.Fatalf("expected [regex=a{ min=5], got %#v", parts)
    }
}

func TestSplitByTopLevelComma_BalancedQuantifierCommaPreserved(t *testing.T) {
    parts := splitByTopLevelComma("regex=a{2,5},min=5")
    if 2 != len(parts) {
        t.Fatalf("expected 2 parts with the quantifier comma protected, got %d: %#v", len(parts), parts)
    }
    if "regex=a{2,5}" != parts[0] || "min=5" != parts[1] {
        t.Fatalf("expected [regex=a{2,5} min=5], got %#v", parts)
    }
}

func TestParseValidationTag_LiteralCurlyKeepsFollowingRule(t *testing.T) {
    rules, err := parseValidationTag("regex=a{,min=5")
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    if 2 != len(rules) {
        t.Fatalf("expected the regex and min rules, got %d: %#v", len(rules), rules)
    }
    if "regex" != rules[0].name || "min" != rules[1].name {
        t.Fatalf("expected rules [regex min], got [%s %s]", rules[0].name, rules[1].name)
    }
}
