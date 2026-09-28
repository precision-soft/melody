package accesscontrol

import (
    "testing"

    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal/testhelper"
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

/* the zero value is refused rather than defaulted: a caller who omits the reach would inherit one they never chose */
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

/* the request side is canonicalized to a leading slash, so a path declared without one could never match and the paths it names would reach their handlers without a decision. */
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

/* the request path is matched cleaned, so a rule spelled with an empty, "." or ".." segment could never match and would govern nothing */
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

/* a trailing slash stays each constructor's to read: the exact and segment rules fold it, so the canonical refusal does not reach it */
func TestACanonicalRulePathWithATrailingSlashIsAccepted(t *testing.T) {
    config := RuleConfig{Attributes: []string{"ROLE_ADMIN"}}

    if "/admin" != NewExactRule("/admin/", config).PathPrefix() {
        t.Fatalf("expected the exact rule to fold its trailing slash")
    }

    if "/admin" != NewSegmentPrefixRule("/admin/", config).PathPrefix() {
        t.Fatalf("expected the segment rule to fold its trailing slash")
    }
}

/* a raw reach spelled with a trailing slash would be stored without it and claim every sibling beginning with the same letters, the reach the slash was written to exclude. */
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

/* the reach the trailing slash meant is the segment one, and declared so the neighbouring tree stays with the rule that names it. */
func TestASegmentRuleLeavesTheNeighbouringTreeToItsOwnRule(t *testing.T) {
    control := NewControl(
        NewSegmentPrefixRule("/api/", RuleConfig{Attributes: []string{"ROLE_USER"}}),
        NewRegexRule("^/api-internal(/|$)", RuleConfig{Attributes: []string{"ROLE_ADMIN"}}),
    )

    attributes, matched := control.Match("/api-internal/x")
    if false == matched || 1 != len(attributes) || "ROLE_ADMIN" != attributes[0] {
        t.Fatalf("expected the regex rule to govern its tree, got %v matched=%v", attributes, matched)
    }

    attributes, matched = control.Match("/api/x")
    if false == matched || "ROLE_USER" != attributes[0] {
        t.Fatalf("expected the segment rule to govern its tree, got %v matched=%v", attributes, matched)
    }
}

/* the zero rule is the empty-prefix fallback with no attribute, so it would deny every path no other rule claimed. */
func TestNewControlRefusesTheZeroRule(t *testing.T) {
    testhelper.AssertPanicsWithError(
        t,
        func() { _ = NewControl(NewSegmentPrefixRule("/admin", RuleConfig{Attributes: []string{"ROLE_ADMIN"}}), Rule{}) },
        "access control rule carries no attribute",
    )
}

