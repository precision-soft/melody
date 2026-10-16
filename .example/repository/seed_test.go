package repository

import (
    "context"
    "strings"
    "testing"
)

/* insertIntoTable answers whether a recorded statement is the seeding insert of one table. */
func insertIntoTable(table string) func(query string) bool {
    return func(query string) bool {
        return strings.HasPrefix(query, "INSERT") && strings.Contains(query, table)
    }
}

func seededTableNameList() []string {
    return []string{
        "melody_example_v1_category",
        "melody_example_v1_currency",
        "melody_example_v1_product",
        "melody_example_v1_user",
    }
}

/* SeedAll is a list of four, and a list is exactly the shape that loses a member without anything else
   changing. It is the only door the reset command has to the opening state, so a seeder missing from it
   leaves a table empty on a volume an operator has just reset — and every other test in the package drives
   one seeder at a time, so none of them could see the absence of a fourth. */
func TestSeedAllSowsEveryNomenclature(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = countingRows(0)

    if seedErr := SeedAll(context.Background(), database); nil != seedErr {
        t.Fatalf("expected every nomenclature to be seeded, got %v", seedErr)
    }

    for _, table := range seededTableNameList() {
        if insertCount := recorder.countMatching(insertIntoTable(table)); 1 != insertCount {
            t.Fatalf("expected exactly one seeding insert into %s, got %d", table, insertCount)
        }
    }
}

/* the same door over a database that already holds rows writes nothing, which is what makes it safe to run
   outside a reset: each seeder asks its own table first. */
func TestSeedAllLeavesPopulatedTablesAlone(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = countingRows(7)

    if seedErr := SeedAll(context.Background(), database); nil != seedErr {
        t.Fatalf("expected the populated tables to answer success, got %v", seedErr)
    }

    for _, table := range seededTableNameList() {
        if insertCount := recorder.countMatching(insertIntoTable(table)); 0 != insertCount {
            t.Fatalf("expected no insert into the populated %s, got %d", table, insertCount)
        }
    }
}
