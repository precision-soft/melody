package validation

import (
    "testing"
)

func TestParseValidationTag_ParenthesizedRegexWithCommaInsideGroup(t *testing.T) {
    pattern := `^(\d{1,3},){3}\d{1,3}$`

    parenRules, parenErr := parseValidationTag(`regex(value=` + pattern + `)`)
    if nil != parenErr {
        t.Fatalf("parenthesized regex with a comma inside a () group must parse like the shorthand, got error: %v", parenErr)
    }
    if 1 != len(parenRules) {
        t.Fatalf("expected exactly one rule, got %d: %#v", len(parenRules), parenRules)
    }
    if "regex" != parenRules[0].name {
        t.Fatalf("expected rule name regex, got %q", parenRules[0].name)
    }
    if pattern != parenRules[0].params["value"] {
        t.Fatalf("expected the comma inside the () group to be preserved as part of the value %q, got %q", pattern, parenRules[0].params["value"])
    }

    shorthandRules, shorthandErr := parseValidationTag(`regex=` + pattern)
    if nil != shorthandErr {
        t.Fatalf("shorthand regex must parse: %v", shorthandErr)
    }
    if shorthandRules[0].params["value"] != parenRules[0].params["value"] {
        t.Fatalf("shorthand and parenthesized forms must agree on the value: %q vs %q", shorthandRules[0].params["value"], parenRules[0].params["value"])
    }
}
