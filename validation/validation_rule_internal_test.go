package validation

import (
    "testing"
)

func TestParseValidationTag_ShorthandRegexWithGroupParses(t *testing.T) {
    rules, err := parseValidationTag("regex=^(a|b)$")
    if nil != err {
        t.Fatalf("shorthand regex with a capture/alternation group must parse, got: %v", err)
    }
    if 1 != len(rules) {
        t.Fatalf("expected a single regex rule, got %d: %#v", len(rules), rules)
    }
    if "regex" != rules[0].name || "^(a|b)$" != rules[0].params["value"] {
        t.Fatalf("expected regex rule with pattern '^(a|b)$' under value, got %#v", rules[0])
    }
}

func TestParseValidationTag_ParenthesizedRegexWithGroupParses(t *testing.T) {
    rules, err := parseValidationTag("regex(pattern=^(a|b)$)")
    if nil != err {
        t.Fatalf("parenthesized regex with a group must parse identically to the shorthand, got: %v", err)
    }
    if 1 != len(rules) {
        t.Fatalf("expected a single regex rule, got %d: %#v", len(rules), rules)
    }
    if "regex" != rules[0].name || "^(a|b)$" != rules[0].params["pattern"] {
        t.Fatalf("expected regex rule with pattern '^(a|b)$' under pattern, got %#v", rules[0])
    }
}

func TestParseValidationTag_UnbalancedParensStillRejected(t *testing.T) {
    if _, err := parseValidationTag("min(value=3"); nil == err {
        t.Fatal("expected an unterminated parenthesized rule to be rejected")
    }
    if _, err := parseValidationTag("min(value=3))"); nil == err {
        t.Fatal("expected a rule with an unbalanced trailing paren to be rejected")
    }
}
