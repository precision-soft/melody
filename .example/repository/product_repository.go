package repository

import (
    "context"
    "fmt"
    "strings"

    "github.com/precision-soft/melody/.example/entity"
    melodycontainer "github.com/precision-soft/melody/container"
    melodycontainercontract "github.com/precision-soft/melody/container/contract"
    "github.com/uptrace/bun"
)

const (
    ServiceProductRepository = "service.example.product.repository"
)

/* ProductRepository carries a context and an error on every method because one of its implementations talks to a database: a listing that cannot reach mysql has to say so rather than answer with an empty catalogue, and a request that was cancelled has to stop the query it started. */
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

/* ProductRepositoryProvider hands back the catalogue the environment can actually support: the database-backed one when the configuration published a connection, and the in-memory one otherwise. The name of the database service is passed in rather than looked up, because the configuration package that names it is the one that decides whether it exists at all, and an empty name is that decision. */
func ProductRepositoryProvider(databaseServiceName string) melodycontainercontract.Provider[ProductRepository] {
    return func(resolver melodycontainercontract.Resolver) (ProductRepository, error) {
        if "" == databaseServiceName {
            return NewInMemoryProductRepository(), nil
        }

        database, databaseErr := melodycontainer.FromResolver[*bun.DB](resolver, databaseServiceName)
        if nil != databaseErr {
            return nil, databaseErr
        }

        repositoryInstance := NewBunProductRepository(database)

        ensureSchemaErr := repositoryInstance.EnsureSchema(context.Background())
        if nil != ensureSchemaErr {
            return nil, ensureSchemaErr
        }

        return repositoryInstance, nil
    }
}

/* validateProduct reports the first field the product fails on. Both implementations share it so the in-memory catalogue and the database refuse the same writes with the same words, which is what lets the end-to-end assertions hold whichever one the environment picked. */
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

/* nextProductId continues the seeded numbering rather than inventing a scheme of its own, so an identifier the caller left empty reads like the ones already in the catalogue. */
func nextProductId(existingIdList []string) string {
    return fmt.Sprintf("prod-%d", highestIdSuffix(existingIdList, "prod-")+1)
}
