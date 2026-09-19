package bunorm

import (
    "errors"
    "fmt"
    "strings"
    "testing"

    "github.com/uptrace/bun"

    "github.com/precision-soft/melody/v3/exception"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

/* unreachableSplitProvider fails the way a provider fails on an outage it could not get past: filed under ErrDatabaseUnreachable, the one class the splitter absorbs */
type unreachableSplitProvider struct{}

func (instance *unreachableSplitProvider) Open(params ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return nil, exception.NewError("database connection failed", nil, DatabaseUnreachable(errors.New("replica is down")))
}

/* refusingSplitProvider fails the way a provider refuses by name — a parameter left empty, a password the server refused — or the way a provider that files nothing under the class fails: a plain error, terminal by nature */
type refusingSplitProvider struct{}

func (instance *refusingSplitProvider) Open(params ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return nil, errors.New("pgsql database open refused: the user is empty")
}

func TestReadWriteSplitter_WriterIsPrimaryReaderRoundRobins(t *testing.T) {
    splitter := &ReadWriteSplitter{
        primaryName:  "primary",
        replicaNames: []string{"replica-a", "replica-b"},
    }

    if "primary" != splitter.WriterName() {
        t.Fatalf("unexpected writer name: %s", splitter.WriterName())
    }

    expected := []string{"replica-a", "replica-b", "replica-a", "replica-b"}
    for index, want := range expected {
        got := splitter.ReaderName()
        if want != got {
            t.Fatalf("round-robin position %d: expected %s, got %s", index, want, got)
        }
    }
}

func TestReadWriteSplitter_ReaderFallsBackToPrimaryWithoutReplicas(t *testing.T) {
    splitter := &ReadWriteSplitter{
        primaryName: "primary",
    }

    if "primary" != splitter.ReaderName() {
        t.Fatalf("expected reader to fall back to primary, got %s", splitter.ReaderName())
    }
}

func TestReadWriteSplitter_ReaderRefusesAnUnknownReplicaName(t *testing.T) {
    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        ProviderDefinition{Name: "primary", Provider: &fakeProvider{}, IsDefault: true},
    )
    if nil != registryErr {
        t.Fatalf("registry: %v", registryErr)
    }

    splitter := NewReadWriteSplitter(registry, "primary", "misspelled-replica")

    database, readerErr := splitter.Reader()
    if nil == readerErr {
        t.Fatalf("expected the unknown replica name to be refused, got a database: %v", database)
    }

    if false == errors.Is(readerErr, ErrProviderDefinitionNotFound) {
        t.Fatalf("expected ErrProviderDefinitionNotFound, got: %v", readerErr)
    }
}

func TestReadWriteSplitter_ReaderFallsBackToPrimaryWhenTheReplicaIsUnreachable(t *testing.T) {
    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        ProviderDefinition{Name: "primary", Provider: &fakeProvider{}, IsDefault: true},
        ProviderDefinition{Name: "replica", Provider: &unreachableSplitProvider{}},
    )
    if nil != registryErr {
        t.Fatalf("registry: %v", registryErr)
    }

    splitter := NewReadWriteSplitter(registry, "primary", "replica")

    database, readerErr := splitter.Reader()
    if nil != readerErr {
        t.Fatalf("expected the open failure to fall back to the primary, got: %v", readerErr)
    }

    primary, primaryErr := registry.Database("primary")
    if nil != primaryErr {
        t.Fatalf("primary: %v", primaryErr)
    }

    if primary != database {
        t.Fatalf("expected the reader to answer the primary database on a replica open failure")
    }
}

func TestReadWriteSplitter_ReaderNamesBothFailuresWhenThePrimaryFailsToo(t *testing.T) {
    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        ProviderDefinition{Name: "primary", Provider: &unreachableSplitProvider{}, IsDefault: true},
        ProviderDefinition{Name: "replica", Provider: &unreachableSplitProvider{}},
    )
    if nil != registryErr {
        t.Fatalf("registry: %v", registryErr)
    }

    splitter := NewReadWriteSplitter(registry, "primary", "replica")

    _, readerErr := splitter.Reader()
    if nil == readerErr {
        t.Fatalf("expected an error when the replica and the primary both fail to open")
    }

    if false == strings.Contains(readerErr.Error(), "could not open the replica nor the primary") {
        t.Fatalf("expected the answer to name both failures, got: %v", readerErr)
    }
}

func TestNewReadWriteSplitter_RefusesAnEmptyReplicaName(t *testing.T) {
    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        ProviderDefinition{Name: "primary", Provider: &fakeProvider{}, IsDefault: true},
    )
    if nil != registryErr {
        t.Fatalf("registry: %v", registryErr)
    }

    defer func() {
        recovered := recover()
        if nil == recovered {
            t.Fatalf("expected the empty replica name to be refused at construction")
        }

        if false == strings.Contains(fmt.Sprintf("%v", recovered), "replica name is empty") {
            t.Fatalf("expected the panic to name the empty replica, got %v", recovered)
        }
    }()

    NewReadWriteSplitter(registry, "primary", "replica-a", "")
}

type noDatabaseSplitProvider struct{}

func (instance *noDatabaseSplitProvider) Open(params ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return nil, nil
}

/* a provider answering neither a database nor an error is the wiring mistake the registry refuses by name; folded into the fallback it routed every read of that replica to the primary forever, with no signal that the replica was dead */
func TestReadWriteSplitter_ReaderRefusesAReplicaWhoseProviderAnsweredNoDatabase(t *testing.T) {
    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        ProviderDefinition{Name: "primary", Provider: &fakeProvider{}, IsDefault: true},
        ProviderDefinition{Name: "replica", Provider: &noDatabaseSplitProvider{}},
    )
    if nil != registryErr {
        t.Fatalf("registry: %v", registryErr)
    }
    t.Cleanup(func() { _ = registry.Close() })

    splitter := NewReadWriteSplitter(registry, "primary", "replica")

    database, readerErr := splitter.Reader()
    if false == errors.Is(readerErr, ErrProviderReturnedNilDatabase) {
        t.Fatalf("expected ErrProviderReturnedNilDatabase, got database=%v err=%v", database, readerErr)
    }

    if nil != database {
        t.Fatal("expected no database beside the refusal")
    }
}

/* a replica whose provider REFUSED — a parameter left empty, a password the server refused, a database that does not exist, or a provider that files nothing under the unreachable class — is refused with the provider's own answer, not served from the primary: the denylist of registry sentinels this replaced let every refusal it did not name fall to the primary, silently and for the life of the process */
func TestReadWriteSplitter_ReaderRefusesAReplicaWhoseProviderRefused(t *testing.T) {
    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        ProviderDefinition{Name: "primary", Provider: &fakeProvider{}, IsDefault: true},
        ProviderDefinition{Name: "replica", Provider: &refusingSplitProvider{}},
    )
    if nil != registryErr {
        t.Fatalf("registry: %v", registryErr)
    }
    t.Cleanup(func() { _ = registry.Close() })

    splitter := NewReadWriteSplitter(registry, "primary", "replica")

    database, readerErr := splitter.Reader()
    if nil != database {
        t.Fatal("expected no database beside the refusal")
    }

    if nil == readerErr || false == strings.Contains(readerErr.Error(), "the user is empty") {
        t.Fatalf("expected the provider's own refusal handed back, got %v", readerErr)
    }

    if primary, primaryErr := registry.Database("primary"); nil != primaryErr || nil == primary {
        t.Fatalf("the primary itself stays reachable: %v", primaryErr)
    }
}
