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
    balanced := []string{"^[)]$", "^[}]$", "^[(){}]+$", "[]]", "[^]]", "a{2,3}[xyz]", "]a", "^a]b$"}
    for _, value := range balanced {
        if false == hasBalancedBrackets(value) {
            t.Fatalf("expected %q to be reported as balanced", value)
        }
    }

    /* a ']' with no open class is a literal in RE2 ("]a" matches "]a"), so it is not a syntax error; the genuinely unbalanced forms are the openers left open and the closers with no opener */
    unbalanced := []string{"^[a", "a{2", "(a", "a)"}
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

/* a ']' outside a character class is a literal in RE2, so a pattern carrying one is valid. */
func TestParseValidationTag_RegexWithLiteralClosingBracketIsAccepted(t *testing.T) {
    if _, err := parseValidationTag(`regex(pattern=^a]b$)`); nil != err {
        t.Fatalf("expected a regex with a literal ']' to parse, got: %v", err)
    }
}

/* a POSIX named class ([[:alpha:]]) carries its own ']' inside the bracket expression, and that inner ']' is not the class close. */
func TestSplitByTopLevelComma_PosixNamedClassKeepsInClassComma(t *testing.T) {
    parts := splitByTopLevelComma("regex=[[:alpha:],]")
    if 1 != len(parts) {
        t.Fatalf("a comma inside a class that carries a POSIX named element must stay protected, got %d parts: %#v", len(parts), parts)
    }
    if "regex=[[:alpha:],]" != parts[0] {
        t.Fatalf("expected the regex value intact, got %#v", parts)
    }
}

func TestParseValidationTag_PosixNamedClassRegexParses(t *testing.T) {
    rules, err := parseValidationTag("regex=[[:alpha:],]")
    if nil != err {
        t.Fatalf("a regex with a POSIX named class must parse, got: %v", err)
    }
    if 1 != len(rules) || "regex" != rules[0].name || "[[:alpha:],]" != rules[0].params["value"] {
        t.Fatalf("expected a single regex rule with the POSIX pattern intact, got %#v", rules)
    }
}

func TestParseValidationTag_MemoizesTheParsedTag(t *testing.T) {
    const tag = "notBlank,min(value=2),regex(pattern=^[a-z]+$)"

    first, firstErr := parseValidationTag(tag)
    if nil != firstErr {
        t.Fatalf("expected the tag to parse, got: %v", firstErr)
    }

    second, secondErr := parseValidationTag(tag)
    if nil != secondErr {
        t.Fatalf("expected the tag to parse, got: %v", secondErr)
    }

    if 0 == len(first) || 0 == len(second) {
        t.Fatalf("expected parsed rules")
    }

    if &first[0] != &second[0] {
        t.Fatalf("expected the same tag to be parsed once and memoized")
    }
}

func TestParseValidationTag_MemoizesTheSyntaxError(t *testing.T) {
    const tag = "min(1))"

    _, firstErr := parseValidationTag(tag)
    _, secondErr := parseValidationTag(tag)

    if nil == firstErr || nil == secondErr {
        t.Fatalf("expected a syntax error for %q", tag)
    }

    if firstErr != secondErr {
        t.Fatalf("expected the syntax error to be memoized, got %v and %v", firstErr, secondErr)
    }
}

func TestParseIntStrict_RefusesTrailingGarbage(t *testing.T) {
    for _, valueString := range []string{"99.5", "-0.5", "1e3", "5abc", "0x10", "1_000", "", " 5"} {
        if _, ok := parseIntStrict(valueString); true == ok {
            t.Fatalf("expected %q to be refused", valueString)
        }
    }

    for _, valueString := range []string{"5", "-5", "+5", "0"} {
        parsed, ok := parseIntStrict(valueString)
        if false == ok {
            t.Fatalf("expected %q to parse", valueString)
        }

        if 0 != parsed && 5 != parsed && -5 != parsed {
            t.Fatalf("unexpected parse of %q: %d", valueString, parsed)
        }
    }
}

func TestParseValidationTag_RefusesATagWithZeroRules(t *testing.T) {
    for _, tag := range []string{",", " , ", ",,"} {
        rules, err := parseValidationTag(tag)

        if nil == err {
            t.Fatalf("expected tag %q to be refused, got rules %v", tag, rules)
        }
    }
}

/* pointerOf builds the optional value every constraint has to skip rather than refuse; it lives beside the rule that decides optionality, and the constraint test files reach it from the package. */
func pointerOf(value string) *string {
    return &value
}

