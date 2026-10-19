package product

import (
    "errors"
    "fmt"
    nethttp "net/http"
    "reflect"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v2/.example/repository"
)

func TestCreateRefusalStatusAnswersAConflictForATakenIdentifier(t *testing.T) {
    status, message := createRefusalStatus(fmt.Errorf("create product: %w", repository.ErrIdAlreadyExists))
    if nethttp.StatusConflict != status || "id already exists" != message {
        t.Fatalf("expected 409 naming the identifier, got %d %q", status, message)
    }
}

func TestCreateRefusalStatusAnswersAnyOtherFailureAsTheCatalogues(t *testing.T) {
    status, message := createRefusalStatus(errors.New("connection refused"))
    if nethttp.StatusInternalServerError != status || "failed to create product" != message {
        t.Fatalf("expected 500, got %d %q", status, message)
    }
}

func TestCreateRequestTrimmedIsTheSpellingTheDoorValidatesAndStores(t *testing.T) {
    trimmed := createRequest{Id: " prod-1 ", Name: " X", Description: " d ", CategoryId: " cat-1", CurrencyId: "cur-eur "}.trimmed()

    if "prod-1" != trimmed.Id || "X" != trimmed.Name || "d" != trimmed.Description || "cat-1" != trimmed.CategoryId || "cur-eur" != trimmed.CurrencyId {
        t.Fatalf("the body was not trimmed field by field: %+v", trimmed)
    }
}

/* the read rounds a price to the cent by multiplying it by a hundred, so a stored price near the float64 ceiling turns into +Inf and the list answers 500 for every caller */
func TestApiCreateDoor_RefusesAPriceAtTheBound(t *testing.T) {
    fixture := newProductDoorFixture(t)

    for _, price := range []string{"1000000000000", "1.8e307"} {
        status, body := fixture.call(t, ApiCreateHandler(), nethttp.MethodPost, productBody("prod-priced", price), nil)
        if nethttp.StatusBadRequest != status || false == strings.Contains(strings.Join(errorListOf(t, body), "\n"), "price") {
            t.Fatalf("price %s: expected 400 naming the price, got %d %q", price, status, body)
        }
    }

    if _, found := fixture.stored(t, "prod-priced"); true == found {
        t.Fatalf("expected no product stored past the bound")
    }

    if status, body := fixture.call(t, ApiCreateHandler(), nethttp.MethodPost, productBody("prod-priced", "999999999999.99"), nil); nethttp.StatusCreated != status {
        t.Fatalf("expected a price below the bound created, got %d %q", status, body)
    }
}

func TestApiCreateDoor_TagSpellsTheRepositoryBound(t *testing.T) {
    for _, dto := range []any{createRequest{}, updateRequest{}} {
        field, _ := reflect.TypeOf(dto).FieldByName("Price")
        if wanted := fmt.Sprintf("lessThan=%d", int64(repository.ProductPriceBound)); false == strings.Contains(field.Tag.Get("validate"), wanted) {
            t.Fatalf("%T: expected the price tag to spell %s, got %q", dto, wanted, field.Tag.Get("validate"))
        }
    }
}

func TestApiCreateDoor_AnswersATakenIdentifier409(t *testing.T) {
    fixture := newProductDoorFixture(t)

    if status, body := fixture.call(t, ApiCreateHandler(), nethttp.MethodPost, productBody("prod-x", "1"), nil); nethttp.StatusCreated != status {
        t.Fatalf("expected the first create stored, got %d %q", status, body)
    }

    status, body := fixture.call(t, ApiCreateHandler(), nethttp.MethodPost, productBody("prod-x", "2"), nil)
    if nethttp.StatusConflict != status || false == strings.Contains(body, "id already exists") {
        t.Fatalf("expected 409 naming the identifier, got %d %q", status, body)
    }

    if status, body := fixture.call(t, ApiCreateHandler(), nethttp.MethodPost, productBody("prod-y", "1"), nil); nethttp.StatusCreated != status {
        t.Fatalf("expected a fresh identifier created, got %d %q", status, body)
    }
}

func TestApiCreateDoor_RefusesASuppliedIdentifierAtTheCeiling400(t *testing.T) {
    fixture := newProductDoorFixture(t)

    status, body := fixture.call(t, ApiCreateHandler(), nethttp.MethodPost, productBody("prod-9223372036854775807", "1"), nil)
    if nethttp.StatusBadRequest != status || false == strings.Contains(body, "id: ") {
        t.Fatalf("expected 400 naming the identifier, got %d %q", status, body)
    }

    if status, body := fixture.call(t, ApiCreateHandler(), nethttp.MethodPost, productBody("", "1"), nil); nethttp.StatusCreated != status {
        t.Fatalf("expected the mint to keep working, got %d %q", status, body)
    }
}

/* the kernel bounds every body; a body past it is the client's too large a payload, not unreadable json */
func TestApiCreateDoor_AnswersABodyPastTheLimit413(t *testing.T) {
    fixture := newProductDoorFixture(t)
    fixture.bodyLimit = 8

    if status, body := fixture.call(t, ApiCreateHandler(), nethttp.MethodPost, productBody("prod-bounded", "1"), nil); nethttp.StatusRequestEntityTooLarge != status {
        t.Fatalf("expected 413, got %d %q", status, body)
    }

    if _, found := fixture.stored(t, "prod-bounded"); true == found {
        t.Fatalf("expected nothing stored from a body never read")
    }
}
