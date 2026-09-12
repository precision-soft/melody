package security

import (
    "testing"

    "github.com/precision-soft/melody/v3/internal/testhelper"
)

func TestNewStaticHmacAppRegistry_RefusesAnEmptyRegistry(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        _ = NewStaticHmacAppRegistry(nil)
    }, "hmac app registry is empty")

    testhelper.AssertPanicsWithError(t, func() {
        _ = NewStaticHmacAppRegistry(map[string][]string{})
    }, "hmac app registry is empty")
}

/* an app with no name cannot be matched against an envelope's claimed app, so it would sit in the registry granting nothing while looking like a configured caller */
func TestNewStaticHmacAppRegistry_RefusesAnEmptyAppName(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        _ = NewStaticHmacAppRegistry(map[string][]string{"": {"ROLE_SERVICE"}})
    }, "hmac app name is empty")
}

func TestStaticHmacAppRegistry_RolesForAppAnswersFalseForAnUnknownApp(t *testing.T) {
    registry := NewStaticHmacAppRegistry(map[string][]string{"billing": {"ROLE_SERVICE"}})

    roles, exists := registry.RolesForApp("crm")
    if true == exists {
        t.Fatalf("expected an unknown app to be refused, got %v", roles)
    }

    if nil != roles {
        t.Fatalf("expected no roles for an unknown app, got %v", roles)
    }
}

/* the registry is the authorization table of every service principal: a caller that kept the slice it handed in, or that mutates the slice it was handed back, would be rewriting the roles of a live caller */
func TestStaticHmacAppRegistry_OwnsItsRoles(t *testing.T) {
    callerRoles := []string{"ROLE_SERVICE"}
    registry := NewStaticHmacAppRegistry(map[string][]string{"billing": callerRoles})

    callerRoles[0] = "ROLE_ADMIN"

    roles, _ := registry.RolesForApp("billing")
    if "ROLE_SERVICE" != roles[0] {
        t.Fatalf("expected the registry to keep its own copy, got %v", roles)
    }

    roles[0] = "ROLE_ADMIN"

    readAgain, _ := registry.RolesForApp("billing")
    if "ROLE_SERVICE" != readAgain[0] {
        t.Fatalf("expected the registry to hand out a copy, got %v", readAgain)
    }
}
