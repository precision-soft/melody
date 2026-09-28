package repository

import (
    "context"
    "database/sql/driver"
    "errors"
    "fmt"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v2/.example/entity"
    "github.com/precision-soft/melody/v2/.example/migration"
)

/* the fake driver records the exact SQL bun emits, so the binary-collation guard is proven on the statement itself: under the column's accent-insensitive collation ('café' = 'cafe' is true under utf8mb4_0900_ai_ci) the lookup would admit spellings the cache keys and the invalidation listeners, which fold with NormalizedUsername alone, could never address, and a deleted user would keep authenticating from the ttl-less cache. */
func isUserSelectOnTheBinaryCollation(query string) bool {
    return strings.HasPrefix(query, "SELECT") &&
        strings.Contains(query, "melody_example_v2_user") &&
        strings.Contains(query, "LOWER(username) = (") &&
        strings.Contains(query, "COLLATE utf8mb4_bin")
}

func TestFindByUsernameComparesOnTheBinaryCollation(t *testing.T) {
    database, recorder := newFakeBunDatabase()

    repositoryInstance := NewBunUserRepository(database)

    _, _, _ = repositoryInstance.FindByUsername(context.Background(), "Café")

    if 1 != recorder.countMatching(isUserSelectOnTheBinaryCollation) {
        t.Fatalf("expected the lookup to compare on the binary collation, recorded: %v", recorder.recordedQueries())
    }
}

func TestUsernameTakenByAnotherComparesOnTheBinaryCollation(t *testing.T) {
    database, recorder := newFakeBunDatabase()

    repositoryInstance := NewBunUserRepository(database)

    _, _ = repositoryInstance.usernameTakenByAnother(context.Background(), "Café", "user-1")

    if 1 != recorder.countMatching(isUserSelectOnTheBinaryCollation) {
        t.Fatalf("expected the uniqueness door to compare on the binary collation, recorded: %v", recorder.recordedQueries())
    }
}

/* the check that precedes the insert is a read, so it cannot hold a name against a caller that passed it at the same moment; the unique index the migration set adds is what holds it, and this is the seam that turns its refusal into the message the door already answers when the read catches the name in time. The match is on the index's own name: a duplicate on the PRIMARY key is an identifier collision, not a taken username. */
func TestAsUsernameAlreadyExists_TranslatesOnlyTheUsernameIndexRefusal(t *testing.T) {
    driverRefusal := fmt.Errorf(
        "Error 1062 (23000): Duplicate entry 'zzprobe' for key 'melody_example_v2_user.%s'",
        migration.UserUsernameIndexName,
    )
    refusal := fmt.Errorf("insert failed: %w", driverRefusal)

    translated := asUsernameAlreadyExists(refusal)
    if false == errors.Is(translated, ErrUsernameAlreadyExists) {
        t.Fatalf("expected the door's own refusal, got %v", translated)
    }

    if "username already exists" != translated.Error() {
        t.Fatalf("expected the door's own message, got %q", translated.Error())
    }
}

/* mysql renders the duplicated value before the key clause and does not escape it, so a username that spells a clause itself would put a first-clause reader onto the value; the clause read is the tail of the message */
func TestAsUsernameAlreadyExists_ReadsTheKeyClauseAtTheTailOfTheMessage(t *testing.T) {
    for _, value := range []string{
        "for key 'z'",
        "for key 'melody_example_v2_user.PRIMARY'",
    } {
        collision := fmt.Errorf("Error 1062 (23000): Duplicate entry '%s' for key 'melody_example_v2_user.%s'", value, migration.UserUsernameIndexName)

        if false == errors.Is(asUsernameAlreadyExists(collision), ErrUsernameAlreadyExists) {
            t.Fatalf("expected the collision on the username index to be read past the value %q, got %v", value, asUsernameAlreadyExists(collision))
        }
    }
}

