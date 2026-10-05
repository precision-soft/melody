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

/* ProductRepository carries a context and an error on every method, so a listing that cannot reach mysql says so rather than answering an empty catalogue, and a cancelled request stops its query. */
type ProductRepository interface {
    All(ctx context.Context) ([]*entity.Product, error)

    FindById(ctx context.Context, id string) (*entity.Product, bool, error)

    Create(ctx context.Context, product *entity.Product) error

    Update(ctx context.Context, product *entity.Product) (bool, error)

    DeleteById(ctx context.Context, id string) (bool, error)

    /* PricedIn answers whether any product is priced in the currency, the question a currency delete asks before the foreign key would refuse it */
    PricedIn(ctx context.Context, currencyId string) (bool, error)

    /* CategorizedIn answers whether any product sits in the category, the question a category delete asks before the foreign key would refuse it */
    CategorizedIn(ctx context.Context, categoryId string) (bool, error)

    /* HoldingReferences runs a reference check and the write it guards as one step against every other: a product write naming a category and a currency, and the delete of a category or a currency. On the database the product table's foreign keys hold the references and the action runs as given; in memory one catalogue lock serializes the actions, so a check cannot pass on a reference a concurrent delete is removing. */
    HoldingReferences(action func() error) error
}

func MustGetProductRepository(resolver melodycontainercontract.Resolver) ProductRepository {
    return melodycontainer.MustFromResolver[ProductRepository](resolver, ServiceProductRepository)
}

/* NewProductRepository answers the database-backed repository when a connection is configured and the in-memory one otherwise; the generated wiring fills it from the container, and the storage handle carries the choice. The migration set is applied and the table seeded on the way out, with the trail's own tables beside it, since an audited write is one transaction over both. */
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

    /* a product row references its category and its currency by foreign key, so a resolution that reaches an emptied catalogue first seeds the two nomenclatures it names; both seeds change nothing over tables that hold rows */
    if seedErr := newBunCategoryRepository(storage.Database()).seedIfEmpty(context.Background()); nil != seedErr {
        return nil, seedErr
    }

    if seedErr := newBunCurrencyRepository(storage.Database()).seedIfEmpty(context.Background()); nil != seedErr {
        return nil, seedErr
    }

    repositoryInstance := newBunProductRepository(storage)

    seedErr := repositoryInstance.seedIfEmpty(context.Background())
    if nil != seedErr {
        return nil, seedErr
    }

    return repositoryInstance, nil
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
