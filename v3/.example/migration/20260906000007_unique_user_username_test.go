package migration

import (
    "context"
    "database/sql/driver"
    "strings"
    "testing"
)

/* indexPresenceRows answers the catalogue question the step asks before it touches the table. The step has
   to tolerate a volume whose table already carries the key, the way every other step tolerates a table that
   already exists — MySQL has no ADD KEY IF NOT EXISTS, so the tolerance is spelled by asking first. */
func indexPresenceRows(present int64) func(query string) ([]string, [][]driver.Value, error) {
    return func(query string) ([]string, [][]driver.Value, error) {
        if true == strings.Contains(query, "information_schema.STATISTICS") {
            return []string{"count"}, [][]driver.Value{{present}}, nil
        }

        return []string{}, nil, nil
    }
}

func isUsernameIndexAdd(query string) bool {
    return strings.HasPrefix(query, "ALTER TABLE") &&
        strings.Contains(query, "ADD UNIQUE KEY")
}

func isUsernameIndexDrop(query string) bool {
    return strings.HasPrefix(query, "ALTER TABLE") &&
        strings.Contains(query, "DROP INDEX")
}

/* the expression is the whole point of the constraint: the identity this application gives a username is
   LOWER(username) compared byte for byte, because NormalizedUsername folds case and nothing else and the
   lookup door compares on utf8mb4_bin for the same reason. Indexed on the column as it stands, the key
   would follow the column's own accent-insensitive collation and refuse two names the application holds
   apart — 'ana' and 'ána' — while admitting 'Ana' beside 'ana', which it holds to be one. Measured on the
   running server with exactly this expression: 'ana' and 'ANA' collide with 'Ana', 'Ána' does not. */
func TestUpUniqueUserUsernameAddsTheKeyOnTheFoldedSpelling(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = indexPresenceRows(0)

    if upErr := upUniqueUserUsername(context.Background(), database); nil != upErr {
        t.Fatalf("expected the up migration to succeed, got %v", upErr)
    }

    index := recorder.firstIndexMatching(isUsernameIndexAdd)
    if 0 > index {
        t.Fatalf("expected the step to add the unique key, recorded: %v", recorder.recordedQueries())
    }

    statement := recorder.recordedQueries()[index]

    for _, wanted := range []string{
        "`melody_example_v3_user`",
        UserUsernameIndexName,
        "LOWER(`username`)",
        "CHARACTER SET utf8mb4",
        "COLLATE utf8mb4_bin",
    } {
        if false == strings.Contains(statement, wanted) {
            t.Fatalf("expected the key to be built on %s, got %q", wanted, statement)
        }
    }
}

/* the tolerance is the half a volume older than this step depends on: asked for a key it already carries,
   the step must do nothing rather than fail the whole set on a duplicate index name. */
func TestUpUniqueUserUsernameLeavesAKeyThatIsAlreadyThere(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = indexPresenceRows(1)

    if upErr := upUniqueUserUsername(context.Background(), database); nil != upErr {
        t.Fatalf("expected the up migration to succeed, got %v", upErr)
    }

    if 0 != recorder.countMatching(isUsernameIndexAdd) {
        t.Fatalf("expected no ALTER when the key is already present, recorded: %v", recorder.recordedQueries())
    }
}

func TestDownUniqueUserUsernameDropsTheKeyOnlyWhenItIsThere(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = indexPresenceRows(1)

    if downErr := downUniqueUserUsername(context.Background(), database); nil != downErr {
        t.Fatalf("expected the down migration to succeed, got %v", downErr)
    }

    if 1 != recorder.countMatching(isUsernameIndexDrop) {
        t.Fatalf("expected the key to be dropped, recorded: %v", recorder.recordedQueries())
    }

    recorder.reset()
    recorder.queryHook = indexPresenceRows(0)

    if downErr := downUniqueUserUsername(context.Background(), database); nil != downErr {
        t.Fatalf("expected the down migration to succeed on a table without the key, got %v", downErr)
    }

    if 0 != recorder.countMatching(isUsernameIndexDrop) {
        t.Fatalf("expected no DROP when the key is not there, recorded: %v", recorder.recordedQueries())
    }
}
