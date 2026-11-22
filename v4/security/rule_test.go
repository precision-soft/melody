package security

import (
    "testing"

    "github.com/precision-soft/melody/v4/http"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

/* the matcher is real because the constructor checks it FIRST: passing nil made both of these observe the matcher's panic instead of their own, and the guard each is named for could be deleted with the suite still green. The empty expected value is the one that matters — it makes constantTimeSecretEquals("", "") accept a request that carries no header at all. */
func TestNewApiKeyHeaderRule_EmptyExpectedValuePanics(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        _ = NewApiKeyHeaderRule(&alwaysApplyingMatcher{}, "", ApiKeyHeaderRuleConfig{HeaderName: "X-Api-Key"})
    }, "api key header rule expected value is empty")
}

func TestNewApiKeyHeaderRule_NilMatcherPanics(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        _ = NewApiKeyHeaderRule(nil, "secret", ApiKeyHeaderRuleConfig{HeaderName: "X-Api-Key"})
    }, "api key header rule matcher is nil")
}

func TestNewApiKeyHeaderRule_TypedNilMatcherPanics(t *testing.T) {
    var typedNilMatcher *PathPrefixMatcher

    testhelper.AssertPanicsWithError(t, func() {
        _ = NewApiKeyHeaderRule(typedNilMatcher, "secret", ApiKeyHeaderRuleConfig{HeaderName: "X-Api-Key"})
    }, "api key header rule matcher is nil")
}

/* alwaysApplyingMatcher is the application's matcher answering yes for any request, which is what carries
a request past Applies and into Check's own guards. The framework's PathPrefixMatcher refuses a request it
cannot read, so it can never exercise them. */
type alwaysApplyingMatcher struct{}

func (instance *alwaysApplyingMatcher) Matches(request httpcontract.Request) bool {
    return true
}

var _ securitycontract.Matcher = (*alwaysApplyingMatcher)(nil)

/* A nil pointer of a request type is a non-nil interface, so a bare comparison would carry it
to the header read, which dereferences it. The rule must refuse a request it cannot read, not crash on it. */
func TestApiKeyHeaderRule_Check_ATypedNilRequestIsForbidden(t *testing.T) {
    rule := NewApiKeyHeaderRule(&alwaysApplyingMatcher{}, "secret", ApiKeyHeaderRuleConfig{HeaderName: "X-Api-Key"})

    var unassignedRequest *http.Request

    err := rule.Check(nil, unassignedRequest)
    if nil == err {
        t.Fatalf("expected a typed nil request to be refused")
    }
}

func TestNewApiKeyHeaderRule_RefusesAMissingArgumentAndDefaultsTheHeaderName(t *testing.T) {
    matcher := NewPathPrefixMatcher("/api")

    cases := []struct {
        name               string
        matcher            securitycontract.Matcher
        expectedValue      string
        config             ApiKeyHeaderRuleConfig
        expectedArgument   string
        expectedHeaderName string
    }{
        {"a nil matcher", nil, "secret", ApiKeyHeaderRuleConfig{}, "matcher", ""},
        {"an empty expected value", matcher, "", ApiKeyHeaderRuleConfig{HeaderName: "X-Api-Key"}, "expectedValue", ""},
        {"an empty header name", matcher, "secret", ApiKeyHeaderRuleConfig{}, "", DefaultApiKeyHeaderName},
        {"a declared header name", matcher, "secret", ApiKeyHeaderRuleConfig{HeaderName: "X-Service-Key"}, "", "X-Service-Key"},
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            if "" != testCase.expectedArgument {
                expectPanicNamingTheArgument(t, testCase.expectedArgument, func() {
                    NewApiKeyHeaderRule(testCase.matcher, testCase.expectedValue, testCase.config)
                })

                return
            }

            if rule := NewApiKeyHeaderRule(testCase.matcher, testCase.expectedValue, testCase.config); testCase.expectedHeaderName != rule.headerName {
                t.Fatalf("expected the header %q, got %q", testCase.expectedHeaderName, rule.headerName)
            }
        })
    }
}
