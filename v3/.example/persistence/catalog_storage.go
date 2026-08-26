package persistence

import (
    "context"

    melodyaudit "github.com/precision-soft/melody/integrations/bunorm/v3/audit"
    bun "github.com/uptrace/bun"
)

const (
    ServiceCatalogStorage = "service.example.catalog.storage"

    /* the audit trail of the nomenclature. It is per major, like every other table the example owns, so three applications sharing one database do not write into each other's history. */
    auditTable = "melody_example_v3_audit"

    /* the entities whose field-level history is kept, named once so the repositories that write them and anyone reading the trail agree. */
    AuditEntityProduct = "product"
    AuditEntityUser    = "user"
)

/* actorContextKey carries who is making a change from the service layer down to the repository that writes it.

   The repositories take a context rather than a runtime — they know nothing about requests — and the audit trail has to name a person. A key of this package's own is what lets the service put the answer on the context without the service layer having to import an ORM integration to do it. */
type actorContextKey struct{}

func WithActor(ctx context.Context, actor string) context.Context {
    return context.WithValue(ctx, actorContextKey{}, actor)
}

func ActorFromContext(ctx context.Context) string {
    actor, ok := ctx.Value(actorContextKey{}).(string)
    if false == ok {
        return ""
    }

    return actor
}

/* CatalogStorage is where the nomenclature is kept, or the absence of anywhere to keep it.

   The repositories are wired by melody:wiring:generate, which fills a constructor's arguments by resolving them from the container by type. A constructor asking for a *bun.DB directly could therefore only be generated for an application that has one, and the example is meant to boot without a database as well. This handle is always registered and answers whether there is a connection behind it, so one generated provider serves both environments and the choice stays in the repository package rather than in the configuration.

   It also owns the one audit tracker the audited repositories share. There is one because the trail is one: a registry per repository would mean each deciding on its own which fields are too sensitive to record, and the answer belongs to the application rather than to whichever repository asked last. */
type CatalogStorage struct {
    database      *bun.DB
    auditRegistry *melodyaudit.Registry
    tracker       *melodyaudit.Tracker
}

func NewCatalogStorage(database *bun.DB) *CatalogStorage {
    if nil == database {
        return &CatalogStorage{}
    }

    /* updated_at moves on every write of a product and says nothing a trail entry does not already carry through its own timestamp, so it is not recorded as a change */
    registry := melodyaudit.NewRegistry(auditTable, "updated_at").
        Register(AuditEntityProduct, melodyaudit.EntityOptions{}).
        /* a deleted account has to stay answerable for — which roles it held when it was removed is the question a directory is asked after the fact, and the identifier alone cannot answer it */
        Register(AuditEntityUser, melodyaudit.EntityOptions{CaptureDeleteBeforeImage: true})

    recorder := melodyaudit.NewRecorderWithStorage(melodyaudit.NewBunStorage(database), registry)

    return &CatalogStorage{
        database:      database,
        auditRegistry: registry,
        tracker:       melodyaudit.NewTracker(database, recorder),
    }
}

/* Database is nil when the environment configured no connection. */
func (instance *CatalogStorage) Database() *bun.DB {
    return instance.database
}

func (instance *CatalogStorage) IsPersistent() bool {
    return nil != instance.database
}

/* Tracker is the handle the audited repositories write through. It is nil when there is no database, which is the same condition under which those repositories are not built at all. */
func (instance *CatalogStorage) Tracker() *melodyaudit.Tracker {
    return instance.tracker
}

/* EnsureAuditSchema creates the trail's tables when they are absent. The example carries no migration runner, so the storage owns the tables it writes, exactly as each repository owns its own. */
func (instance *CatalogStorage) EnsureAuditSchema(ctx context.Context) error {
    if nil == instance.auditRegistry {
        return nil
    }

    return instance.auditRegistry.EnsureSchema(ctx, instance.database)
}
