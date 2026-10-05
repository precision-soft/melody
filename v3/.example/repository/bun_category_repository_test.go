package repository

import (
    "context"
    "errors"
    "fmt"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/.example/migration"
)

/* the product table's foreign key refusing a category delete is answered as the category in use, not as the driver's failure */
func TestBunCategoryRepositoryDeleteById_AnswersTheForeignKeyRefusalAsACategoryInUse(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.execErr = func(query string) error {
        if false == strings.HasPrefix(query, "DELETE") {
            return nil
        }

        return fmt.Errorf("Error 1451 (23000): Cannot delete or update a parent row: a foreign key constraint fails (`example`.`melody_example_v3_product`, CONSTRAINT `%s` FOREIGN KEY (`category_id`) REFERENCES `melody_example_v3_category` (`id`) ON DELETE RESTRICT)", migration.ProductCategoryForeignKeyName)
    }

    deleted, deleteErr := newBunCategoryRepository(database).DeleteById(context.Background(), "cat-1")
    if true == deleted || false == errors.Is(deleteErr, ErrCategoryInUse) {
        t.Fatalf("expected the refusal answered as a category in use, got deleted=%v err=%v", deleted, deleteErr)
    }
}
