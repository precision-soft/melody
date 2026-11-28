package accesscontrol

import (
    "testing"

    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal/testhelper"
)

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
        control := NewControl([]Rule{testCase.rule})

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

func TestNewRuleRefusesAnUnspecifiedMatching(t *testing.T) {
    defer func() {
        if nil == recover() {
            t.Fatalf("expected the unspecified matching mode to be refused")
        }
    }()

    _ = NewRule("/admin", MatchingUnspecified, RuleConfig{Attributes: []string{"ROLE_ADMIN"}})
}

func TestNewRawPrefixRuleRefusesPublicAccess(t *testing.T) {
    defer func() {
        if nil == recover() {
            t.Fatalf("expected PUBLIC_ACCESS to be refused on a raw prefix rule")
        }
    }()

    _ = NewRawPrefixRule("/health", RuleConfig{Attributes: []string{"PUBLIC_ACCESS"}})
}

func TestPublicAccessIsAllowedOnTheBoundedReaches(t *testing.T) {
    defer func() {
        if recovered := recover(); nil != recovered {
            t.Fatalf("expected PUBLIC_ACCESS to be allowed on a bounded rule, got %v", recovered)
        }
    }()

    _ = NewSegmentPrefixRule("/health", RuleConfig{Attributes: []string{"PUBLIC_ACCESS"}})
    _ = NewExactRule("/health", RuleConfig{Attributes: []string{"PUBLIC_ACCESS"}})
}

func TestNewSegmentPrefixRuleRefusesAnEmptyPath(t *testing.T) {
    defer func() {
        if nil == recover() {
            t.Fatalf("expected an empty segment prefix to be refused")
        }
    }()

    _ = NewSegmentPrefixRule("", RuleConfig{Attributes: []string{"ROLE_ADMIN"}})
}

