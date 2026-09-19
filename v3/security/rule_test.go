package security

import (
    "testing"

    "github.com/precision-soft/melody/v3/http"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal/testhelper"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

/* the matcher is real because the constructor checks it FIRST: passing nil made both of these observe the matcher's panic instead of their own, and the guard each is named for could be deleted with the suite still green. The empty expected value is the one that matters — it makes constantTimeSecretEquals("", "") accept a request that carries no header at all. */
func TestNewApiKeyHeaderRule_EmptyExpectedValuePanics(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        _ = NewApiKeyHeaderRule(&alwaysApplyingMatcher{}, "X-Api-Key", "")
    }, "api key header rule expected value is empty")
}

func TestNewApiKeyHeaderRule_EmptyHeaderNamePanics(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        _ = NewApiKeyHeaderRule(&alwaysApplyingMatcher{}, "", "expected-secret")
    }, "api key header rule header name is empty")
}

func TestNewApiKeyHeaderRule_NilMatcherPanics(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        _ = NewApiKeyHeaderRule(nil, "X-Api-Key", "secret")
    }, "api key header rule matcher is nil")
}

func TestNewApiKeyHeaderRule_TypedNilMatcherPanics(t *testing.T) {
    var typedNilMatcher *PathPrefixMatcher

    testhelper.AssertPanicsWithError(t, func() {
        _ = NewApiKeyHeaderRule(typedNilMatcher, "X-Api-Key", "secret")
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

/* A nil pointer of a request type is a non-nil interface, so the bare comparison this replaces carried it
to the header read, which dereferences it. The rule must refuse a request it cannot read, not crash on it. */
func TestApiKeyHeaderRule_Check_ATypedNilRequestIsForbidden(t *testing.T) {
    rule := NewApiKeyHeaderRule(&alwaysApplyingMatcher{}, "X-Api-Key", "secret")

    var unassignedRequest *http.Request

    err := rule.Check(unassignedRequest)
    if nil == err {
        t.Fatalf("expected a typed nil request to be refused")
    }
}
