package repository

import (
    "context"
    "fmt"
    "strings"

    "github.com/precision-soft/melody/.example/entity"
    "github.com/precision-soft/melody/.example/migration"
    melodycontainer "github.com/precision-soft/melody/container"
    melodycontainercontract "github.com/precision-soft/melody/container/contract"
    "github.com/uptrace/bun"
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

/* CurrencyRepositoryProvider hands back the nomenclature the environment can actually support: the database-backed one when the configuration published a connection, and the in-memory one otherwise. An empty database service name is how the configuration says there is no connection. */
func CurrencyRepositoryProvider(databaseServiceName string) melodycontainercontract.Provider[CurrencyRepository] {
    return func(resolver melodycontainercontract.Resolver) (CurrencyRepository, error) {
        if "" == databaseServiceName {
            return NewInMemoryCurrencyRepository(), nil
        }

        database, databaseErr := melodycontainer.FromResolver[*bun.DB](resolver, databaseServiceName)
        if nil != databaseErr {
            return nil, databaseErr
        }

        if migrateErr := migration.EnsureMigrated(context.Background(), database); nil != migrateErr {
            return nil, migrateErr
        }

        repositoryInstance := NewBunCurrencyRepository(database)

        if seedErr := repositoryInstance.seedIfEmpty(context.Background()); nil != seedErr {
            return nil, seedErr
        }

        return repositoryInstance, nil
    }
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
