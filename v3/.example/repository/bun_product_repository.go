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
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/uptrace/bun"
)

/* productRow is the catalogue as the database holds it; the entity stays free of storage concerns because it is cached through a gob serializer. */
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
    return &bunProductRepository{database: storage.Database(), tracker: storage.Tracker(), recorder: storage.Recorder()}
}

/* bunProductRepository keeps the catalogue in the database and its history beside it. Every write goes through the audit tracker, which performs it and records the field-level change in one transaction, so the catalogue never holds a version of a row the trail cannot account for. */
/* productIdentifierMintLockName names the advisory lock the creates of melody_example_v3_product mint their identifiers under */
const productIdentifierMintLockName = "melody_example_v3_product.id"

type bunProductRepository struct {
    database *bun.DB
    tracker  *melodyaudit.Tracker
    recorder *melodyaudit.Recorder
}

/* auditContext names whoever is behind the write for the trail; a write with nobody on the context is a scheduled or console one, recorded as the system. */
func auditContext(ctx context.Context) context.Context {
    actor := persistence.ActorFromContext(ctx)
    if "" == actor {
        actor = CatalogJournalActorSystem
    }

    return melodyaudit.WithActor(ctx, actor)
}

func (instance *bunProductRepository) seedIfEmpty(ctx context.Context) error {
    identifierList := make([]string, 0)
    for _, product := range seedProductList(time.Now()) {
        identifierList = append(identifierList, product.Id)
    }

    if raiseErr := raiseSequenceOverSeeds(ctx, instance.database, "prod-", identifierList); nil != raiseErr {
        return raiseErr
    }

    return seedIfEmptyAudited(ctx, instance.database, instance.recorder, persistence.AuditEntityProduct, func() []*productRow {
        seedList := seedProductList(time.Now())
        rowList := make([]*productRow, 0, len(seedList))
        for _, product := range seedList {
            rowList = append(rowList, newProductRow(product))
        }

        return rowList
    }, func(row *productRow) string {
        return row.Id
    })
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

/* findRowById separates a row that is not there from a query that could not run: only sql.ErrNoRows is an answer. */
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

    mintsIdentifier := "" == strings.TrimSpace(product.Id)
    if false == mintsIdentifier {
        if ceilingErr := refuseIdentifierAtCeiling(product.Id, "prod-"); nil != ceilingErr {
            return ceilingErr
        }

        _, exists, existsErr := instance.findRowById(ctx, product.Id)
        if nil != existsErr {
            return existsErr
        }

        if true == exists {
            return ErrIdAlreadyExists
        }
    }

    now := time.Now()
    if true == product.CreatedAt.IsZero() {
        product.CreatedAt = now
    }
    if true == product.UpdatedAt.IsZero() {
        product.UpdatedAt = now
    }

    return insertWithMintedIdentifier(
        ctx,
        instance.database,
        productIdentifierMintLockName,
        identifierSequence{prefix: "prod-", identifier: func() string { return product.Id }},
        mintsIdentifier,
        func(floor string) error {
            identifierList, identifierErr := instance.identifierList(ctx)
            if nil != identifierErr {
                return identifierErr
            }

            product.Id = nextProductId(append(identifierList, floor))

            return nil
        },
        func() error {
            return asProductReferenceRefusal(instance.tracker.Insert(auditContext(ctx), persistence.AuditEntityProduct, product.Id, newProductRow(product)))
        },
    )
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

    /* the tracker loads the before-image itself, so the trail records the row as the database held it rather than what the caller passed */
    updateErr := instance.tracker.Update(auditContext(ctx), persistence.AuditEntityProduct, id, newProductRow(product))
    if nil != updateErr {
        return false, asProductReferenceRefusal(updateErr)
    }

    return true, nil
}

func (instance *bunProductRepository) DeleteById(ctx context.Context, id string) (bool, error) {
    normalizedId := strings.TrimSpace(id)
    if "" == normalizedId {
        return false, fmt.Errorf("id is required")
    }

    /* the row is read FOR UPDATE inside the transaction that deletes it, so two concurrent deletes serialise on it: the second finds no row and answers false, and raises no event. The entry is recorded through the same transaction; a product captures no before-image, as the tracker recorded none. */
    deleted := false

    txErr := instance.database.RunInTx(auditContext(ctx), nil, func(ctx context.Context, tx bun.Tx) error {
        row := &productRow{Id: normalizedId}

        selectErr := tx.NewSelect().Model(row).WherePK().For("UPDATE").Scan(ctx)
        if true == errors.Is(selectErr, sql.ErrNoRows) {
            return nil
        }
        if nil != selectErr {
            return selectErr
        }

        if _, deleteErr := tx.NewDelete().Model(row).WherePK().Exec(ctx); nil != deleteErr {
            return deleteErr
        }

        if recordErr := instance.recorder.RecordDelete(melodyaudit.WithDatabase(ctx, tx), persistence.AuditEntityProduct, normalizedId, nil); nil != recordErr {
            return recordErr
        }

        deleted = true

        return nil
    })
    if nil != txErr {
        return false, exception.NewError(
            "deleting the "+persistence.AuditEntityProduct+" "+normalizedId+" did not complete",
            exceptioncontract.Context{"entity": persistence.AuditEntityProduct, "operation": "delete", "id": normalizedId},
            txErr,
        )
    }

    return deleted, nil
}

func (instance *bunProductRepository) PricedIn(ctx context.Context, currencyId string) (bool, error) {
    return instance.database.
        NewSelect().
        Model((*productRow)(nil)).
        Where("currency_id = ?", currencyId).
        Exists(ctx)
}

func (instance *bunProductRepository) CategorizedIn(ctx context.Context, categoryId string) (bool, error) {
    return instance.database.
        NewSelect().
        Model((*productRow)(nil)).
        Where("category_id = ?", categoryId).
        Exists(ctx)
}

/* HoldingReferences runs the action as given: the product table's foreign keys hold the references against a concurrent write */
func (instance *bunProductRepository) HoldingReferences(action func() error) error {
    return action()
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
