package service

import (
    "testing"
    "time"

    examplecache "github.com/precision-soft/melody/v3/.example/cache"
    melodycache "github.com/precision-soft/melody/v3/cache"
    melodyclock "github.com/precision-soft/melody/v3/clock"
)

func TestProductService_RecordViewCountsEachReadOnTheProductsOwnCounter(t *testing.T) {
    backend := melodycache.NewInMemoryBackend(0, time.Minute, melodyclock.NewSystemClock())
    manager := melodycache.NewManagerOwningBackend(backend, examplecache.NewGobSerializer())
    t.Cleanup(func() { _ = manager.Close() })

    productService := NewProductService(nil, nil, nil, manager, nil, nil)

    for expected := int64(1); expected <= 2; expected++ {
        views, recordErr := productService.RecordView("prod-1")
        if nil != recordErr {
            t.Fatalf("record: %v", recordErr)
        }

        if expected != views {
            t.Fatalf("expected %d views, got %d", expected, views)
        }
    }

    other, otherErr := productService.RecordView("prod-2")
    if nil != otherErr || 1 != other {
        t.Fatalf("expected another product to count from one, got %d (%v)", other, otherErr)
    }

    stored, exists, readErr := manager.GetCounter(CacheKeyProductViews("prod-1"))
    if nil != readErr || false == exists || 2 != stored {
        t.Fatalf("expected the counter under the product's key to hold 2, got %d exists=%t (%v)", stored, exists, readErr)
    }
}
