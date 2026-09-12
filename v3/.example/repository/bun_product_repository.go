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

/* productRow is the catalogue as the database holds it. The domain entity stays free of storage concerns because it is cached through a gob serializer, so the mapping lives here, beside the repository that performs it. */
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

/* bunProductRepository keeps the catalogue in the database and its history beside it. Every write goes through the audit tracker, which performs the write and records the field-level change in one transaction: an entry that failed to persist rolls the change back with it, so the catalogue never holds a version of a row the trail cannot account for. */
type bunProductRepository struct {
    database *bun.DB
    tracker  *melodyaudit.Tracker
}

/* auditContext names whoever is behind the write for the trail. The actor is put on the context by the service layer, which is the last place that still knows which request it is serving; a write with nobody on the context is a scheduled or console one, and the trail says so rather than leaving the column empty. */
func auditContext(ctx context.Context) context.Context {
    actor := persistence.ActorFromContext(ctx)
    if "" == actor {
        actor = CatalogJournalActorSystem
    }

    return melodyaudit.WithActor(ctx, actor)
}

/* seedIfEmpty writes the opening catalogue into an empty table; the table itself belongs to the migration set the constructor has already applied. The insert ignores duplicate keys because several example applications may reach an empty table at the same time, and losing that race is not a failure. */
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

    /* the row is known to be there — it was just read — so the tracker's own load of the before-image cannot come up empty; what it adds is that the recorded before-image is the row as the DATABASE held it rather than whatever the caller passed */
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

    /* the tracker deletes by primary key and treats a row that was not there as nothing to do, so whether there was one is asked first: the caller's answer distinguishes a delete that happened from a request for a product that does not exist, and a silent no-op cannot tell them apart */
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
