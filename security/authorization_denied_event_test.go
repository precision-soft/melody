package security

import (
    "errors"
    "testing"
)

/* @info the denial event carries the attributes that were refused AND the decision error; a listener that alerts on refusals reads all three, and the sibling granted event deliberately carries no error */
func TestAuthorizationDeniedEvent_CarriesTheRequestAttributesAndError(t *testing.T) {
    request := newFirewallTestRequest("/admin")
    decisionErr := errors.New("forbidden")

    eventValue := NewAuthorizationDeniedEvent(request, []string{"ROLE_ADMIN"}, decisionErr)

    if request != eventValue.Request() {
        t.Fatalf("expected the request to be carried")
    }

    if 1 != len(eventValue.Attributes()) || "ROLE_ADMIN" != eventValue.Attributes()[0] {
        t.Fatalf("expected the refused attributes to be carried, got %v", eventValue.Attributes())
    }

    if false == errors.Is(eventValue.Err(), decisionErr) {
        t.Fatalf("expected the decision error to be carried, got %v", eventValue.Err())
    }
}

func TestAuthorizationDeniedEvent_OwnsItsAttributes(t *testing.T) {
    callerAttributes := []string{"ROLE_ADMIN"}

    eventValue := NewAuthorizationDeniedEvent(newFirewallTestRequest("/admin"), callerAttributes, nil)

    callerAttributes[0] = "ROLE_USER"
    if "ROLE_ADMIN" != eventValue.Attributes()[0] {
        t.Fatalf("expected the event to keep its own copy, got %v", eventValue.Attributes())
    }

    readAttributes := eventValue.Attributes()
    readAttributes[0] = "ROLE_USER"
    if "ROLE_ADMIN" != eventValue.Attributes()[0] {
        t.Fatalf("expected the event to hand out a copy, got %v", eventValue.Attributes())
    }
}
