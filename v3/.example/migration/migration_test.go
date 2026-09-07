package migration

import (
    "testing"
)

/* the set is six tables and one constraint: the unique key on the folded username, which is what actually
   holds a name against two callers that pass the read-then-write check at the same moment. */
func TestMigrationsRegisterTheSixTablesAndTheUsernameConstraintInOrder(t *testing.T) {
    sorted := Migrations.Sorted()
    if 7 != len(sorted) {
        t.Fatalf("expected the set to hold seven migrations, got %d", len(sorted))
    }

    expectedNames := []string{
        "20260819000001",
        "20260819000002",
        "20260819000003",
        "20260819000004",
        "20260819000005",
        "20260819000006",
        "20260906000007",
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
