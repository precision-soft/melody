package migration

import (
    "context"
    "testing"
)

/* the set is ONE migration because the example has one state, so what is pinned here is the CONTENT of
   that migration — which statements it emits, and in what order — rather than how many steps the schema
   is spread over. The count this replaced could not see either: a set of five steps emitted backwards
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

func TestUpSchemaCreatesEveryTableTolerantly(t *testing.T) {
    database, recorder := newFakeBunDatabase()

    if upErr := upSchema(context.Background(), database); nil != upErr {
        t.Fatalf("expected the up migration to succeed, got %v", upErr)
    }

    assertQueryOrder(t, recorder.recordedQueries(), []string{
        "CREATE TABLE IF NOT EXISTS `melody_example_v2_category`",
        "CREATE TABLE IF NOT EXISTS `melody_example_v2_currency`",
        "CREATE TABLE IF NOT EXISTS `melody_example_v2_product`",
        "CREATE TABLE IF NOT EXISTS `melody_example_v2_user`",
        "CREATE TABLE IF NOT EXISTS `melody_example_v2_catalog_journal`",
    })
}

func TestDownSchemaDropsTheTablesInReverse(t *testing.T) {
    database, recorder := newFakeBunDatabase()

    if downErr := downSchema(context.Background(), database); nil != downErr {
        t.Fatalf("expected the down migration to succeed, got %v", downErr)
    }

    assertQueryOrder(t, recorder.recordedQueries(), []string{
        "DROP TABLE IF EXISTS `melody_example_v2_catalog_journal`",
        "DROP TABLE IF EXISTS `melody_example_v2_user`",
        "DROP TABLE IF EXISTS `melody_example_v2_product`",
        "DROP TABLE IF EXISTS `melody_example_v2_currency`",
        "DROP TABLE IF EXISTS `melody_example_v2_category`",
    })
}
