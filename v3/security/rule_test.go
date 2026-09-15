package security

import (
    "testing"

    "github.com/precision-soft/melody/v3/http"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal/testhelper"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

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

type alwaysApplyingMatcher struct{}

func (instance *alwaysApplyingMatcher) Matches(request httpcontract.Request) bool {
    return true
}

var _ securitycontract.Matcher = (*alwaysApplyingMatcher)(nil)

func TestApiKeyHeaderRule_Check_ATypedNilRequestIsForbidden(t *testing.T) {
    rule := NewApiKeyHeaderRule(&alwaysApplyingMatcher{}, "X-Api-Key", "secret")

    var unassignedRequest *http.Request

    err := rule.Check(unassignedRequest)
    if nil == err {
        t.Fatalf("expected a typed nil request to be refused")
    }
}
