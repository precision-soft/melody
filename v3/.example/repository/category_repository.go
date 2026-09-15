package repository

import (
    "context"
    "fmt"
    "strings"

    "github.com/precision-soft/melody/v3/.example/entity"
    "github.com/precision-soft/melody/v3/.example/migration"
    "github.com/precision-soft/melody/v3/.example/persistence"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
)

const (
    ServiceCategoryRepository = "service.example.category.repository"
)

/* CategoryRepository propagates database failures and caller cancellation rather than treating them as missing data. */
type CategoryRepository interface {
    All(ctx context.Context) ([]*entity.Category, error)

    FindById(ctx context.Context, id string) (*entity.Category, bool, error)

    Create(ctx context.Context, category *entity.Category) error

    Update(ctx context.Context, category *entity.Category) (bool, error)

    DeleteById(ctx context.Context, id string) (bool, error)
}

func MustGetCategoryRepository(resolver melodycontainercontract.Resolver) CategoryRepository {
    return melodycontainer.MustFromResolver[CategoryRepository](resolver, ServiceCategoryRepository)
}

/* NewCategoryRepository selects persistent or in-memory storage. Persistent construction applies migrations and seeds an empty table. */
//melody:service ServiceCategoryRepository
func NewCategoryRepository(storage *persistence.CatalogStorage) (CategoryRepository, error) {
    if false == storage.IsPersistent() {
        return newInMemoryCategoryRepository(), nil
    }

    migrateErr := migration.EnsureMigrated(context.Background(), storage.Database())
    if nil != migrateErr {
        return nil, migrateErr
    }

    repositoryInstance := newBunCategoryRepository(storage.Database())

    seedErr := repositoryInstance.seedIfEmpty(context.Background())
    if nil != seedErr {
        return nil, seedErr
    }

    return repositoryInstance, nil
}

func validateCategory(category *entity.Category) error {
    if nil == category {
        return fmt.Errorf("category is required")
    }

    if "" == strings.TrimSpace(category.Name) {
        return fmt.Errorf("name is required")
    }

    return nil
}

func nextCategoryId(existingIdList []string) string {
    return fmt.Sprintf("cat-%d", highestIdSuffix(existingIdList, "cat-")+1)
}
