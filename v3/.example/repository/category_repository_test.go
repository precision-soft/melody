package repository

import (
    "testing"

    "github.com/precision-soft/melody/v3/.example/entity"
)

func TestValidateCategoryNamesTheFirstFieldItFailsOn(t *testing.T) {
    if validationErr := validateCategory(nil); nil == validationErr || "category is required" != validationErr.Error() {
        t.Fatalf("expected an absent category to be refused, got %v", validationErr)
    }

    if validationErr := validateCategory(&entity.Category{Name: " "}); nil == validationErr || "name is required" != validationErr.Error() {
        t.Fatalf("expected a blank name to be refused, got %v", validationErr)
    }

    if validationErr := validateCategory(&entity.Category{Name: "Memory"}); nil != validationErr {
        t.Fatalf("expected a named category to pass, got %v", validationErr)
    }
}

func TestNextCategoryIdContinuesTheSeededNumbering(t *testing.T) {
    if "cat-6" != nextCategoryId([]string{"cat-1", "cat-5", "cat-2"}) {
        t.Fatalf("expected cat-6, got %q", nextCategoryId([]string{"cat-1", "cat-5", "cat-2"}))
    }
}
