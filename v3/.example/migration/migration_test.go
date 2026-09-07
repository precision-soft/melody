package migration

import (
    "context"
    "database/sql/driver"
    "strings"
    "testing"
)

/* the set is ONE migration because the example has one state, so what is pinned here is the CONTENT of
   that migration — which statements it emits, and in what order — rather than how many steps the schema
   is spread over. The count this replaced could not see either: a set of seven steps in the wrong order
   would have satisfied it. */
func TestMigrationsHoldOneSchemaMigration(t *testing.T) {
    sorted := Migrations.Sorted()
    if 1 != len(sorted) {
        t.Fatalf("expected the set to hold one migration, got %d", len(sorted))
    }

    if "20260907000001" != sorted[0].Name {
        t.Fatalf("expected the migration to be 20260907000001, got %s", sorted[0].Name)
    }
    if nil == sorted[0].Up || nil == sorted[0].Down {
        t.Fatalf("expected migration %s to carry both directions", sorted[0].Name)
    }
}

func TestUpSchemaCreatesEveryTableTolerantlyThenTheConstraint(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = indexPresenceRows(0)

    if upErr := upSchema(context.Background(), database); nil != upErr {
        t.Fatalf("expected the up migration to succeed, got %v", upErr)
    }

    assertQueryOrder(t, recorder.recordedQueries(), []string{
        "CREATE TABLE IF NOT EXISTS `melody_example_v3_category`",
        "CREATE TABLE IF NOT EXISTS `melody_example_v3_currency`",
        "CREATE TABLE IF NOT EXISTS `melody_example_v3_product`",
        "CREATE TABLE IF NOT EXISTS `melody_example_v3_user`",
        "CREATE TABLE IF NOT EXISTS `melody_example_v3_catalog_journal`",
        "CREATE TABLE IF NOT EXISTS `melody_example_v3_two_factor`",
        "information_schema.STATISTICS",
        "ADD UNIQUE KEY",
    })
}

func TestDownSchemaDropsTheConstraintFirstAndTheTablesInReverse(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = indexPresenceRows(1)

    if downErr := downSchema(context.Background(), database); nil != downErr {
        t.Fatalf("expected the down migration to succeed, got %v", downErr)
    }

    assertQueryOrder(t, recorder.recordedQueries(), []string{
        "information_schema.STATISTICS",
        "DROP INDEX",
        "DROP TABLE IF EXISTS `melody_example_v3_two_factor`",
        "DROP TABLE IF EXISTS `melody_example_v3_catalog_journal`",
        "DROP TABLE IF EXISTS `melody_example_v3_user`",
        "DROP TABLE IF EXISTS `melody_example_v3_product`",
        "DROP TABLE IF EXISTS `melody_example_v3_currency`",
        "DROP TABLE IF EXISTS `melody_example_v3_category`",
    })
}

/* indexPresenceRows answers the catalogue question the constraint asks before it touches the table. It has
   to tolerate a volume whose table already carries the key, the way every table is created IF NOT EXISTS —
   MySQL has no ADD KEY IF NOT EXISTS, so the tolerance is spelled by asking first. */
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
func TestAddUserUsernameIndexBuildsTheKeyOnTheFoldedSpelling(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = indexPresenceRows(0)

    if addErr := addUserUsernameIndex(context.Background(), database); nil != addErr {
        t.Fatalf("expected the constraint to be added, got %v", addErr)
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

/* the tolerance is the half a volume older than this schema depends on: asked for a key it already
   carries, the migration must do nothing rather than fail the whole set on a duplicate index name. */
func TestAddUserUsernameIndexLeavesAKeyThatIsAlreadyThere(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = indexPresenceRows(1)

    if addErr := addUserUsernameIndex(context.Background(), database); nil != addErr {
        t.Fatalf("expected the constraint step to succeed, got %v", addErr)
    }

    if 0 != recorder.countMatching(isUsernameIndexAdd) {
        t.Fatalf("expected no ALTER when the key is already present, recorded: %v", recorder.recordedQueries())
    }
}

func TestDropUserUsernameIndexDropsTheKeyOnlyWhenItIsThere(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = indexPresenceRows(1)

    if dropErr := dropUserUsernameIndex(context.Background(), database); nil != dropErr {
        t.Fatalf("expected the constraint to be dropped, got %v", dropErr)
    }

    if 1 != recorder.countMatching(isUsernameIndexDrop) {
        t.Fatalf("expected the key to be dropped, recorded: %v", recorder.recordedQueries())
    }

    recorder.reset()
    recorder.queryHook = indexPresenceRows(0)

    if dropErr := dropUserUsernameIndex(context.Background(), database); nil != dropErr {
        t.Fatalf("expected the drop to succeed on a table without the key, got %v", dropErr)
    }

    if 0 != recorder.countMatching(isUsernameIndexDrop) {
        t.Fatalf("expected no DROP when the key is not there, recorded: %v", recorder.recordedQueries())
    }
}
