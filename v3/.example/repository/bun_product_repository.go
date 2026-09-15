package repository

import (
    "context"
    "database/sql"
    "errors"
    "fmt"
    "strings"
    "time"

    melodyaudit "github.com/precision-soft/melody/integrations/bunorm/v3/audit"
    "github.com/precision-soft/melody/v3/.example/entity"
    "github.com/precision-soft/melody/v3/.example/persistence"
    "github.com/uptrace/bun"
)

type productRow struct {
    bun.BaseModel `bun:"table:melody_example_v3_product,alias:product"`

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

func newBunProductRepository(storage *persistence.CatalogStorage) *bunProductRepository {
    return &bunProductRepository{database: storage.Database(), tracker: storage.Tracker()}
}

type bunProductRepository struct {
    database *bun.DB
    tracker  *melodyaudit.Tracker
}

func auditContext(ctx context.Context) context.Context {
    actor := persistence.ActorFromContext(ctx)
    if "" == actor {
        actor = CatalogJournalActorSystem
    }

    return melodyaudit.WithActor(ctx, actor)
}

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

func (instance *bunProductRepository) findRowById(ctx context.Context, id string) (*productRow, bool, error) {
    row := &productRow{}

    selectErr := instance.database.
        NewSelect().
        Model(row).
        Where("id = ? AND BINARY id = BINARY ?", id, id).
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

    return instance.tracker.Insert(auditContext(ctx), persistence.AuditEntityProduct, product.Id, newProductRow(product))
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

    updateErr := instance.tracker.Update(auditContext(ctx), persistence.AuditEntityProduct, id, newProductRow(product))
    if nil != updateErr {
        return false, updateErr
    }

    return true, nil
}

func (instance *bunProductRepository) DeleteById(ctx context.Context, id string) (bool, error) {
    normalizedId := strings.TrimSpace(id)
    if "" == normalizedId {
        return false, fmt.Errorf("id is required")
    }

    _, found, findErr := instance.findRowById(ctx, normalizedId)
    if nil != findErr {
        return false, findErr
    }

    if false == found {
        return false, nil
    }

    deleteErr := instance.tracker.Delete(
        auditContext(ctx),
        persistence.AuditEntityProduct,
        normalizedId,
        &productRow{Id: normalizedId},
    )
    if nil != deleteErr {
        return false, deleteErr
    }

    return true, nil
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
