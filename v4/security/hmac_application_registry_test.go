package security

import (
    "testing"

    "github.com/precision-soft/melody/v4/internal/testhelper"
)

func TestNewStaticHmacApplicationRegistry_RefusesAnEmptyRegistry(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        _ = NewStaticHmacApplicationRegistry(nil)
    }, "hmac application registry is empty")

    testhelper.AssertPanicsWithError(t, func() {
        _ = NewStaticHmacApplicationRegistry(map[string][]string{})
    }, "hmac application registry is empty")
}

/* an application with no name cannot be matched against an envelope's claimed application, so it would sit in the registry granting nothing while looking like a configured caller */
func TestNewStaticHmacApplicationRegistry_RefusesAnEmptyApplicationName(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        _ = NewStaticHmacApplicationRegistry(map[string][]string{"": {"ROLE_SERVICE"}})
    }, "hmac application name is empty")
}

func TestStaticHmacApplicationRegistry_RolesForApplicationAnswersFalseForAnUnknownApplication(t *testing.T) {
    registry := NewStaticHmacApplicationRegistry(map[string][]string{"billing": {"ROLE_SERVICE"}})

    roles, exists := registry.RolesForApplication("crm")
    if true == exists {
        t.Fatalf("expected an unknown application to be refused, got %v", roles)
    }

    if nil != roles {
        t.Fatalf("expected no roles for an unknown application, got %v", roles)
    }
}

/* the registry is the authorization table of every service principal: a caller that kept the slice it handed in, or mutates the slice it is handed back, must not rewrite the roles of a live caller */
func TestStaticHmacApplicationRegistry_OwnsItsRoles(t *testing.T) {
    callerRoles := []string{"ROLE_SERVICE"}
    registry := NewStaticHmacApplicationRegistry(map[string][]string{"billing": callerRoles})

    callerRoles[0] = "ROLE_ADMIN"

    roles, _ := registry.RolesForApplication("billing")
    if "ROLE_SERVICE" != roles[0] {
        t.Fatalf("expected the registry to keep its own copy, got %v", roles)
    }

    roles[0] = "ROLE_ADMIN"

    readAgain, _ := registry.RolesForApplication("billing")
    if "ROLE_SERVICE" != readAgain[0] {
        t.Fatalf("expected the registry to hand out a copy, got %v", readAgain)
    }
}
