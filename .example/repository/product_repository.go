package repository

import (
    "context"
    "fmt"
    "strings"

    "github.com/precision-soft/melody/.example/entity"
    "github.com/precision-soft/melody/.example/migration"
    melodycontainer "github.com/precision-soft/melody/container"
    melodycontainercontract "github.com/precision-soft/melody/container/contract"
    "github.com/uptrace/bun"
)

const (
    ServiceProductRepository = "service.example.product.repository"
)

/* ProductRepository carries a context and an error on every method, so a listing that cannot reach mysql says so rather than answering an empty catalogue, and a cancelled request stops its query. */
type ProductRepository interface {
    All(ctx context.Context) ([]*entity.Product, error)

    FindById(ctx context.Context, id string) (*entity.Product, bool, error)

    Create(ctx context.Context, product *entity.Product) error

    Update(ctx context.Context, product *entity.Product) (bool, error)

    DeleteById(ctx context.Context, id string) (bool, error)
}

func MustGetProductRepository(resolver melodycontainercontract.Resolver) ProductRepository {
    return melodycontainer.MustFromResolver[ProductRepository](resolver, ServiceProductRepository)
}

/* ProductRepositoryProvider hands back the catalogue the environment can support: the database-backed one when the configuration published a connection, the in-memory one otherwise. The database service name is passed in, since the configuration package decides whether the connection exists, and an empty name is that decision. */
func ProductRepositoryProvider(databaseServiceName string) melodycontainercontract.Provider[ProductRepository] {
    return func(resolver melodycontainercontract.Resolver) (ProductRepository, error) {
        if "" == databaseServiceName {
            return NewInMemoryProductRepository(), nil
        }

        database, databaseErr := melodycontainer.FromResolver[*bun.DB](resolver, databaseServiceName)
        if nil != databaseErr {
            return nil, databaseErr
        }

        if migrateErr := migration.EnsureMigrated(context.Background(), database); nil != migrateErr {
            return nil, migrateErr
        }

        repositoryInstance := NewBunProductRepository(database)

        if seedErr := repositoryInstance.seedIfEmpty(context.Background()); nil != seedErr {
            return nil, seedErr
        }

        return repositoryInstance, nil
    }
}

/* validateProduct reports the first field the product fails on, shared by both implementations so they refuse with the same words. */
func validateProduct(product *entity.Product) error {
    if nil == product {
        return fmt.Errorf("product is required")
    }

    if "" == strings.TrimSpace(product.Name) {
        return fmt.Errorf("name is required")
    }

    if "" == strings.TrimSpace(product.Description) {
        return fmt.Errorf("description is required")
    }

    if "" == strings.TrimSpace(product.CategoryId) {
        return fmt.Errorf("category id is required")
    }

    if "" == strings.TrimSpace(product.CurrencyId) {
        return fmt.Errorf("currency id is required")
    }

    if 0 > product.Price {
        return fmt.Errorf("price must be >= 0")
    }

    if 0 > product.Stock {
        return fmt.Errorf("stock must be >= 0")
    }

    return nil
}

/* nextProductId continues the seeded numbering, so an identifier the caller left empty reads like the ones already in the catalogue. */
func nextProductId(existingIdList []string) string {
    return fmt.Sprintf("prod-%d", highestIdSuffix(existingIdList, "prod-")+1)
}
