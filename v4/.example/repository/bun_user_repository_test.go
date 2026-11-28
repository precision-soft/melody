package repository

import (
    "context"
    "database/sql/driver"
    "errors"
    "fmt"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v4/.example/entity"
    "github.com/precision-soft/melody/v4/.example/migration"
    "github.com/precision-soft/melody/v4/.example/persistence"
    "github.com/precision-soft/melody/v4/exception"
)

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

/* Go folds the Georgian Mtavruli capitals onto Mkhedruli and MySQL's LOWER() leaves them */
func TestNewUserRow_WritesTheNameAsNormalizedUsernameFoldsIt(t *testing.T) {
    row := newUserRow(entity.NewUser("user-1", "ᲐᲜᲐ", "hash", []string{entity.RoleUser}))

    if "ანა" != row.UsernameNormalized || NormalizedUsername("ანა") != row.UsernameNormalized {
        t.Fatalf("the folded column holds %q; wanted Go's folding, %q", row.UsernameNormalized, NormalizedUsername("ანა"))
    }

    if "ᲐᲜᲐ" != row.Username {
        t.Fatalf("the name as spelled was rewritten to %q", row.Username)
    }
}

func TestAsUsernameAlreadyExists_TranslatesOnlyTheUsernameIndexRefusal(t *testing.T) {
    driverRefusal := fmt.Errorf(
        "Error 1062 (23000): Duplicate entry 'zzprobe' for key 'melody_example_v4_user.%s'",
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

func TestAsUsernameAlreadyExists_ReadsTheKeyClauseAtTheTailOfTheMessage(t *testing.T) {
    for _, value := range []string{
        "for key 'z'",
        "for key 'melody_example_v4_user.PRIMARY'",
    } {
        collision := exception.NewError(
            "audited update failed",
            nil,
            fmt.Errorf("Error 1062 (23000): Duplicate entry '%s' for key 'melody_example_v4_user.%s'", value, migration.UserUsernameIndexName),
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
        fmt.Errorf("Error 1062 (23000): Duplicate entry 'user-7' for key 'melody_example_v4_user.PRIMARY'"),
    )

    if primaryKey != asUsernameAlreadyExists(primaryKey) {
        t.Fatalf("expected a duplicate identifier to stay the diagnosis it is, got %q", asUsernameAlreadyExists(primaryKey))
    }

    spelledLikeTheIndex := exception.NewError(
        "audited insert failed",
        nil,
        fmt.Errorf("Error 1062 (23000): Duplicate entry '%s' for key 'melody_example_v4_user.PRIMARY'", migration.UserUsernameIndexName),
    )

    if spelledLikeTheIndex != asUsernameAlreadyExists(spelledLikeTheIndex) {
        t.Fatalf("expected a duplicate identifier spelled like the index to stay the diagnosis it is, got %q", asUsernameAlreadyExists(spelledLikeTheIndex))
    }

    if nil != asUsernameAlreadyExists(nil) {
        t.Fatalf("expected a write that did not fail to stay unreported")
    }
}

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

    driverRefusal := fmt.Errorf(
        "Error 1062 (23000): Duplicate entry 'zzprobe' for key 'melody_example_v4_user.%s'",
        migration.UserUsernameIndexName,
    )
    deep := exception.NewError("audited insert failed", nil, fmt.Errorf("tracked: %w", errors.Join(errors.New("other"), driverRefusal)))

    if translated := asUsernameAlreadyExists(deep); false == errors.Is(translated, ErrUsernameAlreadyExists) {
        t.Errorf("a refusal three links down came back as %v", translated)
    }
}

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
    return true == strings.HasPrefix(query, "UPDATE") && true == strings.Contains(query, "melody_example_v4_user")
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
        return true == strings.HasPrefix(query, "DELETE") && true == strings.Contains(query, "melody_example_v4_user")
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
