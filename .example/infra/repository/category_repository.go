package inmemoryrepository

import (
    "fmt"
    "strconv"
    "strings"

    "github.com/precision-soft/melody/.example/domain/entity"
    "github.com/precision-soft/melody/.example/domain/repository"
    melodycontainercontract "github.com/precision-soft/melody/container/contract"
)

func NewInMemoryCategoryRepository() repository.CategoryRepository {
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
    categories []*entity.Category
}

func (instance *inMemoryCategoryRepository) All() []*entity.Category {
    return instance.categories
}

func (instance *inMemoryCategoryRepository) FindById(id string) (*entity.Category, bool) {
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
    if nil == category {
        return fmt.Errorf("category is required")
    }

    if "" == strings.TrimSpace(category.Name) {
        return fmt.Errorf("name is required")
    }

    if "" == strings.TrimSpace(category.Id) {
        category.Id = instance.nextId()
    }

    _, exists := instance.FindById(category.Id)
    if true == exists {
        return fmt.Errorf("id already exists")
    }

    instance.categories = append(instance.categories, category)
    return nil
}

func (instance *inMemoryCategoryRepository) Update(category *entity.Category) (bool, error) {
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

func (instance *inMemoryCategoryRepository) nextId() string {
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

var _ repository.CategoryRepository = (*inMemoryCategoryRepository)(nil)

func CategoryRepositoryProvider() melodycontainercontract.Provider[repository.CategoryRepository] {
    return func(resolver melodycontainercontract.Resolver) (repository.CategoryRepository, error) {
        return NewInMemoryCategoryRepository(), nil
    }
}
