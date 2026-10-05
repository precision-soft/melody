package service

import (
    "context"
    "errors"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/.example/entity"
    "github.com/precision-soft/melody/v3/.example/persistence"
    "github.com/precision-soft/melody/v3/.example/repository"
)

/* paddingCategoryRepository answers a lookup the way MySQL's PAD SPACE collation does: trailing spaces of the identifier asked are ignored */
type paddingCategoryRepository struct {
    repository.CategoryRepository
}

func (instance *paddingCategoryRepository) FindById(ctx context.Context, id string) (*entity.Category, bool, error) {
    return instance.CategoryRepository.FindById(ctx, strings.TrimRight(id, " "))
}

func TestCategoryService_FindByIdAnswersAPaddedIdentifierAsAbsentAndCachesNothingUnderIt(t *testing.T) {
    categoryRepository, categoryErr := repository.NewCategoryRepository(persistence.NewCatalogStorage(nil))
    if nil != categoryErr {
        t.Fatalf("build the category repository: %v", categoryErr)
    }

    cacheInstance := newTtlRecordingCache()
    categoryService := NewCategoryService(&paddingCategoryRepository{CategoryRepository: categoryRepository}, newInMemoryProductRepositoryForTest(t), cacheInstance, nil)

    assertPaddedIdentifierAnsweredAbsent(t, cacheInstance, CacheKeyCategoryById, func(id string) (bool, error) {
        _, found, findErr := categoryService.FindById(id)

        return found, findErr
    }, "cat-1")
}

/* a category a product sits in is not deleted from under it, in memory as the foreign key holds it on the database; one no product names is */
func TestCategoryService_RefusesToDeleteACategoryAProductSitsIn(t *testing.T) {
    _, dispatcher, runtimeInstance := currencyServiceUnderTest(t)

    categoryRepository, categoryErr := repository.NewCategoryRepository(persistence.NewCatalogStorage(nil))
    if nil != categoryErr {
        t.Fatalf("build the category repository: %v", categoryErr)
    }

    if createErr := categoryRepository.Create(context.Background(), entity.NewCategory("cat-empty", "Empty")); nil != createErr {
        t.Fatalf("create the empty category: %v", createErr)
    }

    categoryService := NewCategoryService(categoryRepository, newInMemoryProductRepositoryForTest(t), newTtlRecordingCache(), dispatcher.dispatcher)

    if deleted, deleteErr := categoryService.DeleteById(runtimeInstance, "cat-1"); true == deleted || false == errors.Is(deleteErr, repository.ErrCategoryInUse) {
        t.Fatalf("the delete of a used category answered deleted=%v err=%v", deleted, deleteErr)
    }

    if _, found, _ := categoryRepository.FindById(context.Background(), "cat-1"); false == found {
        t.Fatalf("a refused delete removed the category")
    }

    if deleted, deleteErr := categoryService.DeleteById(runtimeInstance, "cat-empty"); false == deleted || nil != deleteErr {
        t.Fatalf("the delete of an unused category answered deleted=%v err=%v", deleted, deleteErr)
    }
}
