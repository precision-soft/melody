package repository

import (
    "context"
    "fmt"
    "strings"
    "sync"

    "github.com/precision-soft/melody/v3/.example/entity"
)

func newInMemoryCategoryRepository() CategoryRepository {
    categories := seedCategoryList()

    identifierList := make([]string, 0, len(categories))
    for _, category := range categories {
        identifierList = append(identifierList, category.Id)
    }

    return &inMemoryCategoryRepository{categories: categories, mintFloor: seededFloor(identifierList, "cat-")}
}

type inMemoryCategoryRepository struct {
    mutex      sync.RWMutex
    categories []*entity.Category
    /* mintFloor is the highest identifier this repository ever stored, see raisedFloor */
    mintFloor  string
}

/* the slice is a shallow copy: the entity pointers stay shared with the repository, so a caller that mutates an entity in place bypasses the lock */
func (instance *inMemoryCategoryRepository) All(ctx context.Context) ([]*entity.Category, error) {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    return append([]*entity.Category{}, instance.categories...), nil
}

func (instance *inMemoryCategoryRepository) FindById(ctx context.Context, id string) (*entity.Category, bool, error) {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    category, found := instance.findByIdLocked(id)

    return category, found, nil
}

func (instance *inMemoryCategoryRepository) findByIdLocked(id string) (*entity.Category, bool) {
    for _, category := range instance.categories {
        if nil == category {
            continue
        }

        if id == category.Id {
            return category, true
        }
    }

    return nil, false
}

func (instance *inMemoryCategoryRepository) Create(ctx context.Context, category *entity.Category) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    validationErr := validateCategory(category)
    if nil != validationErr {
        return validationErr
    }

    if ceilingErr := refuseIdentifierAtCeiling(category.Id, "cat-"); nil != ceilingErr {
        return ceilingErr
    }

    if "" == strings.TrimSpace(category.Id) {
        category.Id = nextCategoryId(append(instance.identifierListLocked(), instance.mintFloor))
    }

    _, exists := instance.findByIdLocked(category.Id)
    if true == exists {
        return ErrIdAlreadyExists
    }

    instance.categories = append(instance.categories, category)
    instance.mintFloor = raisedFloor(instance.mintFloor, category.Id, "cat-")
    return nil
}

func (instance *inMemoryCategoryRepository) Update(ctx context.Context, category *entity.Category) (bool, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    validationErr := validateCategory(category)
    if nil != validationErr {
        return false, validationErr
    }

    id := strings.TrimSpace(category.Id)
    if "" == id {
        return false, fmt.Errorf("id is required")
    }

    for index, existing := range instance.categories {
        if nil == existing {
            continue
        }

        if id != existing.Id {
            continue
        }

        instance.categories[index] = category
        return true, nil
    }

    return false, nil
}

func (instance *inMemoryCategoryRepository) DeleteById(ctx context.Context, id string) (bool, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    normalizedId := strings.TrimSpace(id)
    if "" == normalizedId {
        return false, fmt.Errorf("id is required")
    }

    for index, category := range instance.categories {
        if nil == category {
            continue
        }

        if normalizedId != category.Id {
            continue
        }

        instance.categories = append(instance.categories[:index], instance.categories[index+1:]...)
        return true, nil
    }

    return false, nil
}

func (instance *inMemoryCategoryRepository) identifierListLocked() []string {
    identifierList := make([]string, 0, len(instance.categories))

    for _, category := range instance.categories {
        if nil == category {
            continue
        }

        identifierList = append(identifierList, category.Id)
    }

    return identifierList
}

var _ CategoryRepository = (*inMemoryCategoryRepository)(nil)
