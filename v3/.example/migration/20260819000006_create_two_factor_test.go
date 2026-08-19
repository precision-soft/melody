package migration

import (
    "context"
    "strings"
    "testing"
)

func TestUpCreateTwoFactorCreatesTheTableTolerantly(t *testing.T) {
    database, recorder := newFakeBunDatabase()

    if upErr := upCreateTwoFactor(context.Background(), database); nil != upErr {
        t.Fatalf("expected the up migration to succeed, got %v", upErr)
    }

    queries := recorder.recordedQueries()
    if 1 != len(queries) {
        t.Fatalf("expected exactly one statement, got %v", queries)
    }
    if false == strings.Contains(queries[0], "CREATE TABLE IF NOT EXISTS `melody_example_v3_two_factor`") {
        t.Fatalf("expected a tolerant create of the two-factor enrollment table, got %q", queries[0])
    }
}

func TestDownCreateTwoFactorDropsTheTableTolerantly(t *testing.T) {
    database, recorder := newFakeBunDatabase()

    if downErr := downCreateTwoFactor(context.Background(), database); nil != downErr {
        t.Fatalf("expected the down migration to succeed, got %v", downErr)
    }

    queries := recorder.recordedQueries()
    if 1 != len(queries) {
        t.Fatalf("expected exactly one statement, got %v", queries)
    }
    if false == strings.Contains(queries[0], "DROP TABLE IF EXISTS `melody_example_v3_two_factor`") {
        t.Fatalf("expected a tolerant drop of the two-factor enrollment table, got %q", queries[0])
    }
}
