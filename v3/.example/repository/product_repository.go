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

/* ProductRepository propagates database failures and caller cancellation rather than treating them as missing data. */
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

/* NewProductRepository selects persistent or in-memory storage. Persistent construction applies the shared catalogue and audit schema and seeds an empty product table. */
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

func nextProductId(existingIdList []string) string {
    return fmt.Sprintf("prod-%d", highestIdSuffix(existingIdList, "prod-")+1)
}
