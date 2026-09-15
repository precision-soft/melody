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

/* CurrencyRepository propagates database failures and caller cancellation rather than treating them as missing data. */
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

/* NewCurrencyRepository selects persistent or in-memory storage. Persistent construction applies migrations and seeds an empty table. */
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
