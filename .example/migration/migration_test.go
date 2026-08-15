package migration

import (
    "testing"
)

func TestMigrationsRegisterTheFourCatalogTablesInOrder(t *testing.T) {
    sorted := Migrations.Sorted()
    if 4 != len(sorted) {
        t.Fatalf("expected the catalog set to hold four migrations, got %d", len(sorted))
    }

    expectedNames := []string{
        "20260814000001",
        "20260814000002",
        "20260814000003",
        "20260814000004",
    }
    for index, migrationInstance := range sorted {
        if expectedNames[index] != migrationInstance.Name {
            t.Fatalf(
                "expected migration %d to be %s, got %s",
                index,
                expectedNames[index],
                migrationInstance.Name,
            )
        }
        if nil == migrationInstance.Up || nil == migrationInstance.Down {
            t.Fatalf("expected migration %s to carry both directions", migrationInstance.Name)
        }
    }
}

func TestJournalMigrationsRegisterTheJournalTableAlone(t *testing.T) {
    sorted := JournalMigrations.Sorted()
    if 1 != len(sorted) {
        t.Fatalf("expected the journal set to hold one migration, got %d", len(sorted))
    }

    if "20260814000005" != sorted[0].Name {
        t.Fatalf("expected the journal migration to keep its name, got %s", sorted[0].Name)
    }
    if nil == sorted[0].Up || nil == sorted[0].Down {
        t.Fatalf("expected migration %s to carry both directions", sorted[0].Name)
    }
}
