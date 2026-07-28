package persistence

import (
    bun "github.com/uptrace/bun"
)

const (
    ServiceCatalogStorage = "service.example.catalog.storage"
)

/* CatalogStorage is where the nomenclature is kept, or the absence of anywhere to keep it.

The repositories are wired by melody:wiring:generate, which fills a constructor's arguments by resolving them from the container by type. A constructor asking for a *bun.DB directly could therefore only be generated for an application that has one, and the example is meant to boot without a database as well. This handle is always registered and answers whether there is a connection behind it, so one generated provider serves both environments and the choice stays in the repository package rather than in the configuration. */
type CatalogStorage struct {
    database *bun.DB
}

func NewCatalogStorage(database *bun.DB) *CatalogStorage {
    return &CatalogStorage{database: database}
}

/* Database is nil when the environment configured no connection. */
func (instance *CatalogStorage) Database() *bun.DB {
    return instance.database
}

func (instance *CatalogStorage) IsPersistent() bool {
    return nil != instance.database
}
