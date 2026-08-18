package migration

import (
    "testing"
)

func TestMigrationsRegisterTheFiveTablesInOrder(t *testing.T) {
    sorted := Migrations.Sorted()
    if 5 != len(sorted) {
        t.Fatalf("expected the set to hold five migrations, got %d", len(sorted))
    }

    expectedNames := []string{
        "20260818000001",
        "20260818000002",
        "20260818000003",
        "20260818000004",
        "20260818000005",
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
