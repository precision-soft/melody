package product

import (
    "errors"
    "fmt"
    nethttp "net/http"
    "testing"

    "github.com/precision-soft/melody/.example/repository"
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
