package security

import (
    "testing"
)

/* @info the listener that reacts to a sign-in reads both fields off this event; each accessor is pinned to its OWN field, because a copy-paste between the four security events would be invisible to every other test in the package */
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
