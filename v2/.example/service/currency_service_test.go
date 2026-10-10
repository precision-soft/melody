package service

import (
    "testing"

    "github.com/precision-soft/melody/v2/.example/event"
)

func TestCurrencyService_AnswersTheCommittedRowWhenADispatchFails(t *testing.T) {
    catalogue := newCatalogueUnderTest(t, true)
    catalogue.prime(t, CacheKeyCurrencyList, CacheKeyCurrencyById("cur-committed"))

    currency, createErr := catalogue.currency.Create(catalogue.runtime, "cur-committed", "XCM", "Probe")
    if nil != createErr || nil == currency {
        t.Fatalf("expected the created currency answered, got %+v, %v", currency, createErr)
    }
    catalogue.assertCommittedDispatchFailure(t, event.CurrencyCreatedEventName, "cur-committed", CacheKeyCurrencyList, CacheKeyCurrencyById("cur-committed"))

    catalogue.logger.records = nil
    catalogue.prime(t, CacheKeyCurrencyList, CacheKeyCurrencyById("cur-committed"))
    updated, found, updateErr := catalogue.currency.Update(catalogue.runtime, "cur-committed", "XCM", "Renamed")
    if nil != updateErr || false == found || nil == updated {
        t.Fatalf("expected the updated currency answered, got %+v, %t, %v", updated, found, updateErr)
    }
    catalogue.assertCommittedDispatchFailure(t, event.CurrencyUpdatedEventName, "cur-committed", CacheKeyCurrencyList, CacheKeyCurrencyById("cur-committed"))

    catalogue.logger.records = nil
    catalogue.prime(t, CacheKeyCurrencyList, CacheKeyCurrencyById("cur-committed"))
    deleted, deleteErr := catalogue.currency.DeleteById(catalogue.runtime, "cur-committed")
    if nil != deleteErr || false == deleted {
        t.Fatalf("expected the delete answered, got %t, %v", deleted, deleteErr)
    }
    catalogue.assertCommittedDispatchFailure(t, event.CurrencyDeletedEventName, "cur-committed", CacheKeyCurrencyList, CacheKeyCurrencyById("cur-committed"))
}
