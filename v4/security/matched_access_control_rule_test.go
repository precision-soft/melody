package security

import (
    "testing"

    "github.com/precision-soft/melody/v4/security/accesscontrol"
)

func TestNewMatchedAccessControlRule_CarriesTheMatchAndAnswersACopyHoldingNoneOfTheCallersAttributes(t *testing.T) {
    callerAttributes := []string{"ROLE_EDITOR", "ROLE_ADMIN"}

    matched := NewMatchedAccessControlRule(
        accesscontrol.NewRawPrefixRule("/admin", accesscontrol.RuleConfig{Attributes: callerAttributes}),
        SourceFirewall,
        3,
        "main",
    )

    if "/admin" != matched.PathPrefix() {
        t.Fatalf("unexpected path prefix: %q", matched.PathPrefix())
    }

    if SourceFirewall != matched.Source() {
        t.Fatalf("unexpected source: %v", matched.Source())
    }

    if 3 != matched.RuleIndex() {
        t.Fatalf("unexpected rule index: %d", matched.RuleIndex())
    }

    if "main" != matched.Firewall() {
        t.Fatalf("unexpected firewall: %q", matched.Firewall())
    }

    callerAttributes[0] = "ROLE_ANONYMOUS"

    if "ROLE_EDITOR" != matched.Attributes()[0] {
        t.Fatalf("expected the record to hold its own copy of the attributes, got %v", matched.Attributes())
    }

    handedOut := matched.Attributes()
    handedOut[0] = "ROLE_ANONYMOUS"

    if "ROLE_EDITOR" != matched.Attributes()[0] {
        t.Fatalf("expected a caller's mutation of what it was handed not to reach the record, got %v", matched.Attributes())
    }
}

func TestNewMatchedAccessControlRule_ARuleWithoutAttributesCarriesAnEmptyList(t *testing.T) {
    matched := NewMatchedAccessControlRule(accesscontrol.Rule{}, SourceGlobal, 0, "")

    if nil == matched.Attributes() {
        t.Fatalf("expected an empty attribute list rather than a nil one")
    }

    if 0 != len(matched.Attributes()) {
        t.Fatalf("expected no attributes, got %v", matched.Attributes())
    }
}

func TestNewMatchedAccessControlRule_AnswersTheDeclarationOfEveryMode(t *testing.T) {
    config := accesscontrol.RuleConfig{Attributes: []string{"ROLE_ADMIN"}}

    testCases := []struct {
        rule               accesscontrol.Rule
        expectedMatching   accesscontrol.Matching
        expectedPathPrefix string
        expectedPattern    string
    }{
        {accesscontrol.NewExactRule("/admin", config), accesscontrol.MatchingExact, "/admin", ""},
        {accesscontrol.NewSegmentPrefixRule("/admin", config), accesscontrol.MatchingSegmentPrefix, "/admin", ""},
        {accesscontrol.NewRawPrefixRule("/admin", config), accesscontrol.MatchingRawPrefix, "/admin", ""},
        {accesscontrol.NewRawPrefixRule("", config), accesscontrol.MatchingRawPrefix, "", ""},
        {accesscontrol.NewRegexRule("^/admin", config), accesscontrol.MatchingRegex, "", "^/admin"},
    }

    for _, testCase := range testCases {
        matched := NewMatchedAccessControlRule(testCase.rule, SourceFirewall, 0, "main")

        if testCase.expectedMatching != matched.Matching() {
            t.Fatalf("expected the mode %s, got %s", testCase.expectedMatching, matched.Matching())
        }

        if testCase.expectedPathPrefix != matched.PathPrefix() {
            t.Fatalf("%s: unexpected path prefix: %q", testCase.expectedMatching, matched.PathPrefix())
        }

        if testCase.expectedPattern != matched.Pattern() {
            t.Fatalf("%s: unexpected pattern: %q", testCase.expectedMatching, matched.Pattern())
        }

        if testCase.expectedMatching != matched.Rule().Matching() || testCase.expectedPathPrefix != matched.Rule().PathPrefix() || testCase.expectedPattern != matched.Rule().Pattern() {
            t.Fatalf("%s: expected Rule to answer the rule it was given", testCase.expectedMatching)
        }

        if 1 != len(matched.Rule().Attributes()) || "ROLE_ADMIN" != matched.Rule().Attributes()[0] {
            t.Fatalf("%s: unexpected attributes on the rule: %v", testCase.expectedMatching, matched.Rule().Attributes())
        }
    }
}
