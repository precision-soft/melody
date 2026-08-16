package security

import (
    nethttp "net/http"
    "testing"

    "github.com/precision-soft/melody/v2/exception"
    httpcontract "github.com/precision-soft/melody/v2/http/contract"
    "github.com/precision-soft/melody/v2/internal/testhelper"
    securitycontract "github.com/precision-soft/melody/v2/security/contract"
)

/* alwaysMatchingRuleMatcher answers yes to every request, a nil one included. The matchers this package ships all decline a nil request, so the nil guards inside Check are reachable only through a matcher written outside it — and "match everything" is the first matcher an application writes. */
type alwaysMatchingRuleMatcher struct {
    calledWithNil bool
}

func (instance *alwaysMatchingRuleMatcher) Matches(request httpcontract.Request) bool {
    if nil == request {
        instance.calledWithNil = true
    }

    return true
}

var _ securitycontract.Matcher = (*alwaysMatchingRuleMatcher)(nil)

/* neverMatchingRuleMatcher declines every request without reading it. */
type neverMatchingRuleMatcher struct {
}

func (instance *neverMatchingRuleMatcher) Matches(request httpcontract.Request) bool {
    return false
}

var _ securitycontract.Matcher = (*neverMatchingRuleMatcher)(nil)

/* requestWithoutHttpRequest carries the shape Check guards against on its second nil branch: a request object that exists while the net/http request behind it does not. */
type requestWithoutHttpRequest struct {
    httpcontract.Request
}

func (instance *requestWithoutHttpRequest) HttpRequest() *nethttp.Request {
    return nil
}

var _ httpcontract.Request = (*requestWithoutHttpRequest)(nil)

func TestNewApiKeyHeaderRule_EmptyExpectedValuePanics(t *testing.T) {
    testhelper.AssertPanicsWithError(
        t,
        func() {
            _ = NewApiKeyHeaderRule(NewPathPrefixMatcher("/"), "X-Api-Key", "")
        },
        "api key header rule expected value is empty",
    )
}

func TestNewApiKeyHeaderRule_EmptyHeaderNamePanics(t *testing.T) {
    testhelper.AssertPanicsWithError(
        t,
        func() {
            _ = NewApiKeyHeaderRule(NewPathPrefixMatcher("/"), "", "expected-secret")
        },
        "api key header rule header name is empty",
    )
}

func TestNewApiKeyHeaderRule_NilMatcherPanics(t *testing.T) {
    testhelper.AssertPanicsWithError(
        t,
        func() {
            _ = NewApiKeyHeaderRule(nil, "X-Api-Key", "expected-secret")
        },
        "api key header rule matcher is nil",
    )
}

func TestApiKeyHeaderRule_AppliesFollowsTheMatcher(t *testing.T) {
    matchingRule := NewApiKeyHeaderRule(&alwaysMatchingRuleMatcher{}, "X-Api-Key", "expected-secret")
    if false == matchingRule.Applies(newFirewallTestRequest("/anything")) {
        t.Fatalf("expected the rule to apply when its matcher matches")
    }

    decliningRule := NewApiKeyHeaderRule(&neverMatchingRuleMatcher{}, "X-Api-Key", "expected-secret")
    if true == decliningRule.Applies(newFirewallTestRequest("/anything")) {
        t.Fatalf("expected the rule not to apply when its matcher declines")
    }
}

func TestApiKeyHeaderRule_CheckAcceptsTheConfiguredKey(t *testing.T) {
    rule := NewApiKeyHeaderRule(NewPathPrefixMatcher("/api"), "X-Api-Key", "expected-secret")

    request := newSecurityTestRequest(
        nethttp.MethodGet,
        "/api/resource",
        map[string]string{"X-Api-Key": "expected-secret"},
        nil,
    )

    err := rule.Check(request)
    if nil != err {
        t.Fatalf("expected the configured key to be accepted, got %v", err)
    }
}

