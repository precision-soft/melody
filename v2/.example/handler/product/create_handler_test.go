package product

import (
    "errors"
    "fmt"
    nethttp "net/http"
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
