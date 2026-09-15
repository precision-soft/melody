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
