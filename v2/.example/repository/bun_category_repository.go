package repository

import (
    "context"
    "database/sql"
    "errors"
    "fmt"
    "strings"

    "github.com/precision-soft/melody/v2/.example/entity"
    "github.com/uptrace/bun"
)

/* categoryRow is the nomenclature as the database holds it; the domain entity stays free of storage concerns because it is cached through a gob serializer. */
type categoryRow struct {
    bun.BaseModel `bun:"table:melody_example_v2_category,alias:category"`

    Id   string `bun:"id,pk"`
    Name string `bun:"name,notnull"`
}

func newCategoryRow(category *entity.Category) *categoryRow {
    return &categoryRow{
        Id:   category.Id,
        Name: category.Name,
    }
}

func (instance *categoryRow) toEntity() *entity.Category {
    return entity.NewCategory(instance.Id, instance.Name)
}

func NewBunCategoryRepository(database *bun.DB) *bunCategoryRepository {
    return &bunCategoryRepository{database: database}
}

type bunCategoryRepository struct {
    database *bun.DB
}

/* EnsureSchema creates the table when it is absent and writes the opening nomenclature into it when it is empty. The seeding insert ignores duplicate keys because several example applications may reach an empty table at the same time, and losing that race is not a failure. */
func (instance *bunCategoryRepository) EnsureSchema(ctx context.Context) error {
    _, createErr := instance.database.
        NewCreateTable().
        Model((*categoryRow)(nil)).
        IfNotExists().
        Exec(ctx)
    if nil != createErr {
        return createErr
    }

    count, countErr := instance.database.
        NewSelect().
        Model((*categoryRow)(nil)).
        Count(ctx)
    if nil != countErr {
        return countErr
    }

    if 0 < count {
        return nil
    }

    seedList := seedCategoryList()
    rowList := make([]*categoryRow, 0, len(seedList))
    for _, category := range seedList {
        rowList = append(rowList, newCategoryRow(category))
    }

    _, insertErr := instance.database.
        NewInsert().
        Model(&rowList).
        Ignore().
        Exec(ctx)

    return insertErr
}

func (instance *bunCategoryRepository) All(ctx context.Context) ([]*entity.Category, error) {
    rowList := make([]*categoryRow, 0)

    selectErr := instance.database.
        NewSelect().
        Model(&rowList).
        Order("id ASC").
        Scan(ctx)
    if nil != selectErr {
        return nil, selectErr
    }

    categories := make([]*entity.Category, 0, len(rowList))
    for _, row := range rowList {
        categories = append(categories, row.toEntity())
    }

    return categories, nil
}

func (instance *bunCategoryRepository) FindById(ctx context.Context, id string) (*entity.Category, bool, error) {
    row, found, findErr := instance.findRowById(ctx, id)
    if nil != findErr {
        return nil, false, findErr
    }

    if false == found {
        return nil, false, nil
    }

    return row.toEntity(), true, nil
}

/* findRowById separates a row that is not there from a query that could not run: only sql.ErrNoRows is an answer, and every other failure is reported. */
func (instance *bunCategoryRepository) findRowById(ctx context.Context, id string) (*categoryRow, bool, error) {
    row := &categoryRow{}

    selectErr := instance.database.
        NewSelect().
        Model(row).
        Where("id = ?", id).
        Limit(1).
        Scan(ctx)
    if nil != selectErr {
        if true == errors.Is(selectErr, sql.ErrNoRows) {
            return nil, false, nil
        }

        return nil, false, selectErr
    }

    return row, true, nil
}

func (instance *bunCategoryRepository) Create(ctx context.Context, category *entity.Category) error {
    validationErr := validateCategory(category)
    if nil != validationErr {
        return validationErr
    }

    if "" == strings.TrimSpace(category.Id) {
        identifierList, identifierErr := instance.identifierList(ctx)
        if nil != identifierErr {
            return identifierErr
        }

        category.Id = nextCategoryId(identifierList)
    }

    _, exists, existsErr := instance.findRowById(ctx, category.Id)
    if nil != existsErr {
        return existsErr
    }

    if true == exists {
        return fmt.Errorf("id already exists")
    }

    _, insertErr := instance.database.
        NewInsert().
        Model(newCategoryRow(category)).
        Exec(ctx)

    return insertErr
}

func (instance *bunCategoryRepository) Update(ctx context.Context, category *entity.Category) (bool, error) {
    validationErr := validateCategory(category)
    if nil != validationErr {
        return false, validationErr
    }

    id := strings.TrimSpace(category.Id)
    if "" == id {
        return false, fmt.Errorf("id is required")
    }

    _, found, findErr := instance.findRowById(ctx, id)
    if nil != findErr {
        return false, findErr
    }

    if false == found {
        return false, nil
    }

    result, updateErr := instance.database.
        NewUpdate().
        Model(newCategoryRow(category)).
        WherePK().
        Exec(ctx)
    if nil != updateErr {
        return false, updateErr
    }

    return affectedAtLeastOneRow(result), nil
}

func (instance *bunCategoryRepository) DeleteById(ctx context.Context, id string) (bool, error) {
    normalizedId := strings.TrimSpace(id)
    if "" == normalizedId {
        return false, fmt.Errorf("id is required")
    }

    result, deleteErr := instance.database.
        NewDelete().
        Model((*categoryRow)(nil)).
        Where("id = ?", normalizedId).
        Exec(ctx)
    if nil != deleteErr {
        return false, deleteErr
    }

    return affectedAtLeastOneRow(result), nil
}

func (instance *bunCategoryRepository) identifierList(ctx context.Context) ([]string, error) {
    identifierList := make([]string, 0)

    selectErr := instance.database.
        NewSelect().
        Model((*categoryRow)(nil)).
        Column("id").
        Scan(ctx, &identifierList)
    if nil != selectErr {
        return nil, selectErr
    }

    return identifierList, nil
}

var _ CategoryRepository = (*bunCategoryRepository)(nil)
