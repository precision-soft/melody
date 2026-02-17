package security

import (
	nethttp "net/http"
	"testing"
)

func TestApiKeyHeaderAuthenticator_SupportsReturnsFalseWhenHeaderMissing(t *testing.T) {
	auth := NewApiKeyHeaderAuthenticator("X-Api-Key", "expected", "u1", []string{"ROLE_API"})

	request := newSecurityTestRequest(nethttp.MethodGet, "/x", map[string]string{}, nil)

	if true == auth.Supports(request) {
		t.Fatalf("expected supports to be false")
	}
}

func TestApiKeyHeaderAuthenticator_SupportsReturnsTrueWhenHeaderPresent(t *testing.T) {
	auth := NewApiKeyHeaderAuthenticator("X-Api-Key", "expected", "u1", []string{"ROLE_API"})

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
	auth := NewApiKeyHeaderAuthenticator("X-Api-Key", "expected", "u1", []string{"ROLE_API"})

	request := newSecurityTestRequest(
		nethttp.MethodGet,
		"/x",
		map[string]string{"X-Api-Key": "wrong"},
		nil,
	)

	token, err := auth.Authenticate(request)
	if nil != err {
		t.Fatalf("unexpected error: %v", err)
	}
	if true == token.IsAuthenticated() {
		t.Fatalf("expected anonymous token")
	}
}

func TestApiKeyHeaderAuthenticator_AuthenticateReturnsAuthenticatedOnMatch(t *testing.T) {
	auth := NewApiKeyHeaderAuthenticator("X-Api-Key", "expected", "u1", []string{"ROLE_API"})

	request := newSecurityTestRequest(
		nethttp.MethodGet,
		"/x",
		map[string]string{"X-Api-Key": "expected"},
		nil,
	)

	token, err := auth.Authenticate(request)
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
