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

/* CategoryRepository carries a context and an error on every method because one of its implementations talks to a database: a listing that cannot reach mysql has to say so rather than answer with an empty nomenclature, and a request that was cancelled has to stop the query it started. */
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

/* NewCategoryRepository hands back the nomenclature the environment can actually support: the database-backed one when a connection was configured, and the in-memory one otherwise. The choice is made here rather than in the configuration because the generated wiring fills this constructor from the container, and the storage handle is what carries the answer.

   The migration set is applied and the table seeded on the way out, so the first caller finds a nomenclature rather than an empty one. */
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

/* validateCategory reports the first field the category fails on, shared by both implementations so a bad write is refused with the same words whichever one the environment picked. */
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
