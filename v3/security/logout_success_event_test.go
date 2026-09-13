package security

import (
    "testing"
)

func TestLogoutSuccessEvent_CarriesTheRequest(t *testing.T) {
    request := newFirewallTestRequest("/logout")

    eventValue := NewLogoutSuccessEvent(request)

    if request != eventValue.Request() {
        t.Fatalf("expected the request to be carried")
    }
}
