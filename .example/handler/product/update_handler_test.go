package product

import (
    nethttp "net/http"
    "strings"
    "testing"
)

func TestApiUpdateDoor_RefusesAPriceAtTheBound(t *testing.T) {
    fixture := newProductDoorFixture(t)

    for _, price := range []string{"1000000000000", "1.8e307"} {
        status, body := fixture.call(t, ApiUpdateHandler(), nethttp.MethodPost, productBody("", price), map[string]string{"id": "prod-1"})
        if nethttp.StatusBadRequest != status || false == strings.Contains(strings.Join(errorListOf(t, body), "\n"), "price") {
            t.Fatalf("price %s: expected 400 naming the price, got %d %q", price, status, body)
        }
    }

    if product, _ := fixture.stored(t, "prod-1"); nil == product || 1000000000000 <= product.Price {
        t.Fatalf("expected the stored price untouched, got %+v", product)
    }

    if status, body := fixture.call(t, ApiUpdateHandler(), nethttp.MethodPost, productBody("", "999999999999.99"), map[string]string{"id": "prod-1"}); nethttp.StatusOK != status {
        t.Fatalf("expected a price below the bound updated, got %d %q", status, body)
    }
}

func TestApiUpdateDoor_AnswersABodyPastTheLimit413(t *testing.T) {
    fixture := newProductDoorFixture(t)
    fixture.bodyLimit = 8

    if status, body := fixture.call(t, ApiUpdateHandler(), nethttp.MethodPost, productBody("", "2"), map[string]string{"id": "prod-1"}); nethttp.StatusRequestEntityTooLarge != status {
        t.Fatalf("expected 413, got %d %q", status, body)
    }
}
