package twofactor

import (
    "strings"
    "testing"
    "time"

    melodyencrypt "github.com/precision-soft/melody/integrations/bunorm/v3/encrypt"
)

/* the write is read as the dialect renders it: what has to be pinned is the clause that decides what a
   SECOND enrollment does, and that lives entirely in the statement.

   A plain insert made the enrollment permanent. The primary key is the user identifier, so an account
   whose authenticator was lost stayed bound to its first secret for good and the second attempt surfaced
   the key as an opaque 500 — no door at all for the person holding the lost device. Replacing is only safe
   because the caller no longer names the account: the handler takes the identifier from the authenticated
   token, so the row this overwrites is always the caller's own. */
func renderedEnrollmentUpsert(t *testing.T) string {
    t.Helper()

    store := &Store{database: newRenderingDatabase()}

    return store.enrollmentUpsert(&Enrollment{
        UserIdentifier: "user-2",
        Secret:         melodyencrypt.EncryptedString("zz-secret"),
        RecoveryCodes:  melodyencrypt.EncryptedString(`["zz-one"]`),
        CreatedAt:      time.Unix(1, 0),
    }).String()
}

func TestEnrollmentUpsertReplacesAnEnrollmentThatIsAlreadyThere(t *testing.T) {
    rendered := renderedEnrollmentUpsert(t)

    if false == strings.Contains(rendered, " ON DUPLICATE KEY UPDATE secret = VALUES(secret)") {
        t.Fatalf(
            "expected a second enrollment to replace the first rather than collide with the primary key, in the form mysql accepts; got %q",
            rendered,
        )
    }

    /* the form bun renders for a dialect without the feature — mysql refuses it */
    if true == strings.Contains(rendered, "UPDATE SET") {
        t.Fatalf("expected the mysql upsert form, got the generic one mysql refuses: %q", rendered)
    }
}

/* the secret alone is not the enrollment. The recovery codes are minted beside it and open the account on
   their own, so a set left from the previous enrollment would keep letting whoever holds the old device in
   after the factor it belongs to was replaced — which is the whole reason for replacing it. */
func TestEnrollmentUpsertReplacesTheRecoveryCodesWithTheSecret(t *testing.T) {
    rendered := renderedEnrollmentUpsert(t)

    for _, column := range []string{"secret = VALUES(secret)", "recovery_codes = VALUES(recovery_codes)", "created_at = VALUES(created_at)"} {
        if false == strings.Contains(rendered, column) {
            t.Fatalf("expected the replacement to write %s, got %q", column, rendered)
        }
    }
}

/* the deletion is keyed on the account identifier and on nothing wider: a statement without the predicate would release every account's factor when one account is deleted. */
func TestEnrollmentDeleteIsKeyedOnTheAccountIdentifierAlone(t *testing.T) {
    rendered := NewStore(newRenderingDatabase()).enrollmentDelete("user-4").String()

    if false == strings.HasPrefix(rendered, "DELETE FROM `melody_example_v3_two_factor`") || false == strings.Contains(rendered, "WHERE (user_identifier = 'user-4')") {
        t.Fatalf("expected the enrollment of user-4 alone to be deleted, got %q", rendered)
    }
}
