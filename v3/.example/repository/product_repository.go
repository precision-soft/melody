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

/* NewProductRepository hands back the nomenclature the environment can actually support: the database-backed one when a connection was configured, and the in-memory one otherwise. The choice is made here rather than in the configuration because the generated wiring fills this constructor from the container, and the storage handle is what carries the answer.

The migration set is applied and the table seeded on the way out, so the first caller finds a nomenclature rather than an empty one. The trail's own tables are created beside it: an audited write is one transaction over both, so a missing trail table would turn every write into a failure rather than into an unaudited success. */
//melody:service ServiceProductRepository
func NewProductRepository(storage *persistence.CatalogStorage) (ProductRepository, error) {
    if false == storage.IsPersistent() {
        return newInMemoryProductRepository(), nil
    }

    migrateErr := migration.EnsureMigrated(context.Background(), storage.Database())
    if nil != migrateErr {
        return nil, migrateErr
    }

    ensureAuditSchemaErr := storage.EnsureAuditSchema(context.Background())
    if nil != ensureAuditSchemaErr {
        return nil, ensureAuditSchemaErr
    }

    repositoryInstance := newBunProductRepository(storage)

    seedErr := repositoryInstance.seedIfEmpty(context.Background())
    if nil != seedErr {
        return nil, seedErr
    }

    return repositoryInstance, nil
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
