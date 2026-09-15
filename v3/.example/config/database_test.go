package config

import (
    "context"
    "database/sql"
    "errors"
    "strings"
    "testing"
    "time"

    melodybunorm "github.com/precision-soft/melody/integrations/bunorm/v3"
    melodypgsql "github.com/precision-soft/melody/integrations/bunorm/pgsql/v3"
    "github.com/precision-soft/melody/v3/.example/generated"
    "github.com/precision-soft/melody/v3/.example/persistence"
    "github.com/precision-soft/melody/v3/.example/repository"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
    melodylogging "github.com/precision-soft/melody/v3/logging"
    bun "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/pgdialect"
)

func TestDialIsInsecureOnlyOnTheExactSpelling(t *testing.T) {
    if false == dialIsInsecure("true") {
        t.Fatal("expected the exact spelling to arm the insecure dial")
    }

    for _, value := range []string{"", "false", "TRUE", "1", "yes", " true"} {
        if true == dialIsInsecure(value) {
            t.Fatalf("expected %q to keep the verified handshake", value)
        }
    }
}

func TestDatabaseOpenedBy_NamesTheManagerAndItsLocationWhenTheOpenRefuses(t *testing.T) {
    registry, registryErr := melodybunorm.NewManagerRegistryWithContext(
        context.Background(),
        melodylogging.NewNopLogger(),
        melodybunorm.ProviderDefinition{
            Name:      "declared",
            Provider:  melodypgsql.NewProvider(),
            Params:    melodybunorm.ConnectionParameters{Host: "127.0.0.1", Port: "1", Database: "never", User: "nobody"},
            IsDefault: true,
        },
    )
    if nil != registryErr {
        t.Fatalf("build the registry: %v", registryErr)
    }

    database, openErr := databaseOpenedBy(registry, "archive", "postgres:5432/melody_example_v3_archive")
    if nil == openErr || nil != database {
        t.Fatalf("expected a manager the registry does not declare to be refused, got db=%v err=%v", database, openErr)
    }

    if false == strings.Contains(openErr.Error(), "the archive database at postgres:5432/melody_example_v3_archive could not be opened") {
        t.Fatalf("expected the refusal to name the manager and its location, got %q", openErr.Error())
    }

    if "archive" != exception.LogContext(openErr)["manager"] {
        t.Fatalf("expected the context to carry the manager, got %v", exception.LogContext(openErr))
    }

    if nil == errors.Unwrap(openErr) {
        t.Fatal("expected the registry's own refusal to stay the cause")
    }
}

func TestArchiveReadingRepositoryResolvedByTypeNamesTheArchiveRatherThanTheWiring(t *testing.T) {
    serviceContainer := melodycontainer.NewContainer()

    melodycontainer.MustRegister(
        serviceContainer,
        persistence.ServiceArchiveStorage,
        func(resolver melodycontainercontract.Resolver) (*persistence.ArchiveStorage, error) {
            return persistence.NewArchiveStorageAt(bun.NewDB(sql.OpenDB(&refusingConnector{}), pgdialect.New()), "postgres:5432/melody_example_v3_archive"), nil
        },
    )

    generated.RegisterGeneratedServices(serviceContainer)

    _, resolveErr := melodycontainer.FromResolverByType[repository.CatalogReadingRepository](serviceContainer)
    if nil == resolveErr {
        t.Fatal("expected the refusing archive to fail the resolution")
    }

    if true == strings.Contains(resolveErr.Error(), "service not registered") {
        t.Fatalf("expected the refusal to name the archive, not the wiring, got %q", resolveErr.Error())
    }

    if false == strings.Contains(resolveErr.Error(), "on the archive set") {
        t.Fatalf("expected the refusal to name the archive set, got %q", resolveErr.Error())
    }
}

func TestBuildDatabase_BindsTheArchiveOpenToTheProcessContext(t *testing.T) {
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    moduleInstance := moduleWithEnvironment(t, map[string]string{
        environmentKeyPgsqlHost:     "127.0.0.1",
        environmentKeyPgsqlPort:     "1",
        environmentKeyPgsqlDatabase: "never",
        environmentKeyPgsqlUser:     "nobody",
        environmentKeyPgsqlPassword: "nothing",
    })
    moduleInstance.processContext = ctx
    moduleInstance.buildDatabase()

    if nil == moduleInstance.databaseRegistry || false == moduleInstance.archiveWired {
        t.Fatal("expected the archive to be declared on the registry")
    }

    openDone := make(chan error, 1)
    go func() {
        _, openErr := moduleInstance.databaseRegistry.Database(databaseArchiveManagerName)
        openDone <- openErr
    }()

    time.Sleep(50 * time.Millisecond)
    cancel()

    select {
    case openErr := <-openDone:
        if nil == openErr || false == strings.Contains(openErr.Error(), "cancelled by the caller's context") {
            t.Fatalf("expected the open to end with the cancellation, got %v", openErr)
        }
    case <-time.After(2 * time.Second):
        t.Fatal("expected the cancelled open to end at once; it is still retrying")
    }

    closeContext, cancelClose := context.WithTimeout(context.Background(), 2*time.Second)
    defer cancelClose()

    if closeErr := moduleInstance.databaseRegistry.CloseWithContext(closeContext); nil != closeErr {
        t.Fatalf("expected the registry to close with nothing in flight, got %v", closeErr)
    }
}

func TestBuildDatabase_GivesTheArchiveARequestSizedOpenBudget(t *testing.T) {
    moduleInstance := moduleWithEnvironment(t, map[string]string{
        environmentKeyPgsqlHost:     "127.0.0.1",
        environmentKeyPgsqlPort:     "1",
        environmentKeyPgsqlDatabase: "never",
        environmentKeyPgsqlUser:     "nobody",
        environmentKeyPgsqlPassword: "nothing",
    })
    moduleInstance.buildDatabase()

    startedAt := time.Now()
    _, openErr := moduleInstance.databaseRegistry.Database(databaseArchiveManagerName)
    elapsed := time.Since(startedAt)

    if nil == openErr || false == strings.Contains(openErr.Error(), "database connection failed") {
        t.Fatalf("expected the open to give up after its attempts, got %v", openErr)
    }

    if 3*time.Second < elapsed {
        t.Fatalf("expected the archive's budget to be spent in under three seconds, took %s", elapsed)
    }
}
