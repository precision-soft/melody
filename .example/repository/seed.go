package repository

import (
    "context"
    "time"

    "github.com/precision-soft/melody/.example/entity"
    "github.com/precision-soft/melody/.example/security"
    "github.com/uptrace/bun"
)

/* The catalogue the example opens with. Both implementations start from it: the in-memory one holds it for the life of the process, and the database-backed one writes it once into an empty table, so the application shows the same nomenclature whichever way it was configured. */

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

func seedCurrencyList() []*entity.Currency {
    return []*entity.Currency{
        entity.NewCurrency("cur-eur", "EUR", "Euro"),
        entity.NewCurrency("cur-usd", "USD", "US Dollar"),
        entity.NewCurrency("cur-ron", "RON", "Romanian Leu"),
    }
}

/* seedUserList hashes at every call, so two calls answer different bytes for the same credentials — bcrypt salts anew. Nothing may compare seeded hashes across calls; the seeded PASSWORDS are the stable contract, and the e2e credentials depend on them. */
func seedUserList() []*entity.User {
    return []*entity.User{
        entity.NewUser("user-1", "user", security.MustHashPassword("user"), []string{entity.RoleUser}),
        entity.NewUser("user-2", "editor", security.MustHashPassword("editor"), []string{entity.RoleUser, entity.RoleEditor}),
        entity.NewUser("user-3", "admin", security.MustHashPassword("admin"), []string{entity.RoleUser, entity.RoleEditor, entity.RoleAdmin}),
    }
}

/* SeedAll writes the opening state of every nomenclature this application ships with, over a database that
   has just been brought to the schema. It is the door example:db:reset uses, and it does exactly what the
   repository providers do at first resolution — the same seedIfEmpty over the same four repositories and
   the same seed lists above — because a reset has to leave the application in the state a fresh volume
   would be in, not in a second, hand-written version of it.

   Each of the four is a no-op over a table that already holds rows, so calling this over a database that
   was not reset changes nothing. */
func SeedAll(ctx context.Context, database *bun.DB) error {
    seedList := []func(ctx context.Context) error{
        NewBunCategoryRepository(database).seedIfEmpty,
        NewBunCurrencyRepository(database).seedIfEmpty,
        NewBunProductRepository(database).seedIfEmpty,
        NewBunUserRepository(database).seedIfEmpty,
    }

    for _, seed := range seedList {
        if seedErr := seed(ctx); nil != seedErr {
            return seedErr
        }
    }

    return nil
}
