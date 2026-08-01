package security

import (
    "errors"
    "testing"
)

func TestLogoutFailureEvent_CarriesTheRequestAndTheError(t *testing.T) {
    request := newFirewallTestRequest("/logout")
    failureErr := errors.New("session store unreachable")

    eventValue := NewLogoutFailureEvent(request, failureErr)

    if request != eventValue.Request() {
        t.Fatalf("expected the request to be carried")
    }

    if false == errors.Is(eventValue.Error(), failureErr) {
        t.Fatalf("expected the failure reason to be carried, got %v", eventValue.Error())
    }
}
