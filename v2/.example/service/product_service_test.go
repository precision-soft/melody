package service

import (
    "testing"

    "github.com/precision-soft/melody/v2/.example/event"
)

/* the row is stored before the event goes out, so a failed event is no refusal: the caller is answered with the row, the failure is journaled, and the entries a listener that never ran would have dropped are dropped */
func TestProductService_AnswersTheCommittedRowWhenADispatchFails(t *testing.T) {
    catalogue := newCatalogueUnderTest(t, true)
    catalogue.prime(t, CacheKeyProductList, CacheKeyProductById("prod-committed"))

    product, createErr := catalogue.product.Create(catalogue.runtime, "prod-committed", "Probe", "d", "cat-1", 1, "cur-eur", 1)
    if nil != createErr || nil == product || "prod-committed" != product.Id {
        t.Fatalf("expected the created product answered, got %+v, %v", product, createErr)
    }
    catalogue.assertCommittedDispatchFailure(t, event.ProductCreatedEventName, "prod-committed", CacheKeyProductList, CacheKeyProductById("prod-committed"))

    catalogue.logger.records = nil
    catalogue.prime(t, CacheKeyProductList, CacheKeyProductById("prod-committed"))
    updated, found, updateErr := catalogue.product.Update(catalogue.runtime, "prod-committed", "Renamed", "d", "cat-1", 2, "cur-eur", 1)
    if nil != updateErr || false == found || nil == updated || "Renamed" != updated.Name {
        t.Fatalf("expected the updated product answered, got %+v, %t, %v", updated, found, updateErr)
    }
    catalogue.assertCommittedDispatchFailure(t, event.ProductUpdatedEventName, "prod-committed", CacheKeyProductList, CacheKeyProductById("prod-committed"))

    catalogue.logger.records = nil
    catalogue.prime(t, CacheKeyProductList, CacheKeyProductById("prod-committed"))
    deleted, deleteErr := catalogue.product.DeleteById(catalogue.runtime, "prod-committed")
    if nil != deleteErr || false == deleted {
        t.Fatalf("expected the delete answered, got %t, %v", deleted, deleteErr)
    }
    catalogue.assertCommittedDispatchFailure(t, event.ProductDeletedEventName, "prod-committed", CacheKeyProductList, CacheKeyProductById("prod-committed"))
}

func TestProductService_JournalsNothingWhenTheDispatchSucceeds(t *testing.T) {
    catalogue := newCatalogueUnderTest(t, false)

    if _, createErr := catalogue.product.Create(catalogue.runtime, "prod-healthy", "Probe", "d", "cat-1", 1, "cur-eur", 1); nil != createErr {
        t.Fatalf("create: %v", createErr)
    }

    if records := catalogue.logger.recorded(); 0 != len(records) {
        t.Fatalf("expected no record for a dispatch that succeeded, got %v", records)
    }
}
