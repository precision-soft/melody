package repository

import (
    "errors"
    "fmt"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/.example/migration"
    "github.com/precision-soft/melody/v3/exception"
)

/* the two lookup doors are read as the dialect renders them rather than run against a database: what has to be pinned is which rows they may match, and that lives entirely in the where clause. The column's own collation (utf8mb4_0900_ai_ci) folds accents, 'café' = 'cafe', while NormalizedUsername, the one spelling the cache keys and the invalidation listeners agree on, folds case alone, so under the column's collation an account reachable through an accent-folded spelling is cached under a key the invalidation cannot address, and a revoked password keeps authenticating from the entry, which has no expiry. */
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
    /* the shape production produces: the audit tracker's own exception, whose message says nothing about the index, with the driver's refusal as its cause, so the seam has to find the index's name below the top of the chain */
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

/* mysql renders the duplicated value before the key clause and does not escape it, and the admin doors admit quotes in a name, so a username that carries the clause's own spelling would put a first-clause reader onto the value: the refusal would stay the driver's and the door would answer 500 over a collision it answers 400. The clause is the tail of the message, and the second row spells a whole clause of another index inside the value. */
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

/* selfWrappingError is a chain that closes on itself through Unwrap, and selfJoiningError one that closes on itself through both branches of a join: neither is a shape the driver produces, but the walk reads whatever error a write returns, and an unbounded walk over a cycle exhausts the goroutine stack, a fatal error no recover turns into a response. */
type selfWrappingError struct {
    message string
}

func (instance *selfWrappingError) Error() string {
    return instance.message
}

func (instance *selfWrappingError) Unwrap() error {
    return instance
}

type selfJoiningError struct {
    message string
}

func (instance *selfJoiningError) Error() string {
    return instance.message
}

func (instance *selfJoiningError) Unwrap() []error {
    return []error{instance, instance}
}

func TestAsUsernameAlreadyExists_EndsOnAChainThatClosesOnItself(t *testing.T) {
    for _, writeErr := range []error{
        &selfWrappingError{message: "connection reset"},
        &selfJoiningError{message: "connection reset"},
    } {
        if translated := asUsernameAlreadyExists(writeErr); writeErr != translated {
            t.Errorf("a cyclic %T came back as %v, wanted it unchanged", writeErr, translated)
        }
    }

    /* the bound is on the walk, not on the diagnosis: a refusal three links down is still read */
    driverRefusal := fmt.Errorf(
        "Error 1062 (23000): Duplicate entry 'zzprobe' for key 'melody_example_v3_user.%s'",
        migration.UserUsernameIndexName,
    )
    deep := exception.NewError("audited insert failed", nil, fmt.Errorf("tracked: %w", errors.Join(errors.New("other"), driverRefusal)))

    if translated := asUsernameAlreadyExists(deep); false == errors.Is(translated, ErrUsernameAlreadyExists) {
        t.Errorf("a refusal three links down came back as %v", translated)
    }
}
