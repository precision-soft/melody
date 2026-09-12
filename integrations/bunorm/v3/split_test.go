package bunorm

import (
    "errors"
    "fmt"
    "strings"
    "testing"

    "github.com/uptrace/bun"

    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

type failingSplitProvider struct{}

func (instance *failingSplitProvider) Open(params ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return nil, errors.New("replica is down")
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

func TestReadWriteSplitter_ReaderFallsBackToPrimaryWhenTheReplicaFailsToOpen(t *testing.T) {
    registry, registryErr := NewManagerRegistry(
        &fakeLogger{},
        ProviderDefinition{Name: "primary", Provider: &fakeProvider{}, IsDefault: true},
        ProviderDefinition{Name: "replica", Provider: &failingSplitProvider{}},
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
        ProviderDefinition{Name: "primary", Provider: &failingSplitProvider{}, IsDefault: true},
        ProviderDefinition{Name: "replica", Provider: &failingSplitProvider{}},
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
