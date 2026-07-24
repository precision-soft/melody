package repository

import (
    "fmt"
    "strconv"
    "strings"
    "sync"

    "github.com/precision-soft/melody/v3/.example/entity"
)

//melody:service ServiceCategoryRepository
func NewInMemoryCategoryRepository() CategoryRepository {
    return &inMemoryCategoryRepository{
        categories: []*entity.Category{
            entity.NewCategory("cat-1", "Memory"),
            entity.NewCategory("cat-2", "Storage"),
            entity.NewCategory("cat-3", "Graphics"),
            entity.NewCategory("cat-4", "Power"),
        },
    }
}

type inMemoryCategoryRepository struct {
    mutex      sync.RWMutex
    categories []*entity.Category
}

/* @info the returned slice is a copy, but a shallow one: the entity pointers stay shared with the
repository, so a caller that mutates an entity in place bypasses the lock */
func (instance *inMemoryCategoryRepository) All() []*entity.Category {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    return append([]*entity.Category{}, instance.categories...)
}

func (instance *inMemoryCategoryRepository) FindById(id string) (*entity.Category, bool) {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    return instance.findByIdLocked(id)
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

func (instance *inMemoryCategoryRepository) Create(category *entity.Category) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if nil == category {
        return fmt.Errorf("category is required")
    }

    if "" == strings.TrimSpace(category.Name) {
        return fmt.Errorf("name is required")
    }

    if "" == strings.TrimSpace(category.Id) {
        category.Id = instance.nextIdLocked()
    }

    _, exists := instance.findByIdLocked(category.Id)
    if true == exists {
        return fmt.Errorf("id already exists")
    }

    instance.categories = append(instance.categories, category)
    return nil
}

func (instance *inMemoryCategoryRepository) Update(category *entity.Category) (bool, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if nil == category {
        return false, fmt.Errorf("category is required")
    }

    id := strings.TrimSpace(category.Id)
    if "" == id {
        return false, fmt.Errorf("id is required")
    }

    if "" == strings.TrimSpace(category.Name) {
        return false, fmt.Errorf("name is required")
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

func (instance *inMemoryCategoryRepository) DeleteById(id string) (bool, error) {
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

func (instance *inMemoryCategoryRepository) nextIdLocked() string {
    maxSuffix := int64(0)

    for _, category := range instance.categories {
        if nil == category {
            continue
        }

        id := strings.TrimSpace(category.Id)
        if false == strings.HasPrefix(id, "cat-") {
            continue
        }

        suffixString := strings.TrimPrefix(id, "cat-")
        parsedSuffix, parseErr := strconv.ParseInt(suffixString, 10, 64)
        if nil != parseErr {
            continue
        }

        if parsedSuffix > maxSuffix {
            maxSuffix = parsedSuffix
        }
    }

    return fmt.Sprintf("cat-%d", maxSuffix+1)
}

var _ CategoryRepository = (*inMemoryCategoryRepository)(nil)
