package repository

import (
    "context"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v4/.example/persistence"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
)

func TestSeedAll_RefusesARuntimeHoldingNoSeeder(t *testing.T) {
    seedErr := SeedAll(melodyruntime.NewRun(t.Context(), melodycontainer.NewContainer(melodycontainer.ContainerConfig{}).NewScope()))

    if nil == seedErr || false == strings.Contains(seedErr.Error(), "no nomenclature seeder is registered") {
        t.Fatalf("expected a runtime without seeders refused, got %v", seedErr)
    }
}

func TestSeedAll_RefusesAnAbsentRuntimeAsAnError(t *testing.T) {
    if seedErr := SeedAll(nil); nil == seedErr {
        t.Fatalf("expected an absent runtime refused")
    }
}

func TestSeedAll_SeedsTheNomenclatureWithoutTheAccountSeed(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = countingRows(0)

    seedAllOver(t, persistence.NewCatalogStorage(database))

    for _, table := range []string{"melody_example_v4_category", "melody_example_v4_currency", "melody_example_v4_product"} {
        if 0 == countSeedInserts(recorder, table) {
            t.Fatalf("expected the empty %s seeded, got %q", table, recorder.recordedQueries())
        }
    }

    if 0 != countSeedInserts(recorder, "melody_example_v4_user") {
        t.Fatalf("expected no account seeded without the account seed, got %q", recorder.recordedQueries())
    }
}

func TestSeedAll_LeavesAnEmptyCatalogueEmptyWhenTheSeedIsOff(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = countingRows(0)

    seedAllOver(t, persistence.NewCatalogStorage(database).WithoutNomenclatureSeed())

    if queries := recorder.recordedQueries(); 0 != len(queries) {
        t.Fatalf("expected an unseeded catalogue left untouched, got %q", queries)
    }
}

func TestInMemoryRepositories_StartEmptyWhenTheSeedIsOff(t *testing.T) {
    for _, seedsNomenclature := range []bool{true, false} {
        storage := persistence.NewCatalogStorage(nil)
        if false == seedsNomenclature {
            storage = storage.WithoutNomenclatureSeed()
        }

        categoryRepository, categoryErr := NewCategoryRepository(storage, &persistence.CatalogSchema{})
        currencyRepository, currencyErr := NewCurrencyRepository(storage, &persistence.CatalogSchema{})
        productRepository, productErr := NewProductRepository(storage, &persistence.CatalogSchema{})
        if nil != categoryErr || nil != currencyErr || nil != productErr {
            t.Fatalf("build the repositories: %v, %v, %v", categoryErr, currencyErr, productErr)
        }

        categories, _ := categoryRepository.All(context.Background())
        currencies, _ := currencyRepository.All(context.Background())
        products, _ := productRepository.All(context.Background())

        for name, count := range map[string]int{"categories": len(categories), "currencies": len(currencies), "products": len(products)} {
            if seedsNomenclature != (0 < count) {
                t.Fatalf("seed %v: expected the %s seeded %v, got %d", seedsNomenclature, name, seedsNomenclature, count)
            }
        }
    }
}

func seedAllOver(t *testing.T, storage *persistence.CatalogStorage) {
    t.Helper()

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    t.Cleanup(func() { _ = containerInstance.CloseWithContext(context.Background()) })

    containerInstance.MustRegister(persistence.ServiceCatalogStorage, func(resolver melodycontainercontract.Resolver) (*persistence.CatalogStorage, error) {
        return storage, nil
    }, melodycontainer.RegisterOptions{})
    RegisterSeeders(containerInstance)

    if seedErr := SeedAll(melodyruntime.NewRun(t.Context(), containerInstance.NewScope())); nil != seedErr {
        t.Fatalf("seed: %v", seedErr)
    }
}

func countSeedInserts(recorder *queryRecorder, table string) int {
    count := 0
    for _, query := range recorder.recordedQueries() {
        if true == strings.HasPrefix(query, "INSERT INTO `"+table+"`") {
            count = count + 1
        }
    }

    return count
}
