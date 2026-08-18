package repository

import (
    "context"
    "strings"
    "testing"
)

/* the fake driver records the exact SQL bun emits, so the binary-collation guard is proven on the statement itself: left to the column's accent-insensitive collation ('café' = 'cafe' is true under utf8mb4_0900_ai_ci), the lookup admitted spellings the cache keys and the invalidation listeners — which fold with NormalizedUsername alone — could never address, and a deleted user kept authenticating from the ttl-less cache under the collation-only spelling. */
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
