package repository

import (
    "context"
    "database/sql"
    "errors"
    "fmt"
    "strings"

    "github.com/precision-soft/melody/v3/.example/entity"
    "github.com/uptrace/bun"
)

/* currencyRow is the nomenclature as the database holds it; the domain entity stays free of storage concerns because it is cached through a gob serializer. */
type currencyRow struct {
    bun.BaseModel `bun:"table:melody_example_v3_currency,alias:currency"`

    Id   string `bun:"id,pk"`
    Code string `bun:"code,notnull"`
    Name string `bun:"name,notnull"`
}

func newCurrencyRow(currency *entity.Currency) *currencyRow {
    return &currencyRow{
        Id:   currency.Id,
        Code: currency.Code,
        Name: currency.Name,
    }
}

func (instance *currencyRow) toEntity() *entity.Currency {
    return entity.NewCurrency(instance.Id, instance.Code, instance.Name)
}

func newBunCurrencyRepository(database *bun.DB) *bunCurrencyRepository {
    return &bunCurrencyRepository{database: database}
}

type bunCurrencyRepository struct {
    database *bun.DB
}

/* EnsureSchema creates the table when it is absent and writes the opening nomenclature into it when it is empty. The seeding insert ignores duplicate keys because several example applications may reach an empty table at the same time, and losing that race is not a failure. */
func (instance *bunCurrencyRepository) EnsureSchema(ctx context.Context) error {
    _, createErr := instance.database.
        NewCreateTable().
        Model((*currencyRow)(nil)).
        IfNotExists().
        Exec(ctx)
    if nil != createErr {
        return createErr
    }

    count, countErr := instance.database.
        NewSelect().
        Model((*currencyRow)(nil)).
        Count(ctx)
    if nil != countErr {
        return countErr
    }

    if 0 < count {
        return nil
    }

    seedList := seedCurrencyList()
    rowList := make([]*currencyRow, 0, len(seedList))
    for _, currency := range seedList {
        rowList = append(rowList, newCurrencyRow(currency))
    }

    _, insertErr := instance.database.
        NewInsert().
        Model(&rowList).
        Ignore().
        Exec(ctx)

    return insertErr
}

func (instance *bunCurrencyRepository) All(ctx context.Context) ([]*entity.Currency, error) {
    rowList := make([]*currencyRow, 0)

    selectErr := instance.database.
        NewSelect().
        Model(&rowList).
        Order("id ASC").
        Scan(ctx)
    if nil != selectErr {
        return nil, selectErr
    }

    currencies := make([]*entity.Currency, 0, len(rowList))
    for _, row := range rowList {
        currencies = append(currencies, row.toEntity())
    }

    return currencies, nil
}

func (instance *bunCurrencyRepository) FindById(ctx context.Context, id string) (*entity.Currency, bool, error) {
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
func (instance *bunCurrencyRepository) findRowById(ctx context.Context, id string) (*currencyRow, bool, error) {
    row := &currencyRow{}

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

func (instance *bunCurrencyRepository) Create(ctx context.Context, currency *entity.Currency) error {
    validationErr := validateCurrency(currency)
    if nil != validationErr {
        return validationErr
    }

    if "" == strings.TrimSpace(currency.Id) {
        identifierList, identifierErr := instance.identifierList(ctx)
        if nil != identifierErr {
            return identifierErr
        }

        currency.Id = nextCurrencyId(identifierList)
    }

    _, exists, existsErr := instance.findRowById(ctx, currency.Id)
    if nil != existsErr {
        return existsErr
    }

    if true == exists {
        return fmt.Errorf("id already exists")
    }

    _, insertErr := instance.database.
        NewInsert().
        Model(newCurrencyRow(currency)).
        Exec(ctx)

    return insertErr
}

func (instance *bunCurrencyRepository) Update(ctx context.Context, currency *entity.Currency) (bool, error) {
    validationErr := validateCurrency(currency)
    if nil != validationErr {
        return false, validationErr
    }

    id := strings.TrimSpace(currency.Id)
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
        Model(newCurrencyRow(currency)).
        WherePK().
        Exec(ctx)
    if nil != updateErr {
        return false, updateErr
    }

    return affectedAtLeastOneRow(result), nil
}

func (instance *bunCurrencyRepository) DeleteById(ctx context.Context, id string) (bool, error) {
    normalizedId := strings.TrimSpace(id)
    if "" == normalizedId {
        return false, fmt.Errorf("id is required")
    }

    result, deleteErr := instance.database.
        NewDelete().
        Model((*currencyRow)(nil)).
        Where("id = ?", normalizedId).
        Exec(ctx)
    if nil != deleteErr {
        return false, deleteErr
    }

    return affectedAtLeastOneRow(result), nil
}

func (instance *bunCurrencyRepository) identifierList(ctx context.Context) ([]string, error) {
    identifierList := make([]string, 0)

    selectErr := instance.database.
        NewSelect().
        Model((*currencyRow)(nil)).
        Column("id").
        Scan(ctx, &identifierList)
    if nil != selectErr {
        return nil, selectErr
    }

    return identifierList, nil
}

var _ CurrencyRepository = (*bunCurrencyRepository)(nil)
