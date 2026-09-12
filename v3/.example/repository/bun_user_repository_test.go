package repository

import (
    "fmt"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/.example/migration"
)

/* the two lookup doors are read as the dialect renders them rather than run against a database: what has
   to be pinned is which rows they may match, and that lives entirely in the where clause.

   Left to the column's own collation (utf8mb4_0900_ai_ci) the comparison folds accents — 'café' = 'cafe'
   is true under it — while NormalizedUsername, the one spelling the cache keys and the invalidation
   listeners agree on, folds case alone. Measured on the running stack before the repair: an account
   reachable through the collation-only spelling was cached under a key the invalidation could never
   address, so a REVOKED password went on authenticating through that spelling with the roles it had been
   stripped of, for as long as the entry lived — which, the positive being kept without expiry, is until
   the cache is cleared. The frozen majors have carried this clause since they were sealed. */
func renderedUserByUsernameQuery(t *testing.T, wanted string) string {
    t.Helper()

    repositoryInstance := &bunUserRepository{database: newRenderingDatabase()}

    return repositoryInstance.userByUsernameQuery(&userRow{}, wanted).String()
}

func renderedUsernameTakenByAnotherQuery(t *testing.T, wanted string, excludedId string) string {
    t.Helper()

    repositoryInstance := &bunUserRepository{database: newRenderingDatabase()}

    return repositoryInstance.usernameTakenByAnotherQuery(wanted, excludedId).String()
}

func TestUserByUsernameQuery_ComparesOnTheBinaryCollation(t *testing.T) {
    rendered := renderedUserByUsernameQuery(t, "café")

    if false == strings.Contains(rendered, "COLLATE utf8mb4_bin") {
        t.Fatalf(
            "expected the lookup to force the comparison onto the binary collation, so an accented spelling is a different name; got %q",
            rendered,
        )
    }

    if false == strings.Contains(rendered, "LOWER(username) = (") {
        t.Fatalf("expected the lookup to fold case the way NormalizedUsername does, got %q", rendered)
    }
}

func TestUsernameTakenByAnotherQuery_ComparesOnTheBinaryCollation(t *testing.T) {
    rendered := renderedUsernameTakenByAnotherQuery(t, "café", "user-1")

    if false == strings.Contains(rendered, "COLLATE utf8mb4_bin") {
        t.Fatalf(
            "expected the uniqueness door to admit and refuse exactly the spellings the lookup does; got %q",
            rendered,
        )
    }

    if false == strings.Contains(rendered, "id != ") {
        t.Fatalf("expected the uniqueness door to exclude the account being updated, got %q", rendered)
    }
}

/* the check that precedes the insert is a read, so it cannot hold a name against a caller that passed it
   at the same moment; the unique index the migration set adds is what holds it, and this is the seam that
   turns its refusal into the message the door already answers when the read catches the name in time.

   The match has to be on the index's own name and nothing wider: a duplicate on the PRIMARY key is a
   different diagnosis — an identifier collision — and reporting it as a taken username would send the
   caller to rename an account whose name was never the problem. */
func TestAsUsernameAlreadyExists_TranslatesOnlyTheUsernameIndexRefusal(t *testing.T) {
    refusal := fmt.Errorf(
        "audited insert failed: Error 1062 (23000): Duplicate entry 'zzprobe' for key 'melody_example_v3_user.%s'",
        migration.UserUsernameIndexName,
    )

    translated := asUsernameAlreadyExists(refusal)
    if nil == translated {
        t.Fatalf("expected the refusal to survive as an error")
    }

    if "username already exists" != translated.Error() {
        t.Fatalf("expected the door's own message, got %q", translated.Error())
    }
}

func TestAsUsernameAlreadyExists_LeavesEveryOtherFailureAlone(t *testing.T) {
    primaryKey := fmt.Errorf("audited insert failed: Error 1062 (23000): Duplicate entry 'user-7' for key 'melody_example_v3_user.PRIMARY'")

    if primaryKey != asUsernameAlreadyExists(primaryKey) {
        t.Fatalf("expected a duplicate identifier to stay the diagnosis it is, got %q", asUsernameAlreadyExists(primaryKey))
    }

    if nil != asUsernameAlreadyExists(nil) {
        t.Fatalf("expected a write that did not fail to stay unreported")
    }
}
