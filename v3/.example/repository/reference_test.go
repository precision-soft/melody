package repository

import (
    "errors"
    "fmt"
    "testing"

    "github.com/precision-soft/melody/v3/.example/migration"
)

func TestAsProductReferenceRefusal_NamesTheReferenceTheForeignKeyRefused(t *testing.T) {
    childRefusal := func(constraint string) error {
        return fmt.Errorf("insert: %w", fmt.Errorf("Error 1452 (23000): Cannot add or update a child row: a foreign key constraint fails (`example`.`melody_example_v3_product`, CONSTRAINT `%s` FOREIGN KEY (`x`) REFERENCES `y` (`id`) ON DELETE RESTRICT)", constraint))
    }

    if false == errors.Is(asProductReferenceRefusal(childRefusal(migration.ProductCurrencyForeignKeyName)), ErrUnknownCurrency) {
        t.Errorf("the currency key's refusal was not answered as an unknown currency")
    }

    if false == errors.Is(asProductReferenceRefusal(childRefusal(migration.ProductCategoryForeignKeyName)), ErrUnknownCategory) {
        t.Errorf("the category key's refusal was not answered as an unknown category")
    }

    other := errors.New("connection refused")
    if other != asProductReferenceRefusal(other) {
        t.Errorf("another failure was rewritten")
    }
}

func TestAsCurrencyInUse_NamesTheParentRefusalOfTheProductsCurrencyKey(t *testing.T) {
    parentRefusal := fmt.Errorf("Error 1451 (23000): Cannot delete or update a parent row: a foreign key constraint fails (`example`.`melody_example_v3_product`, CONSTRAINT `%s` FOREIGN KEY (`currency_id`) REFERENCES `melody_example_v3_currency` (`id`) ON DELETE RESTRICT)", migration.ProductCurrencyForeignKeyName)
    if false == errors.Is(asCurrencyInUse(parentRefusal), ErrCurrencyInUse) {
        t.Errorf("the currency key's parent refusal was not answered as a currency in use")
    }

    other := errors.New("connection refused")
    if other != asCurrencyInUse(other) {
        t.Errorf("another failure was rewritten")
    }
}
