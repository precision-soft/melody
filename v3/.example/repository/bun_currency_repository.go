package repository

import (
    "context"
    "database/sql"
    "errors"
    "fmt"
    "strings"
    "time"

    "github.com/precision-soft/melody/v3/.example/entity"
    "github.com/uptrace/bun"
)

/* currencyRow is the nomenclature as the database holds it; the domain entity stays free of storage concerns because it is cached through a gob serializer. */
type currencyRow struct {
    bun.BaseModel `bun:"table:melody_example_v3_currency,alias:currency"`

    Id               string    `bun:"id,pk"`
    Code             string    `bun:"code,notnull"`
    Name             string    `bun:"name,notnull"`
    Rate             float64   `bun:"rate,notnull"`
    RateAsOf         time.Time `bun:"rate_as_of,notnull"`
    ProviderRateAsOf time.Time `bun:"provider_rate_as_of,notnull"`
}

/* newCurrencyRow is the one place an entity becomes a row, and the instant is moved to UTC here: the mysql
   dialect renders a time.Time in the value's OWN location when no location is configured, and the driver
   reads the column back as UTC, so an instant a provider stamped with an offset was stored as its wall clock
   and read back shifted by that offset. The in-memory repository keeps the value as given, which is right
   there — an instant compares equal across locations — and the two agree once the row carries UTC. */
func newCurrencyRow(currency *entity.Currency) *currencyRow {
    return &currencyRow{
        Id:               currency.Id,
        Code:             currency.Code,
        Name:             currency.Name,
        Rate:             currency.Rate,
        RateAsOf:         currency.RateAsOf.UTC(),
        ProviderRateAsOf: currency.ProviderRateAsOf.UTC(),
    }
}

func (instance *currencyRow) toEntity() *entity.Currency {
    return entity.NewQuotedCurrency(instance.Id, instance.Code, instance.Name, entity.NewRateQuote(instance.Rate, instance.RateAsOf, instance.ProviderRateAsOf))
}

func newBunCurrencyRepository(database *bun.DB) *bunCurrencyRepository {
    return &bunCurrencyRepository{database: database}
}

type bunCurrencyRepository struct {
    database *bun.DB
}

func (instance *bunCurrencyRepository) seedIfEmpty(ctx context.Context) error {
    return seedIfEmptyRows(ctx, instance.database, func() []*currencyRow {
        seedList := seedCurrencyList()
        rowList := make([]*currencyRow, 0, len(seedList))
        for _, currency := range seedList {
            rowList = append(rowList, newCurrencyRow(currency))
        }

        return rowList
    })
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

func (instance *bunCurrencyRepository) UpdateQuote(ctx context.Context, id string, quote entity.RateQuote) (bool, error) {
    normalizedId := strings.TrimSpace(id)
    if "" == normalizedId {
        return false, fmt.Errorf("id is required")
    }

    result, updateErr := instance.updateQuoteQuery(normalizedId, quote).Exec(ctx)
    if nil != updateErr {
        return false, updateErr
    }

    return affectedAtLeastOneRow(result), nil
}

/* updateQuoteQuery is the conditional write of UpdateQuote, the repository contract in one statement. The instants
   are written and compared in UTC, the spelling the row holds, and the condition is what makes two concurrent
   documents land in reading order whichever process writes last: the row is written when it does not hold a
   newer reading on this clock, or when it names the same reading the provider re-quotes, and never when it
   already holds the quote. */
func (instance *bunCurrencyRepository) updateQuoteQuery(id string, quote entity.RateQuote) *bun.UpdateQuery {
    asOf := quote.AsOf.UTC()
    providerAsOf := quote.ProviderAsOf.UTC()

    return instance.database.
        NewUpdate().
        Model((*currencyRow)(nil)).
        Set("rate = ?", quote.Rate).
        Set("rate_as_of = ?", asOf).
        Set("provider_rate_as_of = ?", providerAsOf).
        Where("id = ?", id).
        Where("(rate_as_of <= ? OR provider_rate_as_of = ?)", asOf, providerAsOf).
        Where("NOT (rate = ? AND provider_rate_as_of = ?)", quote.Rate, providerAsOf)
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
