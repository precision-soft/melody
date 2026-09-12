package accesscontrol

import (
    "testing"
)

/* the four modes are the whole point of the package, and each one answers differently for the same set of paths. The table is the specification: a reader who wants to know which constructor to reach for reads the columns, and a change that blurs two modes into one fails here. */
func TestTheFourMatchingModesGovernDifferentPaths(t *testing.T) {
    paths := []string{"/admin", "/admin/panel", "/administrator", "/x/admin", "/a/admin/b"}

    for _, testCase := range []struct {
        name     string
        rule     Rule
        governed []bool
    }{
        {
            "exact",
            NewExactRule("/admin", RuleConfig{Attributes: []string{"ROLE_ADMIN"}}),
            []bool{true, false, false, false, false},
        },
        {
            "segment prefix",
            NewSegmentPrefixRule("/admin", RuleConfig{Attributes: []string{"ROLE_ADMIN"}}),
            []bool{true, true, false, false, false},
        },
        {
            "raw prefix",
            NewRawPrefixRule("/admin", RuleConfig{Attributes: []string{"ROLE_ADMIN"}}),
            []bool{true, true, true, false, false},
        },
        {
            "regex",
            NewRegexRule("/admin", RuleConfig{Attributes: []string{"ROLE_ADMIN"}}),
            []bool{true, true, true, true, true},
        },
    } {
        control := NewControl(testCase.rule)

        for index, path := range paths {
            _, matched := control.Match(path)
            if testCase.governed[index] != matched {
                t.Fatalf(
                    "the %s rule for /admin answered %v for %q, expected %v",
                    testCase.name,
                    matched,
                    path,
                    testCase.governed[index],
                )
            }
        }
    }
}

/* NewRule is the door for a mode chosen by a variable, so it must answer exactly what the mode-specific constructor answers — otherwise the two doors drift and the wrapper becomes a second implementation. */
func TestNewRuleAnswersTheSameAsTheModeSpecificConstructor(t *testing.T) {
    config := RuleConfig{Attributes: []string{"ROLE_ADMIN"}}

    for _, testCase := range []struct {
        matching Matching
        expected Rule
    }{
        {MatchingExact, NewExactRule("/admin", config)},
        {MatchingSegmentPrefix, NewSegmentPrefixRule("/admin", config)},
        {MatchingRawPrefix, NewRawPrefixRule("/admin", config)},
    } {
        built := NewRule("/admin", testCase.matching, config)

        if testCase.expected.Matching() != built.Matching() {
            t.Fatalf(
                "NewRule with %s built a %s rule",
                testCase.matching,
                built.Matching(),
            )
        }
        if testCase.expected.PathPrefix() != built.PathPrefix() {
            t.Fatalf("NewRule with %s built the path %q", testCase.matching, built.PathPrefix())
        }
    }

    regexRule := NewRule("^/admin", MatchingRegex, config)
    if MatchingRegex != regexRule.Matching() || "^/admin" != regexRule.Pattern() {
        t.Fatalf("NewRule with the regex mode built %s carrying %q", regexRule.Matching(), regexRule.Pattern())
    }
}

/* the zero value is refused rather than defaulted: the reach is what an access control rule IS, and a caller who omits it would inherit one they never chose — which is how a rule silently stops governing a path it used to. */
func TestNewRuleRefusesAnUnspecifiedMatching(t *testing.T) {
    defer func() {
        if nil == recover() {
            t.Fatalf("expected the unspecified matching mode to be refused")
        }
    }()

    _ = NewRule("/admin", MatchingUnspecified, RuleConfig{Attributes: []string{"ROLE_ADMIN"}})
}

/* a raw public rule is the longest match wherever it reaches, so it opens every path merely beginning with the prefix and shadows a bounded denial that would have refused. */
func TestNewRawPrefixRuleRefusesPublicAccess(t *testing.T) {
    defer func() {
        if nil == recover() {
            t.Fatalf("expected PUBLIC_ACCESS to be refused on a raw prefix rule")
        }
    }()

    _ = NewRawPrefixRule("/health", RuleConfig{Attributes: []string{"PUBLIC_ACCESS"}})
}

