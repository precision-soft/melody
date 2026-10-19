package repository

import (
    "context"
    "time"

    "github.com/precision-soft/melody/v3/.example/entity"
    "github.com/precision-soft/melody/v3/.example/persistence"
    "github.com/precision-soft/melody/v3/.example/security"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
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

/* every account holds the one role that names its position; the role hierarchy config/security.go declares (admin above editor above user) is what lets the admin edit and the editor read, so a role added later is one line of the hierarchy rather than an edit of every bundle */
func seedUserList() []*entity.User {
    return []*entity.User{
        entity.NewUser("user-1", "user", security.MustHashPassword("user"), []string{entity.RoleUser}),
        entity.NewUser("user-2", "editor", security.MustHashPassword("editor"), []string{entity.RoleEditor}),
        entity.NewUser("user-3", "admin", security.MustHashPassword("admin"), []string{entity.RoleAdmin}),
    }
}

/* Seeder writes the opening state of one nomenclature into its table when the table is empty and leaves a table holding rows unchanged. Every seeder is registered in the container under a collection priority, and example:db:reset collects them in that order, so a nomenclature another one refers to is seeded first and a new nomenclature joins the reset by registering its seeder rather than by editing a list. */
type Seeder interface {
    SeedIfEmpty(ctx context.Context) error
}

/* the seeder services and their collection priorities: a product names its category and its currency, so those two are seeded first and a reset reads in the order the rows refer to each other. The schema carries no foreign key between the four tables, so the order is declared here rather than enforced by the database */
const (
    ServiceCategorySeeder = "service.example.seeder.category"
    ServiceCurrencySeeder = "service.example.seeder.currency"
    ServiceProductSeeder  = "service.example.seeder.product"
    ServiceUserSeeder     = "service.example.seeder.user"

    CategorySeederPriority = 40
    CurrencySeederPriority = 30
    ProductSeederPriority  = 20
    UserSeederPriority     = 10
)

type seederFunc func(ctx context.Context) error

func (instance seederFunc) SeedIfEmpty(ctx context.Context) error {
    return instance(ctx)
}

/* the four seeders run the same seedIfEmpty as the repositories' constructors, over the database of a persistent storage, so a reset leaves the state of a fresh volume */
func newCategorySeeder(storage *persistence.CatalogStorage) Seeder {
    return seederFunc(newBunCategoryRepository(storage.Database()).seedIfEmpty)
}

func newCurrencySeeder(storage *persistence.CatalogStorage) Seeder {
    return seederFunc(newBunCurrencyRepository(storage.Database()).seedIfEmpty)
}

func newProductSeeder(storage *persistence.CatalogStorage) Seeder {
    return seederFunc(newBunProductRepository(storage).seedIfEmpty)
}

func newUserSeeder(storage *persistence.CatalogStorage) Seeder {
    return seederFunc(newBunUserRepository(storage).seedIfEmpty)
}

/* RegisterSeeders registers the four nomenclature seeders example:db:reset collects, each under the priority that seeds what another refers to first. They share the Seeder type, so the type registration is not strict. A seeder builds on the catalogue storage when it is first collected, and only a reset over a persistent storage collects them. */
func RegisterSeeders(registrar melodycontainercontract.Registrar) {
    seederList := []struct {
        serviceName string
        priority    int
        constructor func(storage *persistence.CatalogStorage) Seeder
    }{
        {serviceName: ServiceCategorySeeder, priority: CategorySeederPriority, constructor: newCategorySeeder},
        {serviceName: ServiceCurrencySeeder, priority: CurrencySeederPriority, constructor: newCurrencySeeder},
        {serviceName: ServiceProductSeeder, priority: ProductSeederPriority, constructor: newProductSeeder},
        {serviceName: ServiceUserSeeder, priority: UserSeederPriority, constructor: newUserSeeder},
    }

    for _, seeder := range seederList {
        constructor := seeder.constructor

        melodycontainer.MustRegister[Seeder](
            registrar,
            seeder.serviceName,
            func(resolver melodycontainercontract.Resolver) (Seeder, error) {
                storage, storageErr := melodycontainer.FromResolver[*persistence.CatalogStorage](resolver, persistence.ServiceCatalogStorage)
                if nil != storageErr {
                    return nil, storageErr
                }

                return constructor(storage), nil
            },
            melodycontainer.WithTypeRegistration(false),
            melodycontainer.WithCollectionPriority(seeder.priority),
        )
    }
}

/* SeedAll writes the opening state of every nomenclature over a database just brought to the schema, the door example:db:reset uses: it collects every registered seeder in descending priority and runs each. A resolver holding no seeder is refused, since a reset that seeds nothing would leave empty tables behind a successful exit; over tables that hold rows it changes nothing. */
func SeedAll(ctx context.Context, resolver melodycontainercontract.Resolver) error {
    seederList, collectErr := melodycontainer.AllImplementing[Seeder](resolver)
    if nil != collectErr {
        return collectErr
    }

    if 0 == len(seederList) {
        return exception.NewError("no nomenclature seeder is registered", nil, nil)
    }

    for _, seeder := range seederList {
        if seedErr := seeder.SeedIfEmpty(ctx); nil != seedErr {
            return seedErr
        }
    }

    return nil
}