func TestNewRawPrefixRuleKeepsTheEmptyPathAsTheFallback(t *testing.T) {
    control := NewControl([]Rule{
        NewSegmentPrefixRule("/admin", RuleConfig{Attributes: []string{"ROLE_ADMIN"}}),
        NewRawPrefixRule("", RuleConfig{Attributes: []string{"ROLE_USER"}}),
    })

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

func TestAttributesAnswersACopy(t *testing.T) {
    rule := NewSegmentPrefixRule("/admin", RuleConfig{Attributes: []string{"ROLE_ADMIN"}})

    handed := rule.Attributes()
    handed[0] = "ROLE_ANONYMOUS"

    if "ROLE_ADMIN" != rule.Attributes()[0] {
        t.Fatalf("the rule's attributes changed under a write to the handed copy: %v", rule.Attributes())
    }
}

func TestNewControlCopiesTheCallersRules(t *testing.T) {
    rules := []Rule{NewSegmentPrefixRule("/admin", RuleConfig{Attributes: []string{"ROLE_ADMIN"}})}

    control := NewControl(rules)
    rules[0] = NewSegmentPrefixRule("/admin", RuleConfig{Attributes: []string{"ROLE_ANONYMOUS"}})

    attributes, _ := control.Match("/admin")
    if "ROLE_ADMIN" != attributes[0] {
        t.Fatalf("the control changed under a write to the caller's slice: %v", attributes)
    }
}

func TestARulePathWithoutALeadingSlashIsRefusedInEveryPathMode(t *testing.T) {
    config := RuleConfig{Attributes: []string{"ROLE_ADMIN"}}

    for _, testCase := range []struct {
        name  string
        build func()
    }{
        {"exact", func() { _ = NewExactRule("admin", config) }},
        {"segment prefix", func() { _ = NewSegmentPrefixRule(" admin/panel", config) }},
        {"raw prefix", func() { _ = NewRawPrefixRule("admin", config) }},
        {"new rule", func() { _ = NewRule("admin", MatchingSegmentPrefix, config) }},
    } {
        t.Run(testCase.name, func(t *testing.T) {
            testhelper.AssertPanicsWithError(t, testCase.build, "access control rule path must begin with a slash")
        })
    }
}

func TestANonCanonicalRulePathIsRefusedInEveryPathMode(t *testing.T) {
    config := RuleConfig{Attributes: []string{"ROLE_ADMIN"}}

    for _, spelling := range []string{"//admin", "/./admin", "/x/../admin", "/admin/.", "/admin//panel"} {
        for _, testCase := range []struct {
            name  string
            build func()
        }{
            {"exact", func() { _ = NewExactRule(spelling, config) }},
            {"segment prefix", func() { _ = NewSegmentPrefixRule(spelling, config) }},
            {"raw prefix", func() { _ = NewRawPrefixRule(spelling, config) }},
        } {
            t.Run(testCase.name+" "+spelling, func(t *testing.T) {
                testhelper.AssertPanicsWithError(t, testCase.build, "access control rule path must be canonical")
            })
        }
    }
}

/* the segment rule folds one trailing slash, so the canonical check reads the declared spelling: "/admin//" folded once would pass as "/admin/" and govern nothing */
func TestASegmentRulePathWithANonCanonicalTrailIsRefused(t *testing.T) {
    config := RuleConfig{Attributes: []string{"ROLE_ADMIN"}}

    for _, spelling := range []string{"/admin//", "/admin/./"} {
        t.Run(spelling, func(t *testing.T) {
            testhelper.AssertPanicsWithError(
                t,
                func() { _ = NewSegmentPrefixRule(spelling, config) },
                "access control rule path must be canonical",
            )
        })
    }
}

func TestTheCanonicalRefusalOfASegmentRuleNamesTheDeclaredPath(t *testing.T) {
    defer func() {
        refusal, isRefusal := recover().(*exception.Error)
        if false == isRefusal {
            t.Fatalf("expected an exception error refusing the path")
        }

        if "/admin///" != refusal.Context()["path"] {
            t.Fatalf("expected the refusal to name the declared path, got %v", refusal.Context())
        }
    }()

    _ = NewSegmentPrefixRule("/admin///", RuleConfig{Attributes: []string{"ROLE_ADMIN"}})
}

func TestACanonicalRulePathWithATrailingSlashIsAccepted(t *testing.T) {
    config := RuleConfig{Attributes: []string{"ROLE_ADMIN"}}

    if "/admin" != NewExactRule("/admin/", config).PathPrefix() {
        t.Fatalf("expected the exact rule to fold its trailing slash")
    }

    if "/admin" != NewSegmentPrefixRule("/admin/", config).PathPrefix() {
        t.Fatalf("expected the segment rule to fold its trailing slash")
    }
}

func TestNewRawPrefixRuleRefusesATrailingSlash(t *testing.T) {
    testhelper.AssertPanicsWithError(
        t,
        func() { _ = NewRawPrefixRule("/api/", RuleConfig{Attributes: []string{"ROLE_USER"}}) },
        "access control raw prefix rule may not end with a slash",
    )
}

func TestNewRawPrefixRuleKeepsTheRootSpelling(t *testing.T) {
    rule := NewRawPrefixRule("/", RuleConfig{Attributes: []string{"ROLE_USER"}})

    if "/" != rule.PathPrefix() {
        t.Fatalf("expected the root rule to keep its spelling, got %q", rule.PathPrefix())
    }
}

func TestASegmentRuleLeavesTheNeighbouringTreeToItsOwnRule(t *testing.T) {
    control := NewControl([]Rule{
        NewSegmentPrefixRule("/api/", RuleConfig{Attributes: []string{"ROLE_USER"}}),
        NewRegexRule("^/api-internal(/|$)", RuleConfig{Attributes: []string{"ROLE_ADMIN"}}),
    })

    attributes, matched := control.Match("/api-internal/x")
    if false == matched || 1 != len(attributes) || "ROLE_ADMIN" != attributes[0] {
        t.Fatalf("expected the regex rule to govern its tree, got %v matched=%v", attributes, matched)
    }

    attributes, matched = control.Match("/api/x")
    if false == matched || "ROLE_USER" != attributes[0] {
        t.Fatalf("expected the segment rule to govern its tree, got %v matched=%v", attributes, matched)
    }
}

func TestNewControlRefusesTheZeroRule(t *testing.T) {
    testhelper.AssertPanicsWithError(
        t,
        func() { _ = NewControl([]Rule{NewSegmentPrefixRule("/admin", RuleConfig{Attributes: []string{"ROLE_ADMIN"}}), Rule{}}) },
        "access control rule carries no attribute",
    )
}

func TestNewRuleRefusalOfAnOutOfRangeMatchingCarriesTheNumber(t *testing.T) {
    defer func() {
        refusal, isRefusal := recover().(*exception.Error)
        if false == isRefusal {
            t.Fatalf("expected an exception error refusing the matching mode")
        }

        if "unspecified" != refusal.Context()["matching"] || 42 != refusal.Context()["matchingValue"] {
            t.Fatalf("expected the refusal to carry the name and the number of the mode, got %v", refusal.Context())
        }
    }()

    _ = NewRule("/admin", Matching(42), RuleConfig{Attributes: []string{"ROLE_ADMIN"}})
}

func TestARepeatedPublicAccessIsRefusedByNameWithTheAttributesItCounted(t *testing.T) {
    defer func() {
        refusal, isRefusal := recover().(*exception.Error)
        if false == isRefusal {
            t.Fatalf("expected an exception error refusing the repeated PUBLIC_ACCESS")
        }

        if "access control PUBLIC_ACCESS may not be combined with other attributes or repeated" != refusal.Message() {
            t.Fatalf("expected the refusal to say repeated, got %q", refusal.Message())
        }

        attributes, areAttributes := refusal.Context()["attributes"].([]string)
        if false == areAttributes || 2 != len(attributes) {
            t.Fatalf("expected the refusal to carry the two attributes it counted, got %v", refusal.Context())
        }
    }()

    _ = NewExactRule("/health", RuleConfig{Attributes: []string{"PUBLIC_ACCESS", " PUBLIC_ACCESS "}})
}
