package repository

import (
    "errors"
    "fmt"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/.example/migration"
    "github.com/precision-soft/melody/v3/exception"
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
    /* the shape production produces: the audit tracker's own exception, whose message says nothing about
       the index, with the driver's refusal as its cause — the previous form of this test flattened the two
       into one text and stayed green over a seam that never saw the index's name */
    driverRefusal := fmt.Errorf(
        "Error 1062 (23000): Duplicate entry 'zzprobe' for key 'melody_example_v3_user.%s'",
        migration.UserUsernameIndexName,
    )
    refusal := exception.NewError("audited insert failed", nil, driverRefusal)

    translated := asUsernameAlreadyExists(refusal)
    if nil == translated {
        t.Fatalf("expected the refusal to survive as an error")
    }

    if false == errors.Is(translated, ErrUsernameAlreadyExists) {
        t.Fatalf("expected the door's own refusal, got %q", translated.Error())
    }

    if "username already exists" != translated.Error() {
        t.Fatalf("expected the door's own message, got %q", translated.Error())
    }
}

/* mysql renders the duplicated value BEFORE the key clause and does not escape it, so a username that carries the
   clause's own spelling — the admin doors admit quotes in a name, and a rename onto `for key 'z'` was measured
   live — put a first-clause reader onto the value: the key it read was the value's, the refusal stayed the
   driver's and the door answered 500 over a collision it answers 400. The clause is the tail of the message,
   and the second row spells a whole clause of ANOTHER index inside the value. */
func TestAsUsernameAlreadyExists_ReadsTheKeyClauseAtTheTailOfTheMessage(t *testing.T) {
    for _, value := range []string{
        "for key 'z'",
        "for key 'melody_example_v3_user.PRIMARY'",
    } {
        collision := exception.NewError(
            "audited update failed",
            nil,
            fmt.Errorf("Error 1062 (23000): Duplicate entry '%s' for key 'melody_example_v3_user.%s'", value, migration.UserUsernameIndexName),
        )

        if false == errors.Is(asUsernameAlreadyExists(collision), ErrUsernameAlreadyExists) {
            t.Fatalf("expected the collision on the username index to be read past the value %q, got %v", value, asUsernameAlreadyExists(collision))
        }
    }
}

func TestAsUsernameAlreadyExists_LeavesEveryOtherFailureAlone(t *testing.T) {
    primaryKey := exception.NewError(
        "audited insert failed",
        nil,
        fmt.Errorf("Error 1062 (23000): Duplicate entry 'user-7' for key 'melody_example_v3_user.PRIMARY'"),
    )

    if primaryKey != asUsernameAlreadyExists(primaryKey) {
        t.Fatalf("expected a duplicate identifier to stay the diagnosis it is, got %q", asUsernameAlreadyExists(primaryKey))
    }

    /* the index is read out of the key clause, not searched for in the whole text: an identifier that happens to spell the index's name is still a duplicate IDENTIFIER */
    spelledLikeTheIndex := exception.NewError(
        "audited insert failed",
        nil,
        fmt.Errorf("Error 1062 (23000): Duplicate entry '%s' for key 'melody_example_v3_user.PRIMARY'", migration.UserUsernameIndexName),
    )

    if spelledLikeTheIndex != asUsernameAlreadyExists(spelledLikeTheIndex) {
        t.Fatalf("expected a duplicate identifier spelled like the index to stay the diagnosis it is, got %q", asUsernameAlreadyExists(spelledLikeTheIndex))
    }

    if nil != asUsernameAlreadyExists(nil) {
        t.Fatalf("expected a write that did not fail to stay unreported")
    }
}
