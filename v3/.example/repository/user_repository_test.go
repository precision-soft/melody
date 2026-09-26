package repository

import (
    "testing"

    "github.com/precision-soft/melody/v3/.example/entity"
)

func TestValidateUserNamesTheFirstFieldItFailsOn(t *testing.T) {
    if validationErr := validateUser(nil); nil == validationErr || "user is required" != validationErr.Error() {
        t.Fatalf("expected an absent user to be refused, got %v", validationErr)
    }

    if validationErr := validateUser(&entity.User{Username: " ", Password: " "}); nil == validationErr || "username is required" != validationErr.Error() {
        t.Fatalf("expected a blank username to be refused first, got %v", validationErr)
    }

    if validationErr := validateUser(&entity.User{Username: "admin", Password: " "}); nil == validationErr || "password hash is required" != validationErr.Error() {
        t.Fatalf("expected a blank password hash to be refused, got %v", validationErr)
    }

    if validationErr := validateUser(&entity.User{Username: "admin", Password: "hash"}); nil != validationErr {
        t.Fatalf("expected a complete user to pass, got %v", validationErr)
    }
}

func TestHoldsRoleMatchesTheExactRoleOnly(t *testing.T) {
    roles := []string{"ROLE_USER", "ROLE_EDITOR"}

    if false == holdsRole(roles, "ROLE_EDITOR") {
        t.Fatal("expected a held role to be found")
    }

    if true == holdsRole(roles, "ROLE_ADMIN") || true == holdsRole(roles, "role_user") {
        t.Fatal("expected a role the account does not carry, or another spelling of one, not to be found")
    }
}

func TestNormalizedUsernameFoldsCaseAndSurroundingSpace(t *testing.T) {
    if "admin" != NormalizedUsername("  Admin ") {
        t.Fatalf("expected admin, got %q", NormalizedUsername("  Admin "))
    }
}

func TestNextUserIdContinuesTheSeededNumbering(t *testing.T) {
    if "user-7" != nextUserId([]string{"user-2", "user-6", "user-1"}) {
        t.Fatalf("expected user-7, got %q", nextUserId([]string{"user-2", "user-6", "user-1"}))
    }
}