/* the key below is wrong at the SAME length, which is the input the constant-time comparison exists for: a length-only check would answer on an obviously different one. */
func TestApiKeyHeaderRule_CheckRefusesAWrongKeyOfTheSameLength(t *testing.T) {
    rule := NewApiKeyHeaderRule(NewPathPrefixMatcher("/api"), "X-Api-Key", "expected-secret")

    request := newSecurityTestRequest(
        nethttp.MethodGet,
        "/api/resource",
        map[string]string{"X-Api-Key": "expected-secreT"},
        nil,
    )

    assertRuleRefused(t, rule.Check(request))
}

func TestApiKeyHeaderRule_CheckRefusesAWrongKeyOfAnotherLength(t *testing.T) {
    rule := NewApiKeyHeaderRule(NewPathPrefixMatcher("/api"), "X-Api-Key", "expected-secret")

    request := newSecurityTestRequest(
        nethttp.MethodGet,
        "/api/resource",
        map[string]string{"X-Api-Key": "expected-secret-and-then-some"},
        nil,
    )

    assertRuleRefused(t, rule.Check(request))
}

func TestApiKeyHeaderRule_CheckRefusesAnAbsentHeader(t *testing.T) {
    rule := NewApiKeyHeaderRule(NewPathPrefixMatcher("/api"), "X-Api-Key", "expected-secret")

    request := newSecurityTestRequest(
        nethttp.MethodGet,
        "/api/resource",
        map[string]string{},
        nil,
    )

    assertRuleRefused(t, rule.Check(request))
}

/* the rule is configured with a lower-case name while the request carries the canonical one, because Header.Get canonicalizes what it looks up. Spelling the header in lower case on the request side would prove nothing: net/http canonicalizes on the way in, so both sides would agree however the rule read them. */
func TestApiKeyHeaderRule_CheckReadsTheConfiguredHeaderNameCaseInsensitively(t *testing.T) {
    rule := NewApiKeyHeaderRule(NewPathPrefixMatcher("/api"), "x-api-key", "expected-secret")

    request := newSecurityTestRequest(
        nethttp.MethodGet,
        "/api/resource",
        map[string]string{"X-Api-Key": "expected-secret"},
        nil,
    )

    err := rule.Check(request)
    if nil != err {
        t.Fatalf("expected a lower-case configured header name to find the canonical header, got %v", err)
    }
}

/* the request below carries a WRONG key, so a nil answer can only come from the early exit — had the check run, it would have refused. */
func TestApiKeyHeaderRule_CheckSkipsARequestItDoesNotApplyTo(t *testing.T) {
    rule := NewApiKeyHeaderRule(NewPathPrefixMatcher("/api"), "X-Api-Key", "expected-secret")

    request := newSecurityTestRequest(
        nethttp.MethodGet,
        "/public/resource",
        map[string]string{"X-Api-Key": "wrong-secret"},
        nil,
    )

    err := rule.Check(request)
    if nil != err {
        t.Fatalf("expected a rule that does not apply to answer nil, got %v", err)
    }
}

func TestApiKeyHeaderRule_CheckRefusesANilRequestAMatcherClaimed(t *testing.T) {
    matcher := &alwaysMatchingRuleMatcher{}
    rule := NewApiKeyHeaderRule(matcher, "X-Api-Key", "expected-secret")

    assertRuleRefused(t, rule.Check(nil))

    if false == matcher.calledWithNil {
        t.Fatalf("expected the matcher to have been consulted with the nil request")
    }
}

func TestApiKeyHeaderRule_CheckRefusesARequestWithoutAnHttpRequest(t *testing.T) {
    rule := NewApiKeyHeaderRule(&alwaysMatchingRuleMatcher{}, "X-Api-Key", "expected-secret")

    assertRuleRefused(t, rule.Check(&requestWithoutHttpRequest{}))
}

func assertRuleRefused(t *testing.T, err error) {
    t.Helper()

    if nil == err {
        t.Fatalf("expected the rule to refuse the request")
    }

    httpException := exception.AsHttpException(err)
    if nil == httpException {
        t.Fatalf("expected the refusal to carry an http status, got %T: %v", err, err)
    }

    if nethttp.StatusForbidden != httpException.StatusCode() {
        t.Fatalf("expected the refusal to be a 403, got %d", httpException.StatusCode())
    }
}
