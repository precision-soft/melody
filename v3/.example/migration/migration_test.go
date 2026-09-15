package migration

import (
    "context"
    "strings"
    "testing"
)

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
