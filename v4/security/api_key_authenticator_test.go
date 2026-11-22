package security

import (
    nethttp "net/http"
    "testing"
)

func TestNewApiKeyHeaderAuthenticator_EmptyExpectedValuePanics(t *testing.T) {
    defer func() {
        if nil == recover() {
            t.Fatalf("expected panic when the expected api key is empty (would never authenticate)")
        }
    }()

    _ = NewApiKeyHeaderAuthenticator("", ApiKeyHeaderAuthenticatorConfig{
        HeaderName:     "X-Api-Key",
        UserIdentifier: "u1",
        Roles:          []string{"ROLE_API"},
    })
}

func TestApiKeyHeaderAuthenticator_SupportsReturnsFalseWhenHeaderMissing(t *testing.T) {
    auth := NewApiKeyHeaderAuthenticator("expected", ApiKeyHeaderAuthenticatorConfig{
        HeaderName:     "X-Api-Key",
        UserIdentifier: "u1",
        Roles:          []string{"ROLE_API"},
    })

    request := newSecurityTestRequest(nethttp.MethodGet, "/x", map[string]string{}, nil)

    if true == auth.Supports(request) {
        t.Fatalf("expected supports to be false")
    }
}

func TestApiKeyHeaderAuthenticator_SupportsReturnsTrueWhenHeaderPresent(t *testing.T) {
    auth := NewApiKeyHeaderAuthenticator("expected", ApiKeyHeaderAuthenticatorConfig{
        HeaderName:     "X-Api-Key",
        UserIdentifier: "u1",
        Roles:          []string{"ROLE_API"},
    })

    request := newSecurityTestRequest(
        nethttp.MethodGet,
        "/x",
        map[string]string{"X-Api-Key": "value"},
        nil,
    )

    if false == auth.Supports(request) {
        t.Fatalf("expected supports to be true")
    }
}

func TestApiKeyHeaderAuthenticator_AuthenticateReturnsAnonymousOnMismatch(t *testing.T) {
    auth := NewApiKeyHeaderAuthenticator("expected", ApiKeyHeaderAuthenticatorConfig{
        HeaderName:     "X-Api-Key",
        UserIdentifier: "u1",
        Roles:          []string{"ROLE_API"},
    })

    request := newSecurityTestRequest(
        nethttp.MethodGet,
        "/x",
        map[string]string{"X-Api-Key": "wrong"},
        nil,
    )

    token, err := auth.Authenticate(request.RuntimeInstance(), request)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    if true == token.IsAuthenticated() {
        t.Fatalf("expected anonymous token")
    }
}

func TestApiKeyHeaderAuthenticator_AuthenticateReturnsAuthenticatedOnMatch(t *testing.T) {
    auth := NewApiKeyHeaderAuthenticator("expected", ApiKeyHeaderAuthenticatorConfig{
        HeaderName:     "X-Api-Key",
        UserIdentifier: "u1",
        Roles:          []string{"ROLE_API"},
    })

    request := newSecurityTestRequest(
        nethttp.MethodGet,
        "/x",
        map[string]string{"X-Api-Key": "expected"},
        nil,
    )

    token, err := auth.Authenticate(request.RuntimeInstance(), request)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    if false == token.IsAuthenticated() {
        t.Fatalf("expected authenticated token")
    }
    if "u1" != token.UserIdentifier() {
        t.Fatalf("unexpected user identifier")
    }
    if 1 != len(token.Roles()) {
        t.Fatalf("unexpected roles")
    }
    if "ROLE_API" != token.Roles()[0] {
        t.Fatalf("unexpected role")
    }
}

func TestNewApiKeyHeaderAuthenticator_RefusesAnEmptyExpectedValueAndDefaultsTheHeaderName(t *testing.T) {
    cases := []struct {
        name               string
        expectedValue      string
        config             ApiKeyHeaderAuthenticatorConfig
        expectedArgument   string
        expectedHeaderName string
    }{
        {"an empty expected value", "", ApiKeyHeaderAuthenticatorConfig{HeaderName: "X-Api-Key"}, "expectedValue", ""},
        {"an empty header name", "secret", ApiKeyHeaderAuthenticatorConfig{}, "", DefaultApiKeyHeaderName},
        {"a declared header name", "secret", ApiKeyHeaderAuthenticatorConfig{HeaderName: "X-Service-Key"}, "", "X-Service-Key"},
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            if "" != testCase.expectedArgument {
                expectPanicNamingTheArgument(t, testCase.expectedArgument, func() {
                    NewApiKeyHeaderAuthenticator(testCase.expectedValue, testCase.config)
                })

                return
            }

            if authenticator := NewApiKeyHeaderAuthenticator(testCase.expectedValue, testCase.config); testCase.expectedHeaderName != authenticator.headerName {
                t.Fatalf("expected the header %q, got %q", testCase.expectedHeaderName, authenticator.headerName)
            }
        })
    }
}
