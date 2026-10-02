package repository

import (
    "context"
    "testing"

    "github.com/precision-soft/melody/v3/.example/entity"
    "github.com/precision-soft/melody/v3/.example/persistence"
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

/* the example's accounts sign in with their names as passwords: a directory on a handle nobody marked starts empty, in memory and in the database, and one marked by the development composition root starts from the three */
func TestNewUserRepository_SeedsTheAccountsOnlyOnAMarkedStorage(t *testing.T) {
    unmarked, unmarkedErr := NewUserRepository(persistence.NewCatalogStorage(nil))
    if nil != unmarkedErr {
        t.Fatalf("build the unmarked repository: %v", unmarkedErr)
    }

    if users, _ := unmarked.All(context.Background()); 0 != len(users) {
        t.Fatalf("expected an unmarked in-memory directory empty, got %d accounts", len(users))
    }

    marked, markedErr := NewUserRepository(persistence.NewCatalogStorage(nil).WithAccountSeed())
    if nil != markedErr {
        t.Fatalf("build the marked repository: %v", markedErr)
    }

    if users, _ := marked.All(context.Background()); 3 != len(users) {
        t.Fatalf("expected a marked in-memory directory to hold the three accounts, got %d", len(users))
    }

    database, recorder := newFakeBunDatabase()
    if seedErr := newBunUserRepository(persistence.NewCatalogStorage(database)).seedIfEmpty(context.Background()); nil != seedErr {
        t.Fatalf("seed an unmarked table: %v", seedErr)
    }

    if 0 != len(recorder.queries) {
        t.Fatalf("expected an unmarked table left untouched, got %q", recorder.queries)
    }
}
