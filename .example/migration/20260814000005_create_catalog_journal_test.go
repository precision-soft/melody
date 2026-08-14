package migration

import (
    "context"
    "strings"
    "testing"
)

func TestUpCreateCatalogJournalCreatesTheTableTolerantly(t *testing.T) {
    database, recorder := newFakeBunDatabase()

    if upErr := upCreateCatalogJournal(context.Background(), database); nil != upErr {
        t.Fatalf("expected the up migration to succeed, got %v", upErr)
    }

    queries := recorder.recordedQueries()
    if 1 != len(queries) {
        t.Fatalf("expected exactly one statement, got %v", queries)
    }
    if false == strings.Contains(queries[0], "CREATE TABLE IF NOT EXISTS `melody_example_v1_catalog_journal`") {
        t.Fatalf("expected a tolerant create of the journal table, got %q", queries[0])
    }
    if false == strings.Contains(queries[0], "`id` BIGINT NOT NULL AUTO_INCREMENT") {
        t.Fatalf("expected the journal identifier to stay database-assigned, got %q", queries[0])
    }
}

func TestDownCreateCatalogJournalDropsTheTableTolerantly(t *testing.T) {
    database, recorder := newFakeBunDatabase()

    if downErr := downCreateCatalogJournal(context.Background(), database); nil != downErr {
        t.Fatalf("expected the down migration to succeed, got %v", downErr)
    }

    queries := recorder.recordedQueries()
    if 1 != len(queries) {
        t.Fatalf("expected exactly one statement, got %v", queries)
    }
    if false == strings.Contains(queries[0], "DROP TABLE IF EXISTS `melody_example_v1_catalog_journal`") {
        t.Fatalf("expected a tolerant drop of the journal table, got %q", queries[0])
    }
}
