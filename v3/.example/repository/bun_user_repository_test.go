package repository

import (
    "context"
    "database/sql/driver"
    "errors"
    "fmt"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/.example/entity"
    "github.com/precision-soft/melody/v3/.example/migration"
    "github.com/precision-soft/melody/v3/.example/persistence"
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

    return usernameTakenByAnotherQuery(newRenderingDatabase(), wanted, excludedId).String()
}

func TestUserByUsernameQuery_ComparesTheColumnTheApplicationFolded(t *testing.T) {
    rendered := renderedUserByUsernameQuery(t, "café")

    if false == strings.Contains(rendered, "username_normalized = 'café'") {
        t.Fatalf("expected the lookup to compare the folded column, got %q", rendered)
    }

    if true == strings.Contains(rendered, "LOWER(") {
        t.Fatalf("the lookup folds in the database again, where LOWER() and NormalizedUsername disagree: %q", rendered)
    }
}

func TestUsernameTakenByAnotherQuery_ComparesTheColumnTheApplicationFolded(t *testing.T) {
    rendered := renderedUsernameTakenByAnotherQuery(t, "café", "user-1")

    if false == strings.Contains(rendered, "username_normalized = 'café'") {
        t.Fatalf("expected the uniqueness door to compare the same column as the lookup; got %q", rendered)
    }

    if false == strings.Contains(rendered, "id != ") {
        t.Fatalf("expected the uniqueness door to exclude the account being updated, got %q", rendered)
    }
}

/* Go folds the Georgian Mtavruli capitals onto Mkhedruli and MySQL's LOWER() leaves them, so a key on LOWER(username) let 'ᲐᲜᲐ' stand beside 'ანა' while the cache and the lookup held them one name: the folded column is written by the application, in Go's spelling */
func TestNewUserRow_WritesTheNameAsNormalizedUsernameFoldsIt(t *testing.T) {
    row := newUserRow(entity.NewUser("user-1", "ᲐᲜᲐ", "hash", []string{entity.RoleUser}))

    if "ანა" != row.UsernameNormalized || NormalizedUsername("ანა") != row.UsernameNormalized {
        t.Fatalf("the folded column holds %q; wanted Go's folding, %q", row.UsernameNormalized, NormalizedUsername("ანა"))
    }

    if "ᲐᲜᲐ" != row.Username {
        t.Fatalf("the name as spelled was rewritten to %q", row.Username)
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

/* lockedUserRows answers the locked read of the update and delete doors with the account as the directory holds it, and every count with zero */
func lockedUserRows(id string, username string, password string, roles string) func(query string) ([]string, [][]driver.Value, error) {
    return func(query string) ([]string, [][]driver.Value, error) {
        if true == strings.Contains(query, "count(*)") {
            return []string{"count"}, [][]driver.Value{{int64(0)}}, nil
        }

        if true == strings.Contains(query, "FOR UPDATE") {
            return []string{"id", "username", "password", "roles"}, [][]driver.Value{{id, username, password, roles}}, nil
        }

        return []string{}, nil, nil
    }
}

func isUserUpdate(query string) bool {
    return true == strings.HasPrefix(query, "UPDATE") && true == strings.Contains(query, "melody_example_v3_user")
}

func TestBunUserRepositoryUpdate_WritesTheChangeOverTheRowItLockedAndNothingElse(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = lockedUserRows("user-7", "alice", "H2", "ROLE_USER")
    repositoryInstance := newBunUserRepository(persistence.NewCatalogStorage(database))

    renamed := "alice-renamed"
    before, after, updateErr := repositoryInstance.Update(context.Background(), "user-7", UserChange{Username: &renamed}, nil)
    if nil != updateErr || nil == before {
        t.Fatalf("the update answered before=%v err=%v", before, updateErr)
    }

    if "H2" != after.Password || 1 != len(after.Roles) || entity.RoleUser != after.Roles[0] || renamed != after.Username {
        t.Fatalf("the update answered %+v; wanted the locked row's password and roles under the new name", after)
    }

    update := recorder.firstMatching(isUserUpdate)
    if false == strings.Contains(update, "'H2'") || false == strings.Contains(update, "'ROLE_USER'") || false == strings.Contains(update, "'"+renamed+"'") {
        t.Fatalf("the written row did not come from the locked read: %q", update)
    }

    queries := recorder.recordedQueries()
    lockAt, updateAt, commitAt := -1, -1, -1
    for index, query := range queries {
        switch {
        case true == strings.Contains(query, "FOR UPDATE") && -1 == lockAt:
            lockAt = index
        case true == isUserUpdate(query) && -1 == updateAt:
            updateAt = index
        case "COMMIT" == query:
            commitAt = index
        }
    }

    if false == (0 <= lockAt && lockAt < updateAt && updateAt < commitAt) {
        t.Fatalf("the read was not locked ahead of the write inside one transaction: %q", queries)
    }
}

func TestBunUserRepositoryUpdate_AGuardRefusalWritesNothing(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = lockedUserRows("admin-2", "admin-2", "H2", "ROLE_USER,ROLE_ADMIN")
    repositoryInstance := newBunUserRepository(persistence.NewCatalogStorage(database))

    refusal := errors.New("refused by the guard")
    var guarded *entity.User

    renamed := "taken-over"
    _, _, updateErr := repositoryInstance.Update(context.Background(), "admin-2", UserChange{Username: &renamed}, func(current *entity.User) error {
        guarded = current

        return refusal
    })
    if false == errors.Is(updateErr, refusal) {
        t.Fatalf("the update answered %v; wanted the guard's refusal", updateErr)
    }

    if nil == guarded || false == holdsRole(guarded.Roles, entity.RoleAdmin) {
        t.Fatalf("the guard was not asked about the locked row, got %+v", guarded)
    }

    if 0 != recorder.countMatching(isUserUpdate) {
        t.Fatalf("a refused update still wrote: %q", recorder.recordedQueries())
    }
}

func TestBunUserRepositoryDeleteById_AGuardRefusalRemovesNothing(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = lockedUserRows("admin-2", "admin-2", "H2", "ROLE_USER,ROLE_ADMIN")
    repositoryInstance := newBunUserRepository(persistence.NewCatalogStorage(database))

    refusal := errors.New("refused by the guard")

    _, deleteErr := repositoryInstance.DeleteById(context.Background(), "admin-2", func(current *entity.User) error {
        return refusal
    })
    if false == errors.Is(deleteErr, refusal) {
        t.Fatalf("the delete answered %v; wanted the guard's refusal", deleteErr)
    }

    isDelete := func(query string) bool {
        return true == strings.HasPrefix(query, "DELETE") && true == strings.Contains(query, "melody_example_v3_user")
    }
    if 0 != recorder.countMatching(isDelete) {
        t.Fatalf("a refused delete still removed: %q", recorder.recordedQueries())
    }

    recorder.queryHook = lockedUserRows("user-7", "alice", "H2", "ROLE_USER")
    removed, deleteErr := repositoryInstance.DeleteById(context.Background(), "user-7", nil)
    if nil != deleteErr || nil == removed || "alice" != removed.Username {
        t.Fatalf("the admitted delete answered removed=%+v err=%v", removed, deleteErr)
    }

    if 1 != recorder.countMatching(isDelete) {
        t.Fatalf("the admitted delete issued %d deletes: %q", recorder.countMatching(isDelete), recorder.recordedQueries())
    }
}
