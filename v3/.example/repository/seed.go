package repository

import (
    "context"
    "time"

    "github.com/precision-soft/melody/v3/.example/entity"
    "github.com/precision-soft/melody/v3/.example/persistence"
    "github.com/precision-soft/melody/v3/.example/security"
)

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

var seedRateAsOf = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

func seedCurrencyList() []*entity.Currency {
    return []*entity.Currency{
        entity.NewCurrency("cur-eur", entity.RateBaseCurrencyCode, "Euro", 1, seedRateAsOf),
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

/* SeedAll seeds each empty catalogue table using the same seed lists as repository construction. Tables containing rows remain unchanged. */
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
