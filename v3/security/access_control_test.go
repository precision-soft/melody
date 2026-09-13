package security

import (
    "reflect"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/internal/testhelper"
    "github.com/precision-soft/melody/v3/security/accesscontrol"
)

func TestAccessControl_Match_ReturnsFalseWhenNoRules(t *testing.T) {
    control := NewAccessControl()

    attributes, matched := control.Match("/admin")
    if true == matched {
        t.Fatalf("expected not matched")
    }
    if 0 != len(attributes) {
        t.Fatalf("expected empty attributes")
    }
}

func TestAccessControl_Match_LongestPrefixWins(t *testing.T) {
    control := NewAccessControl(
        NewAccessControlRule("/admin", "ROLE_ADMIN"),
        NewAccessControlRule("/admin/settings", "ROLE_SETTINGS"),
    )

    attributes, matched := control.Match("/admin/settings/users")
    if false == matched {
        t.Fatalf("expected matched")
    }
    if 1 != len(attributes) {
        t.Fatalf("expected one attribute")
    }
    if "ROLE_SETTINGS" != attributes[0] {
        t.Fatalf("unexpected attribute")
    }
}

func TestAccessControl_Match_FallbackRuleWhenPrefixEmpty(t *testing.T) {
    control := NewAccessControl(
        NewAccessControlRule("", "ROLE_ANY"),
        NewAccessControlRule("/admin", "ROLE_ADMIN"),
    )

    attributes, matched := control.Match("/public")
    if false == matched {
        t.Fatalf("expected matched by fallback")
    }
    if 1 != len(attributes) {
        t.Fatalf("expected one attribute")
    }
    if "ROLE_ANY" != attributes[0] {
        t.Fatalf("unexpected attribute")
    }
}

func TestAccessControlRule_NormalizesPrefixAndFiltersEmptyAttributes(t *testing.T) {
    rule := NewAccessControlRule("/admin/", "ROLE_ADMIN", "", "   ", "ROLE_USER")

    control := NewAccessControl(rule)

    attributes, matched := control.Match("/admin/dashboard")
    if false == matched {
        t.Fatalf("expected matched")
    }

    if 2 != len(attributes) {
        t.Fatalf("expected two attributes")
    }

    if "ROLE_ADMIN" != attributes[0] {
        t.Fatalf("unexpected attribute")
    }
    if "ROLE_USER" != attributes[1] {
        t.Fatalf("unexpected attribute")
    }
}

func TestAccessControl_Match_EmptyPathNormalizedToRoot(t *testing.T) {
    control := NewAccessControl(
        NewAccessControlRule("/", "ROLE_ROOT"),
    )

    attributes, matched := control.Match("")
    if false == matched {
        t.Fatalf("expected matched")
    }
    if 1 != len(attributes) {
        t.Fatalf("expected one attribute")
    }
    if "ROLE_ROOT" != attributes[0] {
        t.Fatalf("unexpected attribute")
    }
}

func TestAccessControl_Match_ExactRuleMatchesMultipleTrailingSlashes(t *testing.T) {
    control := NewAccessControl(
        NewAccessControlExactRule("/admin", "ROLE_ADMIN"),
    )

    attributes, matched := control.Match("/admin//")
    if false == matched {
        t.Fatalf("auth bypass: /admin// did not match the exact /admin rule (router collapses all trailing slashes)")
    }
    if 1 != len(attributes) || "ROLE_ADMIN" != attributes[0] {
        t.Fatalf("unexpected attributes %v", attributes)
    }

    attributes, matched = control.Match("/admin///")
    if false == matched {
        t.Fatalf("auth bypass: /admin/// did not match the exact /admin rule")
    }
}

func TestAccessControlMatch_ExactWinsBeforePrefixAndRegex(t *testing.T) {
    accessControl := NewAccessControl(
        NewAccessControlRegexRule("^/", "ROLE_USER"),
        NewAccessControlRule("/products", "ROLE_EDITOR"),
        NewAccessControlExactRule("/", "PUBLIC_ACCESS"),
    )

    attributes, ok := accessControl.Match("/")
    if false == ok {
        t.Fatalf("expected ok")
    }

    expectedAttributes := []string{"PUBLIC_ACCESS"}
    if false == reflect.DeepEqual(expectedAttributes, attributes) {
        t.Fatalf("expected attributes %v, got %v", expectedAttributes, attributes)
    }

    attributes, ok = accessControl.Match("/products")
    if false == ok {
        t.Fatalf("expected ok")
    }

    expectedAttributes = []string{"ROLE_EDITOR"}
    if false == reflect.DeepEqual(expectedAttributes, attributes) {
        t.Fatalf("expected attributes %v, got %v", expectedAttributes, attributes)
    }
}

