package validation

import (
    "testing"
)

func TestParseValidationTagParenthesizedRegexWithCommaGroup(t *testing.T) {
    pattern := `^(\d{1,3},){3}\d{1,3}$`
    tag := `regex(value=` + pattern + `)`

    rules, err := parseValidationTag(tag)
    if nil != err {
        t.Fatalf("expected no error, got %v", err)
    }

    if 1 != len(rules) {
        t.Fatalf("expected exactly one rule, got %d: %#v", len(rules), rules)
    }

    if "regex" != rules[0].name {
        t.Fatalf("expected rule name %q, got %q", "regex", rules[0].name)
    }

    if pattern != rules[0].params["value"] {
        t.Fatalf("expected value %q, got %q", pattern, rules[0].params["value"])
    }
}
