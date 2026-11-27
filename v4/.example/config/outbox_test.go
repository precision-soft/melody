package config

import (
    "context"
    "database/sql"
    "database/sql/driver"
    "errors"
    "slices"
    "sync/atomic"
    "testing"

    outbox "github.com/precision-soft/melody/integrations/outbox/v4"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    bun "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/mysqldialect"
)

type countingConnector struct {
    connects atomic.Int32
}

func (instance *countingConnector) Connect(ctx context.Context) (driver.Conn, error) {
    instance.connects.Add(1)

    return nil, errors.New("this handle is never dialed")
}

func (instance *countingConnector) Driver() driver.Driver {
    return nil
}

/* endedRuntimeResolver answers the container's services under a runtime whose context has ended */
type endedRuntimeResolver struct {
    melodycontainercontract.Resolver
    runtimeInstance melodycontainercontract.Runtime
}

func (instance endedRuntimeResolver) Runtime() melodycontainercontract.Runtime {
    return instance.runtimeInstance
}

func TestOutboxStoreFactory_EnsuresTheSchemaUnderTheResolvingRuntime(t *testing.T) {
    connector := &countingConnector{}
    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    t.Cleanup(func() { _ = containerInstance.CloseWithContext(context.Background()) })
    containerInstance.MustRegister(serviceDatabase, func(resolver melodycontainercontract.Resolver) (*bun.DB, error) {
        return bun.NewDB(sql.OpenDB(connector), mysqldialect.New()), nil
    }, melodycontainer.RegisterOptions{Closer: registryOwnedDatabaseCloser})

    moduleInstance := moduleWithEnvironment(t, map[string]string{})

    if _, resolveErr := melodycontainer.FromResolver[*bun.DB](t.Context(), containerInstance, serviceDatabase); nil != resolveErr {
        t.Fatalf("resolve the database: %v", resolveErr)
    }
    dialledAtConstruction := connector.connects.Load()

    endedContext, cancel := context.WithCancel(context.Background())
    cancel()

    _, factoryErr := moduleInstance.outboxStoreFactory(endedRuntimeResolver{
        Resolver:        containerInstance,
        runtimeInstance: melodyruntime.NewRun(endedContext, containerInstance.NewScope()),
    })
    if false == errors.Is(factoryErr, context.Canceled) {
        t.Fatalf("expected the ended resolving runtime answered, got %v", factoryErr)
    }

    if dialled := connector.connects.Load() - dialledAtConstruction; 0 != dialled {
        t.Fatalf("expected no ddl sent once the resolving runtime ended, dialled %d times", dialled)
    }
}

/* the module's store is built at boot with the warning mode, and its provider ensures the schema under the boot runtime */
func TestOutboxModuleConfig_TheStoreIsBuiltAtBootAndEnsuresTheSchemaThere(t *testing.T) {
    connector := &countingConnector{}
    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    t.Cleanup(func() { _ = containerInstance.CloseWithContext(context.Background()) })
    containerInstance.MustRegister(serviceDatabase, func(resolver melodycontainercontract.Resolver) (*bun.DB, error) {
        return bun.NewDB(sql.OpenDB(connector), mysqldialect.New()), nil
    }, melodycontainer.RegisterOptions{Closer: registryOwnedDatabaseCloser})

    moduleInstance := moduleWithEnvironment(t, map[string]string{})
    outbox.NewModule(moduleInstance.outboxModuleConfig()).RegisterServices(containerRegistrar{Container: containerInstance})

    names, modes := builtAtBootNames(containerInstance)
    if false == slices.Contains(names, outbox.ServiceStore) || melodycontainer.BootFailureWarns != modes[outbox.ServiceStore] {
        t.Fatalf("expected the outbox store built at boot with the warning mode, got %v %v", names, modes)
    }

    if true == slices.Contains(names, outbox.ServiceRelay) {
        t.Fatalf("expected the relay built at its first use, got %v", names)
    }

    bootRuntime, finish := melodyruntime.NewBoot(t.Context(), containerInstance)
    defer func() {
        _ = finish()
    }()

    if _, buildErr := bootRuntime.Scope().Get(bootRuntime.Context(), outbox.ServiceStore); nil == buildErr {
        t.Fatal("expected the schema's failure answered at the boot build")
    }

    if 0 == connector.connects.Load() {
        t.Fatal("expected the schema ensured at the boot build")
    }
}
