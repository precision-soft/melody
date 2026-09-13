package config

import (
    "context"
    "database/sql"
    "database/sql/driver"
    "errors"
    "testing"

    "github.com/precision-soft/melody/v3/.example/persistence"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyhttp "github.com/precision-soft/melody/v3/http"
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

/* The catalog storage is the one door an ordinary http process passes through on its way to a repository:
   the generated wiring resolves it by type for every one of them. A provider that CAPTURES the handle the
   composition root already built resolves nothing, so the container records no dependency — and measured on
   the running stack that is exactly what happened: over a boot, a login and three authenticated api reads,
   the registry service and the handle service were resolved ZERO times, SetLogger was never called, and at
   SIGTERM the container closed neither the pool nor the registry. Resolving is what writes the edge. */
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

/* countingBackplane closes the way the shipped ones do — clearing itself from the hub as the first step —
   and counts the calls, which is the whole question the composition root's second Close raised. */
type countingBackplane struct {
    hub    *melodyhttp.ServerSentEventHub
    closes int
}

func (instance *countingBackplane) Publish(topic string, event melodyhttp.ServerSentEvent) error {
    return nil
}

func (instance *countingBackplane) Close() error {
    instance.closes++
    instance.hub.SetBackplane(nil)

    return nil
}

/* The hub owns the backplane and closes it: this is the rationale the composition root now carries instead of
   a Close of its own. Measured on the running stack before the repair, the example's hook and the hub's own
   Shutdown ran on separate goroutines and both reached the backplane — the losing path closed one that was
   already closed, and which of the two drained the publishes in flight was decided by the race. */
func TestServerSentEventHubShutdown_ClosesTheBackplaneItOwnsExactlyOnce(t *testing.T) {
    hub := melodyhttp.NewServerSentEventHub()
    backplane := &countingBackplane{hub: hub}
    hub.SetBackplane(backplane)

    hub.Shutdown()

    if 1 != backplane.closes {
        t.Fatalf("expected the hub to close its backplane exactly once, got %d", backplane.closes)
    }
}

/* The sister case, which is what the second closer in the composition root actually bought. The count is not
   even stable: it depends on which of the two http shutdown hooks won, and both orders were observed on the
   running stack. Hook first, the hub finds the backplane already cleared and closes nothing — one call, and
   the drain happens inside the backplane's own SetBackplane(nil). Hub first, the order this test drives, the
   hub takes the reference, drains, closes it, and the hook then closes a backplane that is already closed.
   Two closers, one duty, and the race deciding where the publishes in flight were waited for. */
func TestServerSentEventHubShutdown_ASecondCloserBesideItClosesAnAlreadyClosedBackplane(t *testing.T) {
    hub := melodyhttp.NewServerSentEventHub()
    backplane := &countingBackplane{hub: hub}
    hub.SetBackplane(backplane)

    hub.Shutdown()

    /* the shape of the hook this example used to register beside hub.Shutdown */
    _ = backplane.Close()

    if 2 != backplane.closes {
        t.Fatalf("expected the second closer to close an already-closed backplane, got %d calls", backplane.closes)
    }
}
