package security

import (
    "errors"
    "testing"
)

func TestLoginFailureEvent_CarriesTheRequestAndTheError(t *testing.T) {
    request := newFirewallTestRequest("/login")
    failureErr := errors.New("invalid credentials")

    eventValue := NewLoginFailureEvent(request, failureErr)

    if request != eventValue.Request() {
        t.Fatalf("expected the request to be carried")
    }

    if false == errors.Is(eventValue.Error(), failureErr) {
        t.Fatalf("expected the failure reason to be carried, got %v", eventValue.Error())
    }
}
