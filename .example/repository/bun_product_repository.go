package repository

import (
    "context"
    "database/sql"
    "errors"
    "fmt"
    "strings"
    "time"

    "github.com/precision-soft/melody/.example/entity"
    "github.com/uptrace/bun"
)

/* productRow is the catalogue as the database holds it. The domain entity stays free of storage concerns because it is cached through a gob serializer, so the mapping lives here, beside the repository that performs it. */
type productRow struct {
    bun.BaseModel `bun:"table:melody_example_v1_product,alias:product"`

    Id          string    `bun:"id,pk"`
    Name        string    `bun:"name,notnull"`
    Description string    `bun:"description,notnull"`
    CategoryId  string    `bun:"category_id,notnull"`
    Price       float64   `bun:"price,notnull"`
    CurrencyId  string    `bun:"currency_id,notnull"`
    Stock       int64     `bun:"stock,notnull"`
    CreatedAt   time.Time `bun:"created_at,notnull,type:datetime(6)"`
    UpdatedAt   time.Time `bun:"updated_at,notnull,type:datetime(6)"`
}

func newProductRow(product *entity.Product) *productRow {
    return &productRow{
        Id:          product.Id,
        Name:        product.Name,
        Description: product.Description,
        CategoryId:  product.CategoryId,
        Price:       product.Price,
        CurrencyId:  product.CurrencyId,
        Stock:       product.Stock,
        CreatedAt:   product.CreatedAt,
        UpdatedAt:   product.UpdatedAt,
    }
}

func (instance *productRow) toEntity() *entity.Product {
    return entity.NewProduct(
        instance.Id,
        instance.Name,
        instance.Description,
        instance.CategoryId,
        instance.Price,
        instance.CurrencyId,
        instance.Stock,
        instance.CreatedAt,
        instance.UpdatedAt,
    )
}

func NewBunProductRepository(database *bun.DB) *bunProductRepository {
    return &bunProductRepository{database: database}
}

type bunProductRepository struct {
    database *bun.DB
}

/* seedIfEmpty writes the opening catalogue into an empty table; the table itself belongs to the migration set the provider has already applied. The insert ignores duplicate keys because several example applications may reach an empty table at the same time, and losing that race is not a failure. */
func (instance *bunProductRepository) seedIfEmpty(ctx context.Context) error {
    count, countErr := instance.database.
        NewSelect().
        Model((*productRow)(nil)).
        Count(ctx)
    if nil != countErr {
        return countErr
    }

    if 0 < count {
        return nil
    }

    seedList := seedProductList(time.Now())
    rowList := make([]*productRow, 0, len(seedList))
    for _, product := range seedList {
        rowList = append(rowList, newProductRow(product))
    }

    _, insertErr := instance.database.
        NewInsert().
        Model(&rowList).
        Ignore().
        Exec(ctx)

    return insertErr
}

func (instance *bunProductRepository) All(ctx context.Context) ([]*entity.Product, error) {
    rowList := make([]*productRow, 0)

    selectErr := instance.database.
        NewSelect().
        Model(&rowList).
        Order("created_at ASC", "id ASC").
        Scan(ctx)
    if nil != selectErr {
        return nil, selectErr
    }

    products := make([]*entity.Product, 0, len(rowList))
    for _, row := range rowList {
        products = append(products, row.toEntity())
    }

    return products, nil
}

func (instance *bunProductRepository) FindById(ctx context.Context, id string) (*entity.Product, bool, error) {
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
func (instance *bunProductRepository) findRowById(ctx context.Context, id string) (*productRow, bool, error) {
    row := &productRow{}

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

func (instance *bunProductRepository) Create(ctx context.Context, product *entity.Product) error {
    validationErr := validateProduct(product)
    if nil != validationErr {
        return validationErr
    }

    if "" == strings.TrimSpace(product.Id) {
        identifierList, identifierErr := instance.identifierList(ctx)
        if nil != identifierErr {
            return identifierErr
        }

        product.Id = nextProductId(identifierList)
    }

    _, exists, existsErr := instance.findRowById(ctx, product.Id)
    if nil != existsErr {
        return existsErr
    }

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

    _, insertErr := instance.database.
        NewInsert().
        Model(newProductRow(product)).
        Exec(ctx)

    return insertErr
}

func (instance *bunProductRepository) Update(ctx context.Context, product *entity.Product) (bool, error) {
    validationErr := validateProduct(product)
    if nil != validationErr {
        return false, validationErr
    }

    id := strings.TrimSpace(product.Id)
    if "" == id {
        return false, fmt.Errorf("id is required")
    }

    existing, found, findErr := instance.findRowById(ctx, id)
    if nil != findErr {
        return false, findErr
    }

    if false == found {
        return false, nil
    }

    if true == product.CreatedAt.IsZero() {
        product.CreatedAt = existing.CreatedAt
    }

    if true == product.UpdatedAt.IsZero() {
        product.UpdatedAt = time.Now()
    }

    result, updateErr := instance.database.
        NewUpdate().
        Model(newProductRow(product)).
        WherePK().
        Exec(ctx)
    if nil != updateErr {
        return false, updateErr
    }

    return affectedAtLeastOneRow(result), nil
}

func (instance *bunProductRepository) DeleteById(ctx context.Context, id string) (bool, error) {
    normalizedId := strings.TrimSpace(id)
    if "" == normalizedId {
        return false, fmt.Errorf("id is required")
    }

    result, deleteErr := instance.database.
        NewDelete().
        Model((*productRow)(nil)).
        Where("id = ?", normalizedId).
        Exec(ctx)
    if nil != deleteErr {
        return false, deleteErr
    }

    return affectedAtLeastOneRow(result), nil
}

func (instance *bunProductRepository) identifierList(ctx context.Context) ([]string, error) {
    identifierList := make([]string, 0)

    selectErr := instance.database.
        NewSelect().
        Model((*productRow)(nil)).
        Column("id").
        Scan(ctx, &identifierList)
    if nil != selectErr {
        return nil, selectErr
    }

    return identifierList, nil
}

var _ ProductRepository = (*bunProductRepository)(nil)
