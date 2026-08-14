package migration

import (
    "context"
    "strings"
    "testing"
)

func TestUpCreateUserCreatesTheTableTolerantly(t *testing.T) {
    database, recorder := newFakeBunDatabase()

    if upErr := upCreateUser(context.Background(), database); nil != upErr {
        t.Fatalf("expected the up migration to succeed, got %v", upErr)
    }

    queries := recorder.recordedQueries()
    if 1 != len(queries) {
        t.Fatalf("expected exactly one statement, got %v", queries)
    }
    if false == strings.Contains(queries[0], "CREATE TABLE IF NOT EXISTS `melody_example_v1_user`") {
        t.Fatalf("expected a tolerant create of the user table, got %q", queries[0])
    }
}

func TestDownCreateUserDropsTheTableTolerantly(t *testing.T) {
    database, recorder := newFakeBunDatabase()

    if downErr := downCreateUser(context.Background(), database); nil != downErr {
        t.Fatalf("expected the down migration to succeed, got %v", downErr)
    }

    queries := recorder.recordedQueries()
    if 1 != len(queries) {
        t.Fatalf("expected exactly one statement, got %v", queries)
    }
    if false == strings.Contains(queries[0], "DROP TABLE IF EXISTS `melody_example_v1_user`") {
        t.Fatalf("expected a tolerant drop of the user table, got %q", queries[0])
    }
}
