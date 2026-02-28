package inmemoryrepository

import (
    "fmt"
    "strconv"
    "strings"
    "time"

    "github.com/precision-soft/melody/v2/.example/domain/entity"
    "github.com/precision-soft/melody/v2/.example/domain/repository"
    melodycontainercontract "github.com/precision-soft/melody/v2/container/contract"
)

func NewInMemoryProductRepository() repository.ProductRepository {
    now := time.Now()

    return &inMemoryProductRepository{
        products: []*entity.Product{
            entity.NewProduct(
                "prod-1",
                "DDR5 32GB Dual Kit 6000MT/s",
                "black",
                "cat-1",
                149.99,
                "cur-eur",
                12,
                now.Add(-240*time.Hour),
                now.Add(-12*time.Hour),
            ),
            entity.NewProduct(
                "prod-2",
                "DDR5 64GB Dual Kit 5600MT/s",
                "black",
                "cat-1",
                259.99,
                "cur-eur",
                6,
                now.Add(-232*time.Hour),
                now.Add(-24*time.Hour),
            ),
            entity.NewProduct(
                "prod-3",
                "DDR4 32GB Dual Kit 3600MT/s",
                "black",
                "cat-1",
                99.99,
                "cur-eur",
                18,
                now.Add(-220*time.Hour),
                now.Add(-48*time.Hour),
            ),
            entity.NewProduct(
                "prod-4",
                "NVMe SSD 2TB Gen4",
                "silver",
                "cat-2",
                129.99,
                "cur-usd",
                9,
                now.Add(-210*time.Hour),
                now.Add(-72*time.Hour),
            ),
            entity.NewProduct(
                "prod-5",
                "Mechanical Keyboard TKL",
                "white",
                "cat-3",
                79.99,
                "cur-ron",
                25,
                now.Add(-200*time.Hour),
                now.Add(-96*time.Hour),
            ),
        },
    }
}

type inMemoryProductRepository struct {
    products []*entity.Product
}

func (instance *inMemoryProductRepository) All() []*entity.Product {
    return instance.products
}

func (instance *inMemoryProductRepository) FindById(id string) (*entity.Product, bool) {
    for _, product := range instance.products {
        if nil == product {
            continue
        }

        if id == product.Id {
            return product, true
        }
    }

    return nil, false
}

func (instance *inMemoryProductRepository) Create(product *entity.Product) error {
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
        return fmt.Errorf("currencyId is required")
    }

    if 0 > product.Price {
        return fmt.Errorf("price must be >= 0")
    }

    if 0 > product.Stock {
        return fmt.Errorf("stock must be >= 0")
    }

    if "" == strings.TrimSpace(product.Id) {
        product.Id = instance.nextId()
    }

    _, exists := instance.FindById(product.Id)
    if true == exists {
        return fmt.Errorf("id already exists")
    }

    now := time.Now()
    if true == product.CreatedAt.IsZero() {
        product.CreatedAt = now
    }
    if true == product.UpdatedAt.IsZero() {
        product.UpdatedAt = now
    }

    instance.products = append(instance.products, product)
    return nil
}

func (instance *inMemoryProductRepository) Update(product *entity.Product) (bool, error) {
    if nil == product {
        return false, fmt.Errorf("product is required")
    }

    id := strings.TrimSpace(product.Id)
    if "" == id {
        return false, fmt.Errorf("id is required")
    }

    if "" == strings.TrimSpace(product.Name) {
        return false, fmt.Errorf("name is required")
    }

    if "" == strings.TrimSpace(product.Description) {
        return false, fmt.Errorf("description is required")
    }

    if "" == strings.TrimSpace(product.CategoryId) {
        return false, fmt.Errorf("category id is required")
    }

    if "" == strings.TrimSpace(product.CurrencyId) {
        return false, fmt.Errorf("currencyId is required")
    }

    if 0 > product.Price {
        return false, fmt.Errorf("price must be >= 0")
    }

    if 0 > product.Stock {
        return false, fmt.Errorf("stock must be >= 0")
    }

    for index, existing := range instance.products {
        if nil == existing {
            continue
        }

        if id != existing.Id {
            continue
        }

        if true == product.CreatedAt.IsZero() {
            product.CreatedAt = existing.CreatedAt
        }

        if true == product.UpdatedAt.IsZero() {
            product.UpdatedAt = time.Now()
        }

        instance.products[index] = product
        return true, nil
    }

    return false, nil
}

func (instance *inMemoryProductRepository) DeleteById(id string) (bool, error) {
    normalizedId := strings.TrimSpace(id)
    if "" == normalizedId {
        return false, fmt.Errorf("id is required")
    }

    for index, product := range instance.products {
        if nil == product {
            continue
        }

        if normalizedId != product.Id {
            continue
        }

        instance.products = append(instance.products[:index], instance.products[index+1:]...)
        return true, nil
    }

    return false, nil
}

func (instance *inMemoryProductRepository) nextId() string {
    maxSuffix := int64(0)

    for _, product := range instance.products {
        if nil == product {
            continue
        }

        id := strings.TrimSpace(product.Id)
        if false == strings.HasPrefix(id, "prod-") {
            continue
        }

        suffixString := strings.TrimPrefix(id, "prod-")
        parsedSuffix, parseErr := strconv.ParseInt(suffixString, 10, 64)
        if nil != parseErr {
            continue
        }

        if parsedSuffix > maxSuffix {
            maxSuffix = parsedSuffix
        }
    }

    return fmt.Sprintf("prod-%d", maxSuffix+1)
}

var _ repository.ProductRepository = (*inMemoryProductRepository)(nil)

func ProductRepositoryProvider() melodycontainercontract.Provider[repository.ProductRepository] {
    return func(resolver melodycontainercontract.Resolver) (repository.ProductRepository, error) {
        return NewInMemoryProductRepository(), nil
    }
}
