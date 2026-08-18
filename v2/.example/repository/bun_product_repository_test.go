package repository

import (
    "context"
    "strings"
    "testing"
)

/* The count guard proven here is the same shape the category, currency and user seeders carry; the product seeder is the proven representative because it is the one whose seed the live bands read back. */

func isProductInsert(query string) bool {
    return strings.HasPrefix(query, "INSERT") && strings.Contains(query, "melody_example_v2_product")
}

func TestSeedIfEmptyLeavesAPopulatedTableAlone(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = countingRows(7)

    repositoryInstance := NewBunProductRepository(database)

    if seedErr := repositoryInstance.seedIfEmpty(context.Background()); nil != seedErr {
        t.Fatalf("expected the populated table to answer success, got %v", seedErr)
    }
    if insertCount := recorder.countMatching(isProductInsert); 0 != insertCount {
        t.Fatalf("expected no insert over a populated table, got %d", insertCount)
    }
}

func TestSeedIfEmptySowsTheOpeningCatalogue(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = countingRows(0)

    repositoryInstance := NewBunProductRepository(database)

    if seedErr := repositoryInstance.seedIfEmpty(context.Background()); nil != seedErr {
        t.Fatalf("expected the empty table to be seeded, got %v", seedErr)
    }
    if insertCount := recorder.countMatching(isProductInsert); 1 != insertCount {
        t.Fatalf("expected exactly one seeding insert, got %d", insertCount)
    }
}
