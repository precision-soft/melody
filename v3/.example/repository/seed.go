package repository

import (
    "context"
    "time"

    "github.com/precision-soft/melody/v3/.example/entity"
    "github.com/precision-soft/melody/v3/.example/persistence"
    "github.com/precision-soft/melody/v3/.example/security"
)

/* the catalogue the application opens with: the in-memory repository holds it for the life of the process, and the database-backed one writes it once into an empty table */

func seedProductList(now time.Time) []*entity.Product {
    return []*entity.Product{
        entity.NewProduct(
            "prod-1",
            "DDR5 32GB Dual Kit 6000MT/s",
            "black",
            "cat-1",
            149.99,
            "cur-eur",
            12,
            now.Add(-240*time.Hour),
            now.Add(-12*time.Hour),
        ),
        entity.NewProduct(
            "prod-2",
            "DDR5 64GB Dual Kit 5600MT/s",
            "black",
            "cat-1",
            259.99,
            "cur-eur",
            6,
            now.Add(-232*time.Hour),
            now.Add(-24*time.Hour),
        ),
        entity.NewProduct(
            "prod-3",
            "DDR4 32GB Dual Kit 3600MT/s",
            "black",
            "cat-1",
            99.99,
            "cur-eur",
            18,
            now.Add(-220*time.Hour),
            now.Add(-48*time.Hour),
        ),
        entity.NewProduct(
            "prod-4",
            "NVMe SSD 2TB Gen4",
            "silver",
            "cat-2",
            129.99,
            "cur-usd",
            9,
            now.Add(-210*time.Hour),
            now.Add(-72*time.Hour),
        ),
        entity.NewProduct(
            "prod-5",
            "Mechanical Keyboard TKL",
            "white",
            "cat-3",
            79.99,
            "cur-ron",
            25,
            now.Add(-200*time.Hour),
            now.Add(-96*time.Hour),
        ),
    }
}

func seedCategoryList() []*entity.Category {
    return []*entity.Category{
        entity.NewCategory("cat-1", "Memory"),
        entity.NewCategory("cat-2", "Storage"),
        entity.NewCategory("cat-3", "Graphics"),
        entity.NewCategory("cat-4", "Power"),
    }
}

/* seedRateAsOf is the instant the opening rates carry, a fixed past moment rather than the boot instant: the seeded rates are the state the application ships with, so a refresh is visible as a change in both the number and the instant. */
var seedRateAsOf = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

/* the rates are quoted against the euro, one euro costing this many units of the currency; the refresh refuses a document quoted against any base but RATES_BASE_CURRENCY, EUR by default to match this seed */
func seedCurrencyList() []*entity.Currency {
    return []*entity.Currency{
        entity.NewCurrency("cur-eur", "EUR", "Euro", 1, seedRateAsOf),
        entity.NewCurrency("cur-usd", "USD", "US Dollar", 1.1, seedRateAsOf),
        entity.NewCurrency("cur-ron", "RON", "Romanian Leu", 5.05, seedRateAsOf),
    }
}

func seedUserList() []*entity.User {
    return []*entity.User{
        entity.NewUser("user-1", "user", security.MustHashPassword("user"), []string{entity.RoleUser}),
        entity.NewUser("user-2", "editor", security.MustHashPassword("editor"), []string{entity.RoleUser, entity.RoleEditor}),
        entity.NewUser("user-3", "admin", security.MustHashPassword("admin"), []string{entity.RoleUser, entity.RoleEditor, entity.RoleAdmin}),
    }
}

/* SeedAll writes the opening state of every nomenclature over a database just brought to the schema, the door example:db:reset uses. It runs the same seedIfEmpty over the same four repositories as their constructors, so a reset leaves the state of a fresh volume; over tables that hold rows it changes nothing. */
func SeedAll(ctx context.Context, storage *persistence.CatalogStorage) error {
    seedList := []func(ctx context.Context) error{
        newBunCategoryRepository(storage.Database()).seedIfEmpty,
        newBunCurrencyRepository(storage.Database()).seedIfEmpty,
        newBunProductRepository(storage).seedIfEmpty,
        newBunUserRepository(storage).seedIfEmpty,
    }

    for _, seed := range seedList {
        if seedErr := seed(ctx); nil != seedErr {
            return seedErr
        }
    }

    return nil
}
