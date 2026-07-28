package repository

import (
    "context"
    "fmt"
    "strings"
    "sync"
    "time"

    "github.com/precision-soft/melody/.example/entity"
)

func NewInMemoryProductRepository() ProductRepository {
    return &inMemoryProductRepository{products: seedProductList(time.Now())}
}

type inMemoryProductRepository struct {
    mutex    sync.RWMutex
    products []*entity.Product
}

/* @info the returned slice is a copy, but a shallow one: the entity pointers stay shared with the
repository, so a caller that mutates an entity in place bypasses the lock */
func (instance *inMemoryProductRepository) All(ctx context.Context) ([]*entity.Product, error) {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    return append([]*entity.Product{}, instance.products...), nil
}

func (instance *inMemoryProductRepository) FindById(ctx context.Context, id string) (*entity.Product, bool, error) {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    product, found := instance.findByIdLocked(id)

    return product, found, nil
}

func (instance *inMemoryProductRepository) findByIdLocked(id string) (*entity.Product, bool) {
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

func (instance *inMemoryProductRepository) Create(ctx context.Context, product *entity.Product) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    validationErr := validateProduct(product)
    if nil != validationErr {
        return validationErr
    }

    if "" == strings.TrimSpace(product.Id) {
        product.Id = nextProductId(instance.identifierListLocked())
    }

    _, exists := instance.findByIdLocked(product.Id)
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

func (instance *inMemoryProductRepository) Update(ctx context.Context, product *entity.Product) (bool, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    validationErr := validateProduct(product)
    if nil != validationErr {
        return false, validationErr
    }

    id := strings.TrimSpace(product.Id)
    if "" == id {
        return false, fmt.Errorf("id is required")
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

func (instance *inMemoryProductRepository) DeleteById(ctx context.Context, id string) (bool, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

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

func (instance *inMemoryProductRepository) identifierListLocked() []string {
    identifierList := make([]string, 0, len(instance.products))

    for _, product := range instance.products {
        if nil == product {
            continue
        }

        identifierList = append(identifierList, product.Id)
    }

    return identifierList
}

var _ ProductRepository = (*inMemoryProductRepository)(nil)
