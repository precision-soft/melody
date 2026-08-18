package repository

import (
    "context"
    "strings"
    "testing"
)

func isJournalSelect(query string) bool {
    return strings.HasPrefix(query, "SELECT") && strings.Contains(query, "melody_example_v2_catalog_journal")
}

func TestLatestAnswersEverythingWhenTheLimitIsZero(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    repositoryInstance := NewBunCatalogJournalRepository(database)

    if _, latestErr := repositoryInstance.Latest(context.Background(), 0); nil != latestErr {
        t.Fatalf("expected the listing to succeed, got %v", latestErr)
    }

    selectQuery := recorder.firstMatching(isJournalSelect)
    if "" == selectQuery {
        t.Fatalf("expected a journal select, got %v", recorder.recordedQueries())
    }
    if true == strings.Contains(selectQuery, "LIMIT") {
        t.Fatalf("expected a zero limit to ask for everything, got %q", selectQuery)
    }
    if false == strings.Contains(selectQuery, `ORDER BY "id" DESC`) {
        t.Fatalf("expected the newest entry first, got %q", selectQuery)
    }
}

func TestLatestBoundsTheAnswerWhenTheLimitIsPositive(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    repositoryInstance := NewBunCatalogJournalRepository(database)

    if _, latestErr := repositoryInstance.Latest(context.Background(), 3); nil != latestErr {
        t.Fatalf("expected the listing to succeed, got %v", latestErr)
    }

    selectQuery := recorder.firstMatching(isJournalSelect)
    if "" == selectQuery {
        t.Fatalf("expected a journal select, got %v", recorder.recordedQueries())
    }
    if false == strings.Contains(selectQuery, "LIMIT 3") {
        t.Fatalf("expected the positive limit to bound the answer, got %q", selectQuery)
    }
}