func TestParseValidationTag_EveryShapeTheGrammarRefuses(t *testing.T) {
    for _, refusedTag := range []string{
        "(1,2)",
        "between(min=1,oops)",
        "between(=1)",
        "=5",
        ",",
        " , ",
    } {
        rules, parseErr := parseValidationTagUncached(refusedTag)
        if nil == parseErr {
            t.Fatalf("expected %q to be refused, got %#v", refusedTag, rules)
        }

        if "invalid validation tag syntax" != parseErr.Error() {
            t.Fatalf("unexpected refusal for %q: %v", refusedTag, parseErr)
        }
    }
}

func TestParseValidationTag_ADoubledCommaIsSkippedRatherThanRefused(t *testing.T) {
    rules, parseErr := parseValidationTagUncached("between(min=1,,max=9)")
    if nil != parseErr {
        t.Fatalf("unexpected error: %v", parseErr)
    }

    if 1 != len(rules) {
        t.Fatalf("expected one rule, got %#v", rules)
    }

    if "1" != rules[0].params["min"] || "9" != rules[0].params["max"] {
        t.Fatalf("expected both parameters to survive the doubled comma, got %#v", rules[0].params)
    }

    rules, parseErr = parseValidationTagUncached("notBlank,  ,max=5")
    if nil != parseErr {
        t.Fatalf("unexpected error: %v", parseErr)
    }

    if 2 != len(rules) {
        t.Fatalf("expected the empty part to be skipped and both rules to survive, got %#v", rules)
    }

    if "notBlank" != rules[0].name || "max" != rules[1].name {
        t.Fatalf("unexpected rules: %#v", rules)
    }
}

func TestHasBalancedBrackets_AnUnbalancedClosingBraceIsRefusedToo(t *testing.T) {
    if true == hasBalancedBrackets("a}b") {
        t.Fatalf("expected a stray closing brace to be refused")
    }

    if true == hasBalancedBrackets("a)b") {
        t.Fatalf("expected a stray closing parenthesis to be refused")
    }

    if false == hasBalancedBrackets("a{2}b") {
        t.Fatalf("expected a balanced quantifier to be accepted")
    }

    if false == hasBalancedBrackets("(a){2}") {
        t.Fatalf("expected balanced parentheses and braces to be accepted")
    }
}

func TestSplitByCommaOutsideRegexMeta_BothQuoteCharactersHoldACommaTogether(t *testing.T) {
    doubleQuoted := splitByCommaOutsideRegexMeta(`message="one, two",max=5`)
    if 2 != len(doubleQuoted) {
        t.Fatalf("expected the double-quoted comma to stay inside its member, got %#v", doubleQuoted)
    }

    if `message="one, two"` != doubleQuoted[0] {
        t.Fatalf("unexpected first member: %q", doubleQuoted[0])
    }

    singleQuoted := splitByCommaOutsideRegexMeta(`message='one, two',max=5`)
    if 2 != len(singleQuoted) {
        t.Fatalf("expected the single-quoted comma to stay inside its member, got %#v", singleQuoted)
    }

    if `message='one, two'` != singleQuoted[0] {
        t.Fatalf("unexpected first member: %q", singleQuoted[0])
    }

    /* a quote of the other kind inside a quoted section is a literal, so it must not close the section it sits in */
    mixed := splitByCommaOutsideRegexMeta(`message="it's one, two",max=5`)
    if 2 != len(mixed) {
        t.Fatalf("expected the apostrophe inside the double-quoted value to stay literal, got %#v", mixed)
    }
}

func TestSplitByCommaOutsideRegexMeta_AnEscapeInsideACharacterClassCountsAsContent(t *testing.T) {
    parts := splitByCommaOutsideRegexMeta(`regex=[\],]+,max=5`)
    if 2 != len(parts) {
        t.Fatalf("expected the comma inside the class to stay literal, got %#v", parts)
    }

    if `regex=[\],]+` != parts[0] {
        t.Fatalf("unexpected first member: %q", parts[0])
    }

    if `max=5` != parts[1] {
        t.Fatalf("unexpected second member: %q", parts[1])
    }

    if false == hasBalancedBrackets(`[\]]`) {
        t.Fatalf("expected a class whose member is an escaped bracket to be balanced")
    }

    rules, parseErr := parseValidationTagUncached(`regex=[\d],max=5`)
    if nil != parseErr {
        t.Fatalf("unexpected error: %v", parseErr)
    }

    if 2 != len(rules) {
        t.Fatalf("expected the escaped class member to keep both rules, got %#v", rules)
    }
}
