package migration

import (
    "context"
    "strings"
    "testing"
)

func TestUpCreateProductCreatesTheTableTolerantly(t *testing.T) {
    database, recorder := newFakeBunDatabase()

    if upErr := upCreateProduct(context.Background(), database); nil != upErr {
        t.Fatalf("expected the up migration to succeed, got %v", upErr)
    }

    queries := recorder.recordedQueries()
    if 1 != len(queries) {
        t.Fatalf("expected exactly one statement, got %v", queries)
    }
    if false == strings.Contains(queries[0], "CREATE TABLE IF NOT EXISTS `melody_example_v2_product`") {
        t.Fatalf("expected a tolerant create of the product table, got %q", queries[0])
    }
    if false == strings.Contains(queries[0], "`created_at` DATETIME(6) NOT NULL") {
        t.Fatalf("expected the creation stamp to keep microseconds, got %q", queries[0])
    }
}

func TestDownCreateProductDropsTheTableTolerantly(t *testing.T) {
    database, recorder := newFakeBunDatabase()

    if downErr := downCreateProduct(context.Background(), database); nil != downErr {
        t.Fatalf("expected the down migration to succeed, got %v", downErr)
    }

    queries := recorder.recordedQueries()
    if 1 != len(queries) {
        t.Fatalf("expected exactly one statement, got %v", queries)
    }
    if false == strings.Contains(queries[0], "DROP TABLE IF EXISTS `melody_example_v2_product`") {
        t.Fatalf("expected a tolerant drop of the product table, got %q", queries[0])
    }
}
