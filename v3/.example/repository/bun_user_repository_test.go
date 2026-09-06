package repository

import (
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/.example/entity"
)

/* the statement is read as the dialect renders it rather than run against a database: what has to be pinned
   is which rows the repair may touch, and that lives entirely in the two where clauses. The behaviour on
   real rows was measured against the live mysql of the development stack — the three seeded accounts
   rewritten from a planted sha256 digest, an account the operator created left as it was. */
func renderedLegacyPasswordUpdate(t *testing.T, user *entity.User) string {
    t.Helper()

    repositoryInstance := &bunUserRepository{database: newRenderingDatabase()}

    return repositoryInstance.legacyPasswordUpdate(user).String()
}

/* the repair exists because bcrypt refuses a value it cannot read, so an account seeded by a build older
   than the move to bcrypt could never sign in again — but it must reach only the accounts this application
   itself seeded and only while they still hold such a value, or every boot would undo an administrator's
   password change. */
func TestLegacyPasswordUpdate_TouchesOnlyTheSeededRowThatBcryptCannotRead(t *testing.T) {
    rendered := renderedLegacyPasswordUpdate(t, entity.NewUser("user-3", "admin", "$2a$10$freshly-hashed", []string{entity.RoleAdmin}))

    if false == strings.Contains(rendered, "melody_example_v3_user") {
        t.Fatalf("expected the repair to update the directory table, got %q", rendered)
    }

    if false == strings.Contains(rendered, "'user-3'") {
        t.Fatalf("expected the repair to name the seeded account it rewrites, got %q", rendered)
    }

    if false == strings.Contains(rendered, "NOT LIKE '$%'") {
        t.Fatalf(
            "expected the repair to spare a password bcrypt can already read, but the statement carries no such guard: %q",
            rendered,
        )
    }

    if false == strings.Contains(rendered, "$2a$10$freshly-hashed") {
        t.Fatalf("expected the repair to write the seed's own digest, got %q", rendered)
    }
}

/* the pattern is what tells a stored bcrypt digest from a value written before this application used one;
   spelled wrong it either spares every row, leaving the accounts locked out, or spares none and rewrites a
   changed password on every boot. */
func TestBcryptDigestPatternMatchesADigestAndNotALegacyOne(t *testing.T) {
    if false == strings.HasPrefix("$2a$10$abcdefghijklmnopqrstuv", strings.TrimSuffix(bcryptDigestPattern, "%")) {
        t.Fatalf("expected a bcrypt digest to carry the pattern's prefix")
    }

    if true == strings.HasPrefix("8c6976e5b5410415bde908bd4dee15df", strings.TrimSuffix(bcryptDigestPattern, "%")) {
        t.Fatalf("expected a legacy sha256 digest not to carry the pattern's prefix")
    }
}