func TestAccessControlMatch_ExactRuleMatchesTrailingSlashPath(t *testing.T) {
    accessControl := NewAccessControl(
        NewAccessControlExactRule("/admin", "ROLE_ADMIN"),
    )

    attributes, ok := accessControl.Match("/admin/")
    if false == ok {
        t.Fatalf("expected ok")
    }

    expectedAttributes := []string{"ROLE_ADMIN"}
    if false == reflect.DeepEqual(expectedAttributes, attributes) {
        t.Fatalf("expected attributes %v, got %v", expectedAttributes, attributes)
    }
}

func TestAccessControlMatch_PrefixLongestWinsBeforeRegex(t *testing.T) {
    accessControl := NewAccessControl(
        NewAccessControlRegexRule("^/", "ROLE_USER"),
        NewAccessControlRule("/products", "ROLE_EDITOR"),
        NewAccessControlRule("/products/api", "ROLE_ADMIN"),
    )

    attributes, ok := accessControl.Match("/products/api/read")
    if false == ok {
        t.Fatalf("expected ok")
    }

    expectedAttributes := []string{"ROLE_ADMIN"}
    if false == reflect.DeepEqual(expectedAttributes, attributes) {
        t.Fatalf("expected attributes %v, got %v", expectedAttributes, attributes)
    }
}

func TestAccessControlMatch_RegexFirstMatchWinsInDeclarationOrder(t *testing.T) {
    accessControl := NewAccessControl(
        NewAccessControlRegexRule("^/products", "A"),
        NewAccessControlRegexRule("^/products/api", "B"),
    )

    attributes, ok := accessControl.Match("/products/api/read")
    if false == ok {
        t.Fatalf("expected ok")
    }

    expectedAttributes := []string{"A"}
    if false == reflect.DeepEqual(expectedAttributes, attributes) {
        t.Fatalf("expected attributes %v, got %v", expectedAttributes, attributes)
    }
}

func TestAccessControlMatch_FallbackEmptyPrefixIsUsedLast(t *testing.T) {
    accessControl := NewAccessControl(
        NewAccessControlRuleWithSegmentPrefix("/login", "PUBLIC_ACCESS"),
        NewAccessControlRule("", "ROLE_USER"),
    )

    attributes, ok := accessControl.Match("/login")
    if false == ok {
        t.Fatalf("expected ok")
    }

    expectedAttributes := []string{"PUBLIC_ACCESS"}
    if false == reflect.DeepEqual(expectedAttributes, attributes) {
        t.Fatalf("expected attributes %v, got %v", expectedAttributes, attributes)
    }

    attributes, ok = accessControl.Match("/anything")
    if false == ok {
        t.Fatalf("expected ok")
    }

    expectedAttributes = []string{"ROLE_USER"}
    if false == reflect.DeepEqual(expectedAttributes, attributes) {
        t.Fatalf("expected attributes %v, got %v", expectedAttributes, attributes)
    }
}

func TestAccessControlMatch_EmptyPathIsNormalizedToSlash(t *testing.T) {
    accessControl := NewAccessControl(
        NewAccessControlExactRule("/", "PUBLIC_ACCESS"),
        NewAccessControlRegexRule("^/", "ROLE_USER"),
    )

    attributes, ok := accessControl.Match("")
    if false == ok {
        t.Fatalf("expected ok")
    }

    expectedAttributes := []string{"PUBLIC_ACCESS"}
    if false == reflect.DeepEqual(expectedAttributes, attributes) {
        t.Fatalf("expected attributes %v, got %v", expectedAttributes, attributes)
    }
}

func TestRules_ReturnsCopy(t *testing.T) {
    accessControl := NewAccessControl(
        NewAccessControlRuleWithSegmentPrefix("/login", "PUBLIC_ACCESS"),
        NewAccessControlRule("/products", "ROLE_EDITOR"),
    )

    rules := accessControl.Rules()
    if 2 != len(rules) {
        t.Fatalf("expected 2 rules, got %d", len(rules))
    }

    rules[0] = NewAccessControlRule("/changed", "X")

    rulesAfter := accessControl.Rules()
    if true == strings.HasPrefix(rulesAfter[0].PathPrefix(), "/changed") {
        t.Fatalf("expected returned rules to be a copy")
    }
}

/* the attribute is ROLE_ADMIN and not PUBLIC_ACCESS, the precaution the invalid-pattern sibling already takes: with PUBLIC_ACCESS the unanchored refusal below panics for an empty pattern too, so this test passed with its own guard deleted and only observed the neighbour's. The message is what pins which one answered. */
func TestNewAccessControlRegexRule_EmptyPatternPanics(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        _ = NewAccessControlRegexRule("", "ROLE_ADMIN")
    }, "access control regex pattern may not be empty")
}

