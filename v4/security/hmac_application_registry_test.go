package security

import (
    "testing"

    "github.com/precision-soft/melody/v4/exception"
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

func TestNewStaticHmacApplicationRegistry_RefusesAnEmptyApplicationName(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        _ = NewStaticHmacApplicationRegistry(map[string][]string{"": {"ROLE_SERVICE"}})
    }, "hmac application name is empty")
}

func TestStaticHmacApplicationRegistry_RolesForApplicationAnswersFalseForAnUnknownApplication(t *testing.T) {
    registry := NewStaticHmacApplicationRegistry(map[string][]string{"billing": {"ROLE_SERVICE"}})

    roles, exists, _ := registry.RolesForApplication(newTestRuntime(), "crm")
    if true == exists {
        t.Fatalf("expected an unknown application to be refused, got %v", roles)
    }

    if nil != roles {
        t.Fatalf("expected no roles for an unknown application, got %v", roles)
    }
}

func TestStaticHmacApplicationRegistry_OwnsItsRoles(t *testing.T) {
    callerRoles := []string{"ROLE_SERVICE"}
    registry := NewStaticHmacApplicationRegistry(map[string][]string{"billing": callerRoles})

    callerRoles[0] = "ROLE_ADMIN"

    roles, _, _ := registry.RolesForApplication(newTestRuntime(), "billing")
    if "ROLE_SERVICE" != roles[0] {
        t.Fatalf("expected the registry to keep its own copy, got %v", roles)
    }

    roles[0] = "ROLE_ADMIN"

    readAgain, _, _ := registry.RolesForApplication(newTestRuntime(), "billing")
    if "ROLE_SERVICE" != readAgain[0] {
        t.Fatalf("expected the registry to hand out a copy, got %v", readAgain)
    }
}

func TestStaticHmacApplicationRegistry_RefusesANilRuntime(t *testing.T) {
    if _, _, rolesErr := hmacTestApplications().RolesForApplication(nil, "wms-service"); "runtimeInstance" != exception.LogContext(rolesErr, exception.LogContextOptions{})["argument"] {
        t.Fatalf("expected a nil runtime refused naming the argument, got %v", rolesErr)
    }
}
