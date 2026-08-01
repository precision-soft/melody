package security

import (
    "testing"
)

func TestAuthorizationGrantedEvent_CarriesTheRequestAndTheAttributes(t *testing.T) {
    request := newFirewallTestRequest("/admin")

    eventValue := NewAuthorizationGrantedEvent(request, []string{"ROLE_ADMIN"})

    if request != eventValue.Request() {
        t.Fatalf("expected the request to be carried")
    }

    if 1 != len(eventValue.Attributes()) || "ROLE_ADMIN" != eventValue.Attributes()[0] {
        t.Fatalf("expected the attributes to be carried, got %v", eventValue.Attributes())
    }
}

/* @info the event owns its attribute list on both sides: a listener editing what it read must not reach the decision that was recorded, nor the caller's slice */
func TestAuthorizationGrantedEvent_OwnsItsAttributes(t *testing.T) {
    callerAttributes := []string{"ROLE_ADMIN"}

    eventValue := NewAuthorizationGrantedEvent(newFirewallTestRequest("/admin"), callerAttributes)

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
