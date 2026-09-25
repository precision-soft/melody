package config

import (
    "bytes"
    "context"
    "database/sql"
    "database/sql/driver"
    "errors"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/.example/persistence"
    "github.com/precision-soft/melody/v3/.example/subscriber"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodylogging "github.com/precision-soft/melody/v3/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    bun "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/mysqldialect"
)

/* refusingConnector stands in for a driver that is never dialed. database/sql opens lazily, and nothing in
   these tests issues a query, so a connector that refuses every connection is the cheapest handle that is a
   real *bun.DB: it proves the tests measure the wiring rather than a server. */
type refusingConnector struct{}

func (instance *refusingConnector) Connect(ctx context.Context) (driver.Conn, error) {
    return nil, errors.New("this handle is never dialed")
}

func (instance *refusingConnector) Driver() driver.Driver {
    return nil
}

func newUndialedDatabase() *bun.DB {
    return bun.NewDB(sql.OpenDB(&refusingConnector{}), mysqldialect.New())
}

/* containerRegistrar is the container under the name-based registrar the composition root is handed. The
   application supplies this method by delegating to MustRegister; the container itself carries only the
   Registrar half, so a test that drives a registration door needs the same one-line adapter. */
type containerRegistrar struct {
    melodycontainercontract.Container
}

func (instance containerRegistrar) RegisterService(
    serviceName string,
    provider any,
    options ...melodycontainercontract.RegisterOption,
) {
    instance.MustRegister(serviceName, provider, options...)
}

/* The catalog storage is the one door an ordinary http process passes through on its way to a repository: the generated wiring resolves it by type for every one of them. A provider that captures the handle the composition root already built resolves nothing, so the container records no dependency, the registry's SetLogger never runs and at SIGTERM neither the pool nor the registry is closed. Resolving is what writes the edge. */
func TestRegisterCatalogStorageService_ResolvesTheHandleRatherThanCapturingIt(t *testing.T) {
    containerInstance := melodycontainer.NewContainer()

    resolutions := 0
    containerInstance.MustRegister(
        serviceDatabase,
        func(resolver melodycontainercontract.Resolver) (*bun.DB, error) {
            resolutions++

            return newUndialedDatabase(), nil
        },
    )

    moduleInstance := &Module{database: newUndialedDatabase()}
    moduleInstance.registerCatalogStorageService(containerRegistrar{Container: containerInstance})

    storage, storageErr := melodycontainer.FromResolver[*persistence.CatalogStorage](containerInstance, persistence.ServiceCatalogStorage)
    if nil != storageErr {
        t.Fatalf("resolve catalog storage: %v", storageErr)
    }

    if 1 != resolutions {
        t.Fatalf("expected the storage provider to resolve the handle service exactly once, it resolved it %d times", resolutions)
    }

    if false == storage.IsPersistent() {
        t.Fatal("expected the resolved storage to carry a handle")
    }
}

/* The sister case the gate exists for: without a configured database the handle service is never registered,
   so a provider that resolved unconditionally would fail here and take the whole nomenclature with it. The
   storage is still published, carrying nothing. */
func TestRegisterCatalogStorageService_PublishesAHandlelessStorageWithoutADatabase(t *testing.T) {
    containerInstance := melodycontainer.NewContainer()

    moduleInstance := &Module{}
    moduleInstance.registerCatalogStorageService(containerRegistrar{Container: containerInstance})

    storage, storageErr := melodycontainer.FromResolver[*persistence.CatalogStorage](containerInstance, persistence.ServiceCatalogStorage)
    if nil != storageErr {
        t.Fatalf("resolve catalog storage: %v", storageErr)
    }

    if true == storage.IsPersistent() {
        t.Fatal("expected a storage without a handle when no database is configured")
    }
}

/* refusingBackplane refuses every publish, the failure the hub files */
type refusingBackplane struct{}

func (instance refusingBackplane) Publish(topic string, event melodyhttp.ServerSentEvent) error {
    return errors.New("the backplane is down")
}

func (instance refusingBackplane) Close() error {
    return nil
}

/* the hub files its own failures, and its provider is where it is handed the application's journal; without the swap a redis outage would silence cross-node delivery into a counter nobody reads. A publish the backplane refuses after the hub is resolved reaches the logger the container publishes. */
func TestRegisterServerSentEventHubService_HandsTheHubTheContainersJournal(t *testing.T) {
    /* read after Shutdown, which waits for the publishes in flight: the record is written before it returns */
    journal := &bytes.Buffer{}

    containerInstance := melodycontainer.NewContainer()
    containerInstance.MustRegister(
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return melodylogging.NewJsonLogger(journal, melodyloggingcontract.LevelDebug), nil
        },
    )

    moduleInstance := &Module{}
    moduleInstance.buildServerSentEvent()
    moduleInstance.registerServerSentEventHubService(containerRegistrar{Container: containerInstance})

    hub, hubErr := melodycontainer.FromResolver[*melodyhttp.ServerSentEventHub](containerInstance, subscriber.ServiceCatalogNotificationHub)
    if nil != hubErr || moduleInstance.serverSentEventHub != hub {
        t.Fatalf("expected the module's hub published, got %v, %v", hub, hubErr)
    }

    hub.SetBackplane(refusingBackplane{})
    hub.Broadcast("catalog", melodyhttp.ServerSentEvent{Event: "notification", Data: "changed"})
    hub.Shutdown()

    if false == strings.Contains(journal.String(), "server sent event backplane publish failed") {
        t.Fatalf("expected the refused publish in the container's journal, got %q", journal.String())
    }
}

/* without an archive the archive's storage is still published, carrying nothing — the generated wiring fills
   the archive repository by type — and it does not reach for the archive's handle, which is not registered */
func TestRegisterArchiveStorageService_PublishesAHandlelessStorageWithoutAnArchive(t *testing.T) {
    containerInstance := melodycontainer.NewContainer()

    moduleInstance := &Module{}
    moduleInstance.registerArchiveStorageService(containerRegistrar{Container: containerInstance})

    storage, storageErr := melodycontainer.FromResolver[*persistence.ArchiveStorage](containerInstance, persistence.ServiceArchiveStorage)
    if nil != storageErr {
        t.Fatalf("resolve the archive storage: %v", storageErr)
    }

    if true == storage.IsPersistent() {
        t.Fatal("expected an archive storage without a handle when no archive is wired")
    }
}