func TestNewAccessControlRegexRule_InvalidPatternPanics(t *testing.T) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatalf("expected panic")
        }
    }()

    /* a non-public attribute, so the compile failure is what panics rather than the unanchored-public refusal that would fire first for PUBLIC_ACCESS */
    _ = NewAccessControlRegexRule("(", "ROLE_ADMIN")
}

func TestNewAccessControlRegexRule_UnanchoredPublicPatternPanics(t *testing.T) {
    defer func() {
        if nil == recover() {
            t.Fatalf("expected an unanchored public regex rule to be refused at construction")
        }
    }()

    _ = NewAccessControlRegexRule("/status", "PUBLIC_ACCESS")
}

func TestNewAccessControlRegexRule_AnchoredPublicPatternIsAllowed(t *testing.T) {
    accessControl := NewAccessControl(
        NewAccessControlRegexRule("^/status(/|$)", "PUBLIC_ACCESS"),
    )

    attributes, matched := accessControl.Match("/status")
    if false == matched {
        t.Fatalf("expected the anchored public rule to match")
    }
    if 1 != len(attributes) || "PUBLIC_ACCESS" != attributes[0] {
        t.Fatalf("expected PUBLIC_ACCESS, got %v", attributes)
    }
}

func TestNewAccessControlExactRule_EmptyPathPanics(t *testing.T) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatalf("expected panic")
        }
    }()

    _ = NewAccessControlExactRule("", "PUBLIC_ACCESS")
}

func TestNewAccessControlRuleWithSegmentPrefix_EmptyPrefixPanics(t *testing.T) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatalf("expected panic when the segment prefix is empty")
        }
    }()

    _ = NewAccessControlRuleWithSegmentPrefix("", "PUBLIC_ACCESS")
}

func TestNewAccessControlRule_PublicAccessCombinedWithOtherAttributesPanics(t *testing.T) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatalf("expected panic when PUBLIC_ACCESS is combined with another attribute")
        }
    }()

    _ = NewAccessControlRuleWithSegmentPrefix("/admin", "PUBLIC_ACCESS", "ROLE_ADMIN")
}

func TestNewAccessControlRule_LonePublicAccessIsAllowed(t *testing.T) {
    defer func() {
        if nil != recover() {
            t.Fatalf("a lone PUBLIC_ACCESS attribute must not panic")
        }
    }()

    rule := NewAccessControlRuleWithSegmentPrefix("/health", "PUBLIC_ACCESS")
    if 1 != len(rule.Attributes()) {
        t.Fatalf("expected exactly one attribute, got %v", rule.Attributes())
    }
}

/* a rule whose attributes all normalize away still matches its path, so it granted every authenticated principal and shadowed any longer-prefixed rule that would have denied; the blank attribute is refused at construction instead */
func TestAccessControlRule_RejectsAnAttributeListThatNormalizesToEmpty(t *testing.T) {
    for _, attributes := range [][]string{
        {},
        {""},
        {"   "},
        {"", "  "},
    } {
        testhelper.AssertPanicsWithError(t, func() {
            NewAccessControlRule("/admin", attributes...)
        }, "access control rule requires at least one attribute")

        testhelper.AssertPanicsWithError(t, func() {
            NewAccessControlExactRule("/admin", attributes...)
        }, "access control rule requires at least one attribute")

        testhelper.AssertPanicsWithError(t, func() {
            NewAccessControlRegexRule("^/admin", attributes...)
        }, "access control rule requires at least one attribute")

        testhelper.AssertPanicsWithError(t, func() {
            NewAccessControlRuleWithSegmentPrefix("/admin", attributes...)
        }, "access control rule requires at least one attribute")
    }

    rule := NewAccessControlRule("/admin", "", "ROLE_ADMIN", "   ")
    if 1 != len(rule.Attributes()) || "ROLE_ADMIN" != rule.Attributes()[0] {
        t.Fatalf("expected the blank attributes to be trimmed from a non-empty list, got %v", rule.Attributes())
    }
}

func TestNewAccessControlRuleWithSegmentPrefix_EmptyPrefixRefused(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        NewAccessControlRuleWithSegmentPrefix("", "ROLE_ADMIN")
    }, "access control segment prefix may not be empty")
}