func TestAsUsernameAlreadyExists_LeavesEveryOtherFailureAlone(t *testing.T) {
    primaryKey := fmt.Errorf("Error 1062 (23000): Duplicate entry 'user-7' for key 'melody_example_v2_user.PRIMARY'")

    if primaryKey != asUsernameAlreadyExists(primaryKey) {
        t.Fatalf("expected a duplicate identifier to stay the diagnosis it is, got %q", asUsernameAlreadyExists(primaryKey))
    }

    spelledLikeTheIndex := fmt.Errorf("Error 1062 (23000): Duplicate entry '%s' for key 'melody_example_v2_user.PRIMARY'", migration.UserUsernameIndexName)

    if spelledLikeTheIndex != asUsernameAlreadyExists(spelledLikeTheIndex) {
        t.Fatalf("expected a duplicate identifier spelled like the index to stay the diagnosis it is, got %q", asUsernameAlreadyExists(spelledLikeTheIndex))
    }

    if nil != asUsernameAlreadyExists(nil) {
        t.Fatalf("expected a write that did not fail to stay unreported")
    }
}

/* selfWrappingError closes on itself through Unwrap and selfJoiningError through both branches of a join: an unbounded walk over a cycle exhausts the goroutine stack, a fatal error no recover turns into a response */
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
        "Error 1062 (23000): Duplicate entry 'zzprobe' for key 'melody_example_v2_user.%s'",
        migration.UserUsernameIndexName,
    )
    deep := fmt.Errorf("insert failed: %w", fmt.Errorf("tracked: %w", errors.Join(errors.New("other"), driverRefusal)))

    if translated := asUsernameAlreadyExists(deep); false == errors.Is(translated, ErrUsernameAlreadyExists) {
        t.Errorf("a refusal three links down came back as %v", translated)
    }
}

/* userRowAnswering answers the read of an account by id with one row and every count with zero, which is what an update of an existing account whose new name no other account holds reads before it writes */
func userRowAnswering(query string) ([]string, [][]driver.Value, error) {
    if true == strings.Contains(query, "count(") {
        return []string{"count"}, [][]driver.Value{{int64(0)}}, nil
    }

    if true == strings.HasPrefix(query, "SELECT") && true == strings.Contains(query, "melody_example_v2_user") {
        return []string{"id", "username", "password", "roles"}, [][]driver.Value{{"user-1", "user", "hash", "ROLE_USER"}}, nil
    }

    return []string{}, nil, nil
}

func usernameIndexRefusal(query string) error {
    if true == strings.HasPrefix(query, "INSERT") || true == strings.HasPrefix(query, "UPDATE") {
        return fmt.Errorf("Error 1062 (23000): Duplicate entry 'user' for key 'melody_example_v2_user.%s'", migration.UserUsernameIndexName)
    }

    return nil
}

/* the read that precedes the insert passed, and the index refused the write: a caller that passed the read at the same moment holds the name, and the door answers it as the taken name it is */
func TestBunUserRepository_CreateAnswersTheUsernameIndexRefusalAsATakenName(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.execHook = usernameIndexRefusal

    createErr := (&bunUserRepository{database: database}).Create(context.Background(), entity.NewUser("user-9", "user", "hash", []string{entity.RoleUser}))
    if false == errors.Is(createErr, ErrUsernameAlreadyExists) {
        t.Fatalf("expected the index refusal answered as a taken name, got %v", createErr)
    }
}

func TestBunUserRepository_UpdateAnswersTheUsernameIndexRefusalAsATakenName(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = userRowAnswering
    recorder.execHook = usernameIndexRefusal

    _, updateErr := (&bunUserRepository{database: database}).Update(context.Background(), entity.NewUser("user-1", "renamed", "hash", []string{entity.RoleUser}))
    if false == errors.Is(updateErr, ErrUsernameAlreadyExists) {
        t.Fatalf("expected the index refusal answered as a taken name, got %v", updateErr)
    }
}

func TestBunUserRepository_CreateLeavesAnotherRefusalAsTheDriverAnswered(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    primaryKeyRefusal := errors.New("Error 1062 (23000): Duplicate entry 'user-9' for key 'melody_example_v2_user.PRIMARY'")
    recorder.execHook = func(query string) error {
        if true == strings.HasPrefix(query, "INSERT") {
            return primaryKeyRefusal
        }

        return nil
    }

    createErr := (&bunUserRepository{database: database}).Create(context.Background(), entity.NewUser("user-9", "user", "hash", []string{entity.RoleUser}))
    if true == errors.Is(createErr, ErrUsernameAlreadyExists) || nil == createErr || false == strings.Contains(createErr.Error(), "PRIMARY") {
        t.Fatalf("expected the identifier collision to stay the driver's, got %v", createErr)
    }
}
