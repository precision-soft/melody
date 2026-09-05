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

/* CurrencyRepository carries a context and an error on every method because one of its implementations talks to a database: a listing that cannot reach mysql has to say so rather than answer with an empty nomenclature, and a request that was cancelled has to stop the query it started. */
type CurrencyRepository interface {
    All(ctx context.Context) ([]*entity.Currency, error)

    FindById(ctx context.Context, id string) (*entity.Currency, bool, error)

    Create(ctx context.Context, currency *entity.Currency) error

    Update(ctx context.Context, currency *entity.Currency) (bool, error)

    DeleteById(ctx context.Context, id string) (bool, error)
}

func MustGetCurrencyRepository(resolver melodycontainercontract.Resolver) CurrencyRepository {
    return melodycontainer.MustFromResolver[CurrencyRepository](resolver, ServiceCurrencyRepository)
}

/* NewCurrencyRepository hands back the nomenclature the environment can actually support: the database-backed one when a connection was configured, and the in-memory one otherwise. The choice is made here rather than in the configuration because the generated wiring fills this constructor from the container, and the storage handle is what carries the answer.

   The migration set is applied and the table seeded on the way out, so the first caller finds a nomenclature rather than an empty one. */
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

/* validateCurrency reports the first field the currency fails on, shared by both implementations so a bad write is refused with the same words whichever one the environment picked. */
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