func TestNewAccessControlRuleWithSegmentPrefix_GovernsItsSegmentAndNoLongerSegmentText(t *testing.T) {
    control := NewAccessControl(
        NewAccessControlRuleWithSegmentPrefix("/admin", "ROLE_ADMIN"),
    )

    for _, matchingPath := range []string{"/admin", "/admin/", "/admin/panel"} {
        if _, matched := control.Match(matchingPath); false == matched {
            t.Fatalf("expected the segment rule to govern %q", matchingPath)
        }
    }

    for _, nonMatchingPath := range []string{"/administrator", "/admin-tools", "/adminpanel"} {
        if _, matched := control.Match(nonMatchingPath); true == matched {
            t.Fatalf("expected the segment rule NOT to govern %q, which only shares the prefix text", nonMatchingPath)
        }
    }
}

func TestNewAccessControlRule_LonePublicAccessAllowed(t *testing.T) {
    defer func() {
        if nil != recover() {
            t.Fatalf("a lone PUBLIC_ACCESS attribute must not panic on the default bounded rule")
        }
    }()

    rule := NewAccessControlRuleWithSegmentPrefix("/health", "PUBLIC_ACCESS")
    if accesscontrol.MatchingSegmentPrefix != rule.Matching() {
        t.Fatalf("expected the default constructor to build a segment-prefix rule")
    }
    if 1 != len(rule.Attributes()) || "PUBLIC_ACCESS" != rule.Attributes()[0] {
        t.Fatalf("expected exactly one PUBLIC_ACCESS attribute, got %v", rule.Attributes())
    }
}

func TestNewAccessControlRule_LonePublicAccessIsAllowedOnBoundedRules(t *testing.T) {
    defer func() {
        if nil != recover() {
            t.Fatalf("a lone PUBLIC_ACCESS attribute must not panic on a bounded rule")
        }
    }()

    segmentRule := NewAccessControlRuleWithSegmentPrefix("/health", "PUBLIC_ACCESS")
    exactRule := NewAccessControlExactRule("/health", "PUBLIC_ACCESS")
    regexRule := NewAccessControlRegexRule("^/health", "PUBLIC_ACCESS")

    for _, rule := range []AccessControlRule{segmentRule, exactRule, regexRule} {
        if 1 != len(rule.Attributes()) || "PUBLIC_ACCESS" != rule.Attributes()[0] {
            t.Fatalf("expected exactly one PUBLIC_ACCESS attribute, got %v", rule.Attributes())
        }
    }
}

func TestNewAccessControlRule_RawReach_PublicAccessIsRefused(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        NewAccessControlRule("/health", "PUBLIC_ACCESS")
    }, "access control PUBLIC_ACCESS may not be declared on a raw prefix rule; use a segment prefix, exact, or regex rule")
}

func TestNewAccessControlRule_RawReach_ReachesAcrossTheSegmentBoundary(t *testing.T) {
    control := NewAccessControl(
        NewAccessControlRule("/admin", "ROLE_ADMIN"),
    )

    for _, matchingPath := range []string{"/admin", "/administrator", "/admin-tools", "/admin/panel"} {
        if _, matched := control.Match(matchingPath); false == matched {
            t.Fatalf("expected the raw prefix rule to govern %q", matchingPath)
        }
    }
}

func TestAccessControl_MatchFoldsTheSpellingsACatchAllRouteAccepts(t *testing.T) {
    control := NewAccessControl(
        NewAccessControlRule("/admin", "ROLE_ADMIN"),
    )

    for _, requestPath := range []string{
        "//admin/panel",
        "/./admin/panel",
        "/open/../admin/panel",
        "/admin//panel",
        "///admin",
    } {
        attributes, matched := control.Match(requestPath)

        if false == matched {
            t.Fatalf("expected %q to be matched by the rule on /admin, it reached no rule at all", requestPath)
        }

        if 1 != len(attributes) || "ROLE_ADMIN" != attributes[0] {
            t.Fatalf("expected ROLE_ADMIN for %q, got %v", requestPath, attributes)
        }
    }
}

func TestAccessControl_MatchFoldsTheSpellingsForASegmentPrefixRule(t *testing.T) {
    control := NewAccessControl(
        NewAccessControlRuleWithSegmentPrefix("/admin", "ROLE_ADMIN"),
    )

    attributes, matched := control.Match("//admin/panel")

    if false == matched || 1 != len(attributes) || "ROLE_ADMIN" != attributes[0] {
        t.Fatalf("expected the segment prefix rule to match the doubled slash, got matched=%v attributes=%v", matched, attributes)
    }
}

func TestAccessControl_MatchStillRefusesAPathThatOnlySharesThePrefixText(t *testing.T) {
    control := NewAccessControl(
        NewAccessControlRuleWithSegmentPrefix("/admin", "ROLE_ADMIN"),
    )

    _, matched := control.Match("//administration/panel")

    if true == matched {
        t.Fatalf("expected the segment prefix rule not to match a longer segment")
    }
}

