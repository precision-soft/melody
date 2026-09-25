package repository

import (
    "context"
    "fmt"
    "strings"

    "github.com/precision-soft/melody/v3/.example/entity"
    "github.com/precision-soft/melody/v3/.example/migration"
    "github.com/precision-soft/melody/v3/.example/persistence"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
)

const (
    ServiceCurrencyRepository = "service.example.currency.repository"
)

/* CurrencyRepository carries a context and an error on every method, so a listing that cannot reach mysql says so rather than answering an empty nomenclature, and a cancelled request stops its query. */
type CurrencyRepository interface {
    All(ctx context.Context) ([]*entity.Currency, error)

    FindById(ctx context.Context, id string) (*entity.Currency, bool, error)

    Create(ctx context.Context, currency *entity.Currency) error

    /* Update writes the currency's code and name onto its row and answers whether a row was there. The quote is not written here, so a rename cannot put back a quote older than one the refresh wrote since; the quote has its own conditional door, UpdateQuote. */
    Update(ctx context.Context, currency *entity.Currency) (bool, error)

    /* UpdateQuote writes a quote onto the row only if the row does not hold a newer reading on this application's clock, in one statement, and answers whether it wrote. The reading the row already names, the same provider stamp, may be re-quoted at another rate; the quote the row already holds is not written again. A false answer means the row is absent, newer, or already holds the quote, and the caller reads it back to tell which. */
    UpdateQuote(ctx context.Context, id string, quote entity.RateQuote) (bool, error)

    DeleteById(ctx context.Context, id string) (bool, error)
}

func MustGetCurrencyRepository(resolver melodycontainercontract.Resolver) CurrencyRepository {
    return melodycontainer.MustFromResolver[CurrencyRepository](resolver, ServiceCurrencyRepository)
}

/* NewCurrencyRepository answers the database-backed repository when a connection is configured and the in-memory one otherwise; the generated wiring fills it from the container, and the storage handle carries the choice. The migration set is applied and the table seeded on the way out. */
//melody:service ServiceCurrencyRepository
func NewCurrencyRepository(storage *persistence.CatalogStorage) (CurrencyRepository, error) {
    if false == storage.IsPersistent() {
        return newInMemoryCurrencyRepository(), nil
    }

    migrateErr := migration.EnsureMigrated(context.Background(), storage.Database())
    if nil != migrateErr {
        return nil, migrateErr
    }

    repositoryInstance := newBunCurrencyRepository(storage.Database())

    seedErr := repositoryInstance.seedIfEmpty(context.Background())
    if nil != seedErr {
        return nil, seedErr
    }

    return repositoryInstance, nil
}

/* validateCurrency reports the first field the currency fails on, shared by both implementations so they refuse with the same words. */
func validateCurrency(currency *entity.Currency) error {
    if nil == currency {
        return fmt.Errorf("currency is required")
    }

    if "" == strings.TrimSpace(currency.Code) {
        return fmt.Errorf("code is required")
    }

    if "" == strings.TrimSpace(currency.Name) {
        return fmt.Errorf("name is required")
    }

    return nil
}

func nextCurrencyId(existingIdList []string) string {
    return fmt.Sprintf("cur-%d", highestIdSuffix(existingIdList, "cur-")+1)
}
