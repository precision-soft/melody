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

func TestSplitByTopLevelComma_CharClassClosingBracketStaysOneRule(t *testing.T) {
    parts := splitByTopLevelComma("regex=^[)]a{2,3}b$")
    if 1 != len(parts) {
        t.Fatalf("a ')' inside a character class must not disable comma protection, got %d parts: %#v", len(parts), parts)
    }
    if "regex=^[)]a{2,3}b$" != parts[0] {
        t.Fatalf("expected the regex value intact, got %#v", parts)
    }
}

func TestSplitByTopLevelComma_CharClassWithLiteralBraceDoesNotSwallowFollowingRule(t *testing.T) {
    parts := splitByTopLevelComma("regex=^[{]$,min=3")
    if 2 != len(parts) {
        t.Fatalf("a literal '{' inside a character class must not protect the following rule separator, got %d parts: %#v", len(parts), parts)
    }
    if "regex=^[{]$" != parts[0] || "min=3" != parts[1] {
        t.Fatalf("expected [regex=^[{]$ min=3], got %#v", parts)
    }
}

func TestSplitByTopLevelComma_CharClassWithLiteralCommaPreserved(t *testing.T) {
    parts := splitByTopLevelComma("regex=^[,]$,min=3")
    if 2 != len(parts) {
        t.Fatalf("a literal ',' inside a character class must be protected, got %d parts: %#v", len(parts), parts)
    }
    if "regex=^[,]$" != parts[0] || "min=3" != parts[1] {
        t.Fatalf("expected [regex=^[,]$ min=3], got %#v", parts)
    }
}

func TestParseValidationTag_ShorthandRegexCharClassWithClosingBracketParses(t *testing.T) {
    rules, err := parseValidationTag("regex=^[)]a{2,3}b$")
    if nil != err {
        t.Fatalf("shorthand regex with ')' inside a character class must parse, got: %v", err)
    }
    if 1 != len(rules) || "regex" != rules[0].name || "^[)]a{2,3}b$" != rules[0].params["value"] {
        t.Fatalf("expected a single regex rule with the pattern intact, got %#v", rules)
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

func TestParseValidationTag_RegexCharClassOfAllBracketsParses(t *testing.T) {
    rules, err := parseValidationTag("regex=^[(){}]+$")
    if nil != err {
        t.Fatalf("a character class containing every bracket must parse, got: %v", err)
    }
    if 1 != len(rules) || "^[(){}]+$" != rules[0].params["value"] {
        t.Fatalf("expected the bracket character class intact, got %#v", rules)
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

func TestParseValidationTag_UnbalancedParensStillRejected(t *testing.T) {
    if _, err := parseValidationTag("min(value=3"); nil == err {
        t.Fatal("expected an unterminated parenthesized rule to be rejected")
    }
    if _, err := parseValidationTag("min(value=3))"); nil == err {
        t.Fatal("expected a rule with an unbalanced trailing paren to be rejected")
    }
}
