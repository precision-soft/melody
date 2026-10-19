package service

import (
    "testing"

    "github.com/precision-soft/melody/v2/.example/event"
)

func TestCategoryService_AnswersTheCommittedRowWhenADispatchFails(t *testing.T) {
    catalogue := newCatalogueUnderTest(t, true)
    catalogue.prime(t, CacheKeyCategoryList, CacheKeyCategoryById("cat-committed"))

    category, createErr := catalogue.category.Create(catalogue.runtime, "cat-committed", "Probe")
    if nil != createErr || nil == category {
        t.Fatalf("expected the created category answered, got %+v, %v", category, createErr)
    }
    catalogue.assertCommittedDispatchFailure(t, event.CategoryCreatedEventName, "cat-committed", CacheKeyCategoryList, CacheKeyCategoryById("cat-committed"))

    catalogue.logger.records = nil
    catalogue.prime(t, CacheKeyCategoryList, CacheKeyCategoryById("cat-committed"))
    updated, found, updateErr := catalogue.category.Update(catalogue.runtime, "cat-committed", "Renamed")
    if nil != updateErr || false == found || nil == updated {
        t.Fatalf("expected the updated category answered, got %+v, %t, %v", updated, found, updateErr)
    }
    catalogue.assertCommittedDispatchFailure(t, event.CategoryUpdatedEventName, "cat-committed", CacheKeyCategoryList, CacheKeyCategoryById("cat-committed"))

    catalogue.logger.records = nil
    catalogue.prime(t, CacheKeyCategoryList, CacheKeyCategoryById("cat-committed"))
    deleted, deleteErr := catalogue.category.DeleteById(catalogue.runtime, "cat-committed")
    if nil != deleteErr || false == deleted {
        t.Fatalf("expected the delete answered, got %t, %v", deleted, deleteErr)
    }
    catalogue.assertCommittedDispatchFailure(t, event.CategoryDeletedEventName, "cat-committed", CacheKeyCategoryList, CacheKeyCategoryById("cat-committed"))
}