/* the same attribute is allowed on the bounded reaches, where it cannot claim a path outside the one it names. */
func TestPublicAccessIsAllowedOnTheBoundedReaches(t *testing.T) {
    defer func() {
        if recovered := recover(); nil != recovered {
            t.Fatalf("expected PUBLIC_ACCESS to be allowed on a bounded rule, got %v", recovered)
        }
    }()

    _ = NewSegmentPrefixRule("/health", RuleConfig{Attributes: []string{"PUBLIC_ACCESS"}})
    _ = NewExactRule("/health", RuleConfig{Attributes: []string{"PUBLIC_ACCESS"}})
}

/* an empty segment prefix would normalize to "" and answer for every path no other rule claimed, so a rule declared for one section would silently govern the whole application. */
func TestNewSegmentPrefixRuleRefusesAnEmptyPath(t *testing.T) {
    defer func() {
        if nil == recover() {
            t.Fatalf("expected an empty segment prefix to be refused")
        }
    }()

    _ = NewSegmentPrefixRule("", RuleConfig{Attributes: []string{"ROLE_ADMIN"}})
}

/* the raw reach keeps the empty spelling: it is the declared catch-all fallback, which answers only when no other rule did. */
func TestNewRawPrefixRuleKeepsTheEmptyPathAsTheFallback(t *testing.T) {
    control := NewControl(
        NewSegmentPrefixRule("/admin", RuleConfig{Attributes: []string{"ROLE_ADMIN"}}),
        NewRawPrefixRule("", RuleConfig{Attributes: []string{"ROLE_USER"}}),
    )

    attributes, matched := control.Match("/anything/else")
    if false == matched || 1 != len(attributes) || "ROLE_USER" != attributes[0] {
        t.Fatalf("expected the fallback to answer, got %v matched=%v", attributes, matched)
    }

    attributes, matched = control.Match("/admin/panel")
    if false == matched || "ROLE_ADMIN" != attributes[0] {
        t.Fatalf("expected the segment rule to outrank the fallback, got %v matched=%v", attributes, matched)
    }
}

func TestARuleRequiresAtLeastOneAttribute(t *testing.T) {
    defer func() {
        if nil == recover() {
            t.Fatalf("expected a rule with no attribute to be refused")
        }
    }()

    _ = NewSegmentPrefixRule("/admin", RuleConfig{Attributes: []string{"  "}})
}

func TestPublicAccessMayNotBeCombinedWithAnotherAttribute(t *testing.T) {
    defer func() {
        if nil == recover() {
            t.Fatalf("expected PUBLIC_ACCESS beside a role to be refused")
        }
    }()

    _ = NewSegmentPrefixRule("/admin", RuleConfig{Attributes: []string{"PUBLIC_ACCESS", "ROLE_ADMIN"}})
}

/* Attributes answers a copy: a caller that writes into the slice it is handed must not change the compiled policy. */
func TestAttributesAnswersACopy(t *testing.T) {
    rule := NewSegmentPrefixRule("/admin", RuleConfig{Attributes: []string{"ROLE_ADMIN"}})

    handed := rule.Attributes()
    handed[0] = "ROLE_ANONYMOUS"

    if "ROLE_ADMIN" != rule.Attributes()[0] {
        t.Fatalf("the rule's attributes changed under a write to the handed copy: %v", rule.Attributes())
    }
}

/* the caller's rule slice is copied too, so a later write to it does not change the compiled control. */
func TestNewControlCopiesTheCallersRules(t *testing.T) {
    rules := []Rule{NewSegmentPrefixRule("/admin", RuleConfig{Attributes: []string{"ROLE_ADMIN"}})}

    control := NewControl(rules...)
    rules[0] = NewSegmentPrefixRule("/admin", RuleConfig{Attributes: []string{"ROLE_ANONYMOUS"}})

    attributes, _ := control.Match("/admin")
    if "ROLE_ADMIN" != attributes[0] {
        t.Fatalf("the control changed under a write to the caller's slice: %v", attributes)
    }
}
