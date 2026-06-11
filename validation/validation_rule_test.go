package validation

import (
    "testing"
)

func TestSplitByCommaOutsideRegexMeta_QuoteInsideCharClassDoesNotSwallowComma(t *testing.T) {
    parts := splitByCommaOutsideRegexMeta(`value=^[a'z]$,other=x`)
    if 2 != len(parts) {
        t.Fatalf("expected 2 parts (a literal quote inside a regex char class must not toggle quote state and swallow the comma), got %d: %#v", len(parts), parts)
    }
    if `value=^[a'z]$` != parts[0] || "other=x" != parts[1] {
        t.Fatalf("expected [value=^[a'z]$ other=x], got %#v", parts)
    }
}

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

func TestParseValidationTag_ParenthesizedRegexCharClassWithClosingBracketParses(t *testing.T) {
    rules, err := parseValidationTag("regex(value=^[)]$)")
    if nil != err {
        t.Fatalf("parenthesized regex with ')' inside a character class must parse, got: %v", err)
    }
    if 1 != len(rules) || "regex" != rules[0].name || "^[)]$" != rules[0].params["value"] {
        t.Fatalf("expected a single regex rule with pattern '^[)]$', got %#v", rules)
    }
}

func TestHasBalancedBrackets_CharClassMembersAreLiteral(t *testing.T) {
    balanced := []string{"^[)]$", "^[}]$", "^[(){}]+$", "[]]", "[^]]", "a{2,3}[xyz]"}
    for _, value := range balanced {
        if false == hasBalancedBrackets(value) {
            t.Fatalf("expected %q to be reported as balanced", value)
        }
    }

    unbalanced := []string{"^[a", "a{2", "(a", "a)", "]a"}
    for _, value := range unbalanced {
        if true == hasBalancedBrackets(value) {
            t.Fatalf("expected %q to be reported as unbalanced", value)
        }
    }
}

/** @info parenthesized regex with comma group */

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
