package security

import (
    "testing"
)

func TestLoginSuccessEvent_CarriesTheRequestAndTheToken(t *testing.T) {
    request := newFirewallTestRequest("/login")
    token := NewAuthenticatedToken("u1", []string{"ROLE_USER"})

    eventValue := NewLoginSuccessEvent(request, token)

    if request != eventValue.Request() {
        t.Fatalf("expected the request to be carried")
    }

    if token != eventValue.Token() {
        t.Fatalf("expected the token to be carried")
    }
}
