package bunorm

import (
    "context"
    "errors"
    "fmt"
    "strings"
    "sync/atomic"
    "testing"
    "time"

    "github.com/uptrace/bun"

    "github.com/precision-soft/melody/v4/bound"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

type unreachableSplitProvider struct{}

func (instance *unreachableSplitProvider) Open(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return nil, exception.NewError("database connection failed", nil, DatabaseUnreachable(errors.New("replica is down")))
}

func (instance *unreachableSplitProvider) OpenForMigration(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.Open(runtimeInstance, parameters, logger)
}

type refusingSplitProvider struct{}

func (instance *refusingSplitProvider) Open(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return nil, errors.New("pgsql database open refused: the user is empty")
}

func (instance *refusingSplitProvider) OpenForMigration(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.Open(runtimeInstance, parameters, logger)
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
        []ProviderDefinition{
            ProviderDefinition{Name: "primary", Provider: &fakeProvider{}, Parameters: testConnectionParameters, IsDefault: true},
        },
        ManagerRegistryConfig{Logger: &fakeLogger{}},
    )
    if nil != registryErr {
        t.Fatalf("registry: %v", registryErr)
    }

    splitter := NewReadWriteSplitter(registry, "primary", SplitterConfig{ReplicaNames: []string{"misspelled-replica"}})

    database, readerErr := splitter.Reader(testRuntime())
    if nil == readerErr {
        t.Fatalf("expected the unknown replica name to be refused, got a database: %v", database)
    }

    if false == errors.Is(readerErr, ErrProviderDefinitionNotFound) {
        t.Fatalf("expected ErrProviderDefinitionNotFound, got: %v", readerErr)
    }
}

func TestReadWriteSplitter_ReaderFallsBackToPrimaryWhenTheReplicaIsUnreachable(t *testing.T) {
    registry, registryErr := NewManagerRegistry(
        []ProviderDefinition{
            ProviderDefinition{Name: "primary", Provider: &fakeProvider{}, Parameters: testConnectionParameters, IsDefault: true},
            ProviderDefinition{Name: "replica", Provider: &unreachableSplitProvider{}, Parameters: testConnectionParameters},
        },
        ManagerRegistryConfig{Logger: &fakeLogger{}},
    )
    if nil != registryErr {
        t.Fatalf("registry: %v", registryErr)
    }

    splitter := NewReadWriteSplitter(registry, "primary", SplitterConfig{ReplicaNames: []string{"replica"}})

    database, readerErr := splitter.Reader(testRuntime())
    if nil != readerErr {
        t.Fatalf("expected the open failure to fall back to the primary, got: %v", readerErr)
    }

    primary, primaryErr := registry.Database(testRuntime(), "primary")
    if nil != primaryErr {
        t.Fatalf("primary: %v", primaryErr)
    }

    if primary != database {
        t.Fatalf("expected the reader to answer the primary database on a replica open failure")
    }
}

func TestReadWriteSplitter_ReaderNamesBothFailuresWhenThePrimaryFailsToo(t *testing.T) {
    registry, registryErr := NewManagerRegistry(
        []ProviderDefinition{
            ProviderDefinition{Name: "primary", Provider: &unreachableSplitProvider{}, Parameters: testConnectionParameters, IsDefault: true},
            ProviderDefinition{Name: "replica", Provider: &unreachableSplitProvider{}, Parameters: testConnectionParameters},
        },
        ManagerRegistryConfig{Logger: &fakeLogger{}},
    )
    if nil != registryErr {
        t.Fatalf("registry: %v", registryErr)
    }

    splitter := NewReadWriteSplitter(registry, "primary", SplitterConfig{ReplicaNames: []string{"replica"}})

    _, readerErr := splitter.Reader(testRuntime())
    if nil == readerErr {
        t.Fatalf("expected an error when the replica and the primary both fail to open")
    }

    if false == strings.Contains(readerErr.Error(), "could not open the replica nor the primary") {
        t.Fatalf("expected the answer to name both failures, got: %v", readerErr)
    }
}

func TestNewReadWriteSplitter_RefusesAnEmptyReplicaName(t *testing.T) {
    registry, registryErr := NewManagerRegistry(
        []ProviderDefinition{
            ProviderDefinition{Name: "primary", Provider: &fakeProvider{}, Parameters: testConnectionParameters, IsDefault: true},
        },
        ManagerRegistryConfig{Logger: &fakeLogger{}},
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

    NewReadWriteSplitter(registry, "primary", SplitterConfig{ReplicaNames: []string{"replica-a", ""}})
}

type noDatabaseSplitProvider struct{}

func (instance *noDatabaseSplitProvider) Open(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return nil, nil
}

func (instance *noDatabaseSplitProvider) OpenForMigration(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.Open(runtimeInstance, parameters, logger)
}

func TestReadWriteSplitter_ReaderRefusesAReplicaWhoseProviderAnsweredNoDatabase(t *testing.T) {
    registry, registryErr := NewManagerRegistry(
        []ProviderDefinition{
            ProviderDefinition{Name: "primary", Provider: &fakeProvider{}, Parameters: testConnectionParameters, IsDefault: true},
            ProviderDefinition{Name: "replica", Provider: &noDatabaseSplitProvider{}, Parameters: testConnectionParameters},
        },
        ManagerRegistryConfig{Logger: &fakeLogger{}},
    )
    if nil != registryErr {
        t.Fatalf("registry: %v", registryErr)
    }
    t.Cleanup(func() { _ = registry.CloseWithContext(context.Background()) })

    splitter := NewReadWriteSplitter(registry, "primary", SplitterConfig{ReplicaNames: []string{"replica"}})

    database, readerErr := splitter.Reader(testRuntime())
    if false == errors.Is(readerErr, ErrProviderReturnedNilDatabase) {
        t.Fatalf("expected ErrProviderReturnedNilDatabase, got database=%v err=%v", database, readerErr)
    }

    if nil != database {
        t.Fatal("expected no database beside the refusal")
    }
}

func TestReadWriteSplitter_ReaderRefusesAReplicaWhoseProviderRefused(t *testing.T) {
    registry, registryErr := NewManagerRegistry(
        []ProviderDefinition{
            ProviderDefinition{Name: "primary", Provider: &fakeProvider{}, Parameters: testConnectionParameters, IsDefault: true},
            ProviderDefinition{Name: "replica", Provider: &refusingSplitProvider{}, Parameters: testConnectionParameters},
        },
        ManagerRegistryConfig{Logger: &fakeLogger{}},
    )
    if nil != registryErr {
        t.Fatalf("registry: %v", registryErr)
    }
    t.Cleanup(func() { _ = registry.CloseWithContext(context.Background()) })

    splitter := NewReadWriteSplitter(registry, "primary", SplitterConfig{ReplicaNames: []string{"replica"}})

    database, readerErr := splitter.Reader(testRuntime())
    if nil != database {
        t.Fatal("expected no database beside the refusal")
    }

    if nil == readerErr || false == strings.Contains(readerErr.Error(), "the user is empty") {
        t.Fatalf("expected the provider's own refusal handed back, got %v", readerErr)
    }

    if primary, primaryErr := registry.Database(testRuntime(), "primary"); nil != primaryErr || nil == primary {
        t.Fatalf("the primary itself stays reachable: %v", primaryErr)
    }
}

type journaledUnreachableSplitProvider struct{}

func (instance *journaledUnreachableSplitProvider) Open(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return nil, exception.MarkLogged(exception.NewError("database connection failed", nil, DatabaseUnreachable(errors.New("primary is down"))))
}

func (instance *journaledUnreachableSplitProvider) OpenForMigration(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.Open(runtimeInstance, parameters, logger)
}

func TestReadWriteSplitter_ADoubleFailureOverAJournaledPrimaryStaysMarkedLogged(t *testing.T) {
    for _, journaled := range []bool{true, false} {
        var primaryProvider Provider = &unreachableSplitProvider{}
        if true == journaled {
            primaryProvider = &journaledUnreachableSplitProvider{}
        }

        registry, registryErr := NewManagerRegistry(
            []ProviderDefinition{
                ProviderDefinition{Name: "primary", Provider: primaryProvider, Parameters: testConnectionParameters, IsDefault: true},
                ProviderDefinition{Name: "replica", Provider: &unreachableSplitProvider{}, Parameters: testConnectionParameters},
            },
            ManagerRegistryConfig{Logger: &fakeLogger{}},
        )
        if nil != registryErr {
            t.Fatalf("registry: %v", registryErr)
        }

        _, readerErr := NewReadWriteSplitter(registry, "primary", SplitterConfig{ReplicaNames: []string{"replica"}}).Reader(testRuntime())
        if nil == readerErr {
            t.Fatal("expected the double failure refused")
        }

        if journaled != exception.IsAlreadyLogged(readerErr) {
            t.Fatalf("expected the refusal marked logged exactly when the primary's failure was (journaled=%v)", journaled)
        }
    }
}

type switchableSplitProvider struct {
    down      bool
    openCount int
}

func (instance *switchableSplitProvider) Open(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    instance.openCount++
    if true == instance.down {
        return nil, exception.NewError("database connection failed", nil, DatabaseUnreachable(errors.New("replica is down")))
    }

    database, _ := newCloseRaceDatabase()

    return database, nil
}

func (instance *switchableSplitProvider) OpenForMigration(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.Open(runtimeInstance, parameters, logger)
}

func TestReadWriteSplitter_AnUnreachableReplicaIsRememberedForTheInterval(t *testing.T) {
    replica := &switchableSplitProvider{down: true}
    registry, registryErr := NewManagerRegistry(
        []ProviderDefinition{
            ProviderDefinition{Name: "primary", Provider: &fakeProvider{}, Parameters: testConnectionParameters, IsDefault: true},
            ProviderDefinition{Name: "replica", Provider: replica, Parameters: testConnectionParameters},
        },
        ManagerRegistryConfig{Logger: &fakeLogger{}},
    )
    if nil != registryErr {
        t.Fatalf("registry: %v", registryErr)
    }

    now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
    splitter := NewReadWriteSplitter(registry, "primary", SplitterConfig{ReplicaNames: []string{"replica"}, ReplicaRetryInterval: bound.Of(time.Minute)})
    splitter.now = func() time.Time { return now }

    for range 2 {
        if _, readerErr := splitter.Reader(testRuntime()); nil != readerErr {
            t.Fatalf("expected the primary served in place of the replica, got: %v", readerErr)
        }
    }

    if 1 != replica.openCount {
        t.Fatalf("expected one dial inside the interval, got %d", replica.openCount)
    }

    now = now.Add(time.Minute)
    if _, readerErr := splitter.Reader(testRuntime()); nil != readerErr {
        t.Fatalf("expected the primary served in place of the replica, got: %v", readerErr)
    }

    if 2 != replica.openCount {
        t.Fatalf("expected the replica dialled again once the interval lapsed, got %d dials", replica.openCount)
    }
}

func TestReadWriteSplitter_PrimaryFallbackDisabledRefusesTheReadOfAnUnreachableReplica(t *testing.T) {
    primary := &switchableSplitProvider{}
    replica := &switchableSplitProvider{down: true}
    registry, registryErr := NewManagerRegistry(
        []ProviderDefinition{
            ProviderDefinition{Name: "primary", Provider: primary, Parameters: testConnectionParameters, IsDefault: true},
            ProviderDefinition{Name: "replica", Provider: replica, Parameters: testConnectionParameters},
        },
        ManagerRegistryConfig{Logger: &fakeLogger{}},
    )
    if nil != registryErr {
        t.Fatalf("registry: %v", registryErr)
    }

    splitter := NewReadWriteSplitter(registry, "primary", SplitterConfig{ReplicaNames: []string{"replica"}, ReplicaRetryInterval: bound.Of(time.Minute), PrimaryFallbackDisabled: true})

    for range 2 {
        database, readerErr := splitter.Reader(testRuntime())
        if nil != database || nil == readerErr || false == exception.ChainIs(readerErr, ErrDatabaseUnreachable) {
            t.Fatalf("expected the read refused with the replica's failure, got %v, %v", database, readerErr)
        }

        var exceptionErr *exception.Error
        if false == errors.As(readerErr, &exceptionErr) || "SplitterConfig.PrimaryFallbackDisabled" != exceptionErr.Context()["field"] {
            t.Fatalf("expected the refusal to name the field, got %v", readerErr)
        }
    }

    if 0 != primary.openCount || 1 != replica.openCount {
        t.Fatalf("expected the primary never opened and the replica dialled once inside the interval, got %d and %d", primary.openCount, replica.openCount)
    }
}

func TestReadWriteSplitter_WritesOneWarningPerTransition(t *testing.T) {
    logger := &capturingDiagnosticLogger{}
    replica := &switchableSplitProvider{down: true}
    registry, registryErr := NewManagerRegistry(
        []ProviderDefinition{
            ProviderDefinition{Name: "primary", Provider: &fakeProvider{}, Parameters: testConnectionParameters, IsDefault: true},
            ProviderDefinition{Name: "replica", Provider: replica, Parameters: testConnectionParameters},
        },
        ManagerRegistryConfig{Logger: logger},
    )
    if nil != registryErr {
        t.Fatalf("registry: %v", registryErr)
    }

    now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
    splitter := NewReadWriteSplitter(registry, "primary", SplitterConfig{ReplicaNames: []string{"replica"}})
    splitter.now = func() time.Time { return now }

    for range 3 {
        _, _ = splitter.Reader(testRuntime())
        now = now.Add(splitterReplicaRetryIntervalRule.Default)
    }

    replica.down = false
    _, _ = splitter.Reader(testRuntime())
    _, _ = splitter.Reader(testRuntime())

    warnings, infos := 0, 0
    for _, record := range logger.captured() {
        switch {
        case loggingcontract.LevelWarning == record.level && "read replica unreachable, reads served by the primary" == record.message:
            warnings++
        case loggingcontract.LevelInfo == record.level && "read replica reachable again, reads served by it" == record.message:
            infos++
        }
    }

    if 3 != replica.openCount-1 || 1 != warnings || 1 != infos {
        t.Fatalf("expected three failed dials journaled as one warning and the recovery as one info, got %d dials, %d warnings, %d infos", replica.openCount, warnings, infos)
    }
}

func TestNewReadWriteSplitter_AnAbsentReplicaRetryIntervalTakesFiveSeconds(t *testing.T) {
    registry, registryErr := NewManagerRegistry([]ProviderDefinition{ProviderDefinition{Name: "primary", Provider: &fakeProvider{}, Parameters: testConnectionParameters, IsDefault: true}}, ManagerRegistryConfig{Logger: &fakeLogger{}})
    if nil != registryErr {
        t.Fatalf("registry: %v", registryErr)
    }

    if splitter := NewReadWriteSplitter(registry, "primary", SplitterConfig{}); 5*time.Second != splitter.replicaRetryInterval {
        t.Fatalf("expected an absent interval to take 5s, got %s", splitter.replicaRetryInterval)
    }
}

func TestNewReadWriteSplitter_RefusesAReplicaRetryIntervalOutsideItsRuleNamingTheField(t *testing.T) {
    registry, registryErr := NewManagerRegistry([]ProviderDefinition{ProviderDefinition{Name: "primary", Provider: &fakeProvider{}, Parameters: testConnectionParameters, IsDefault: true}}, ManagerRegistryConfig{Logger: &fakeLogger{}})
    if nil != registryErr {
        t.Fatalf("registry: %v", registryErr)
    }

    for name, interval := range map[string]bound.Duration{
        "zero":              bound.Of(time.Duration(0)),
        "negative":          bound.Of(-time.Second),
        "below the minimum": bound.Of(time.Microsecond),
        "unlimited":         bound.UnlimitedDuration(),
    } {
        t.Run(name, func(t *testing.T) {
            expectPanicNamingField(t, "SplitterConfig.ReplicaRetryInterval", func() {
                NewReadWriteSplitter(registry, "primary", SplitterConfig{ReplicaRetryInterval: interval})
            })
        })
    }

    if splitter := NewReadWriteSplitter(registry, "primary", SplitterConfig{ReplicaRetryInterval: bound.Of(time.Millisecond)}); time.Millisecond != splitter.replicaRetryInterval {
        t.Fatalf("expected the 1ms minimum admitted, got %s", splitter.replicaRetryInterval)
    }
}

type cyclicChainError struct {
    next error
}

func (instance *cyclicChainError) Error() string {
    return "cyclic link"
}

func (instance *cyclicChainError) Unwrap() error {
    return instance.next
}

func newCyclicChainError() *cyclicChainError {
    first := &cyclicChainError{}
    first.next = &cyclicChainError{next: first}

    return first
}

func assertReturnsOnACyclicChain(t *testing.T, callback func()) {
    t.Helper()

    done := make(chan struct{})
    go func() {
        defer close(done)
        callback()
    }()

    select {
    case <-done:
    case <-time.After(5 * time.Second):
        t.Fatal("expected the call to return on an error chain that reaches itself")
    }
}

type cyclicSplitProvider struct{}

func (instance *cyclicSplitProvider) Open(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return nil, newCyclicChainError()
}

func (instance *cyclicSplitProvider) OpenForMigration(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.Open(runtimeInstance, parameters, logger)
}

func TestReadWriteSplitter_ReaderReturnsOnACyclicChainFromAProvider(t *testing.T) {
    registry, registryErr := NewManagerRegistry(
        []ProviderDefinition{
            {Name: "primary", Provider: &fakeProvider{}, Parameters: testConnectionParameters, IsDefault: true},
            {Name: "replica", Provider: &cyclicSplitProvider{}, Parameters: testConnectionParameters},
        },
        ManagerRegistryConfig{Logger: &fakeLogger{}},
    )
    if nil != registryErr {
        t.Fatalf("registry: %v", registryErr)
    }

    splitter := NewReadWriteSplitter(registry, "primary", SplitterConfig{ReplicaNames: []string{"replica"}})

    assertReturnsOnACyclicChain(t, func() {
        if _, readerErr := splitter.Reader(testRuntime()); nil == readerErr {
            t.Error("expected the provider's refusal")
        }
    })
}

type stallingSplitProvider struct {
    stall   chan struct{}
    entered chan struct{}
    opens   atomic.Int64
}

func (instance *stallingSplitProvider) Open(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    if 1 < instance.opens.Add(1) {
        instance.entered <- struct{}{}
        <-instance.stall
    }

    return nil, exception.NewError("database connection failed", nil, DatabaseUnreachable(errors.New("replica is down")))
}

func (instance *stallingSplitProvider) OpenForMigration(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.Open(runtimeInstance, parameters, logger)
}

func TestReadWriteSplitter_OneReaderProbesALapsedReplicaWhileTheOthersKeepThePrimary(t *testing.T) {
    replica := &stallingSplitProvider{stall: make(chan struct{}), entered: make(chan struct{}, 1)}
    registry, registryErr := NewManagerRegistry(
        []ProviderDefinition{
            ProviderDefinition{Name: "primary", Provider: &fakeProvider{}, Parameters: testConnectionParameters, IsDefault: true},
            ProviderDefinition{Name: "replica", Provider: replica, Parameters: testConnectionParameters},
        },
        ManagerRegistryConfig{Logger: &fakeLogger{}},
    )
    if nil != registryErr {
        t.Fatalf("registry: %v", registryErr)
    }

    var clock atomic.Int64
    start := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
    splitter := NewReadWriteSplitter(registry, "primary", SplitterConfig{ReplicaNames: []string{"replica"}, ReplicaRetryInterval: bound.Of(time.Minute)})
    splitter.now = func() time.Time { return start.Add(time.Duration(clock.Load())) }

    if _, readerErr := splitter.Reader(testRuntime()); nil != readerErr {
        t.Fatalf("expected the primary served in place of the replica, got: %v", readerErr)
    }

    clock.Store(int64(time.Minute))

    probed := make(chan error, 1)
    go func() {
        _, probeErr := splitter.Reader(testRuntime())
        probed <- probeErr
    }()

    select {
    case <-replica.entered:
    case <-time.After(2 * time.Second):
        t.Fatal("the lapsed interval dialled no probe")
    }

    served := make(chan error, 1)
    go func() {
        _, readerErr := splitter.Reader(testRuntime())
        served <- readerErr
    }()

    select {
    case readerErr := <-served:
        if nil != readerErr {
            t.Fatalf("expected the primary served beside the probe, got: %v", readerErr)
        }
    case <-time.After(2 * time.Second):
        t.Fatal("a reader beside the probe waited on the replica's open")
    }

    close(replica.stall)

    if probeErr := <-probed; nil != probeErr {
        t.Fatalf("expected the probe served by the primary once its dial failed, got: %v", probeErr)
    }

    if 2 != replica.opens.Load() {
        t.Fatalf("expected one dial for the lapsed interval, got %d", replica.opens.Load()-1)
    }
}

type departingCallerReplicaProvider struct {
    opens   atomic.Int32
    entered chan struct{}
}

func (instance *departingCallerReplicaProvider) Open(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    if 1 < instance.opens.Add(1) {
        database, _ := newCloseRaceDatabase()

        return database, nil
    }

    close(instance.entered)
    <-runtimeInstance.Context().Done()

    return nil, exception.NewError("database connection failed", nil, DatabaseUnreachable(runtimeInstance.Context().Err()))
}

func (instance *departingCallerReplicaProvider) OpenForMigration(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.Open(runtimeInstance, parameters, logger)
}

func TestReadWriteSplitter_AReplicaOpenTheCallerLeftIsNeitherRememberedNorServedFromThePrimary(t *testing.T) {
    primary := &fakeProvider{}
    replica := &departingCallerReplicaProvider{entered: make(chan struct{})}

    registry, registryErr := NewManagerRegistry(
        []ProviderDefinition{
            ProviderDefinition{Name: "primary", Provider: primary, Parameters: testConnectionParameters, IsDefault: true},
            ProviderDefinition{Name: "replica", Provider: replica, Parameters: testConnectionParameters},
        },
        ManagerRegistryConfig{Logger: &fakeLogger{}},
    )
    if nil != registryErr {
        t.Fatalf("registry: %v", registryErr)
    }
    t.Cleanup(func() { _ = registry.CloseWithContext(context.Background()) })

    splitter := NewReadWriteSplitter(registry, "primary", SplitterConfig{ReplicaNames: []string{"replica"}, ReplicaRetryInterval: bound.Of(time.Hour)})

    callerContext, cancelCaller := context.WithCancel(context.Background())
    answered := make(chan error, 1)
    go func() {
        _, readerErr := splitter.Reader(testRuntimeUnder(callerContext))
        answered <- readerErr
    }()

    <-replica.entered
    cancelCaller()

    select {
    case readerErr := <-answered:
        if false == errors.Is(readerErr, context.Canceled) {
            t.Fatalf("expected the caller's cancellation, got %v", readerErr)
        }
    case <-time.After(2 * time.Second):
        t.Fatal("expected the replica open to end with the caller's context")
    }

    if 0 != primary.openCount {
        t.Fatalf("expected the primary left unopened, got %d opens", primary.openCount)
    }

    if _, rememberedErr := splitter.rememberedUnreachable("replica"); nil != rememberedErr {
        t.Fatal("expected the replica not remembered unreachable")
    }

    if _, readerErr := splitter.Reader(testRuntime()); nil != readerErr {
        t.Fatalf("expected the next read to dial the replica, got %v", readerErr)
    }

    if 2 != replica.opens.Load() || 0 != primary.openCount {
        t.Fatalf("expected the replica dialled again and the primary untouched, got %d replica opens and %d primary opens", replica.opens.Load(), primary.openCount)
    }
}

func TestReadWriteSplitter_AProbeTheCallerCutGivesTheReservationBack(t *testing.T) {
    replica := &switchableSplitProvider{down: true}
    registry, registryErr := NewManagerRegistry(
        []ProviderDefinition{
            ProviderDefinition{Name: "primary", Provider: &fakeProvider{}, Parameters: testConnectionParameters, IsDefault: true},
            ProviderDefinition{Name: "replica", Provider: replica, Parameters: testConnectionParameters},
        },
        ManagerRegistryConfig{Logger: &fakeLogger{}},
    )
    if nil != registryErr {
        t.Fatalf("registry: %v", registryErr)
    }
    t.Cleanup(func() { _ = registry.CloseWithContext(context.Background()) })

    now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
    splitter := NewReadWriteSplitter(registry, "primary", SplitterConfig{ReplicaNames: []string{"replica"}, ReplicaRetryInterval: bound.Of(time.Hour)})
    splitter.now = func() time.Time { return now }

    if _, readerErr := splitter.Reader(testRuntime()); nil != readerErr {
        t.Fatalf("expected the primary served in place of the replica, got: %v", readerErr)
    }

    replica.down = false
    now = now.Add(time.Hour)

    departedCause := errors.New("client gone")
    departedContext, cancelDeparted := context.WithCancelCause(context.Background())
    cancelDeparted(departedCause)

    if _, readerErr := splitter.Reader(testRuntimeUnder(departedContext)); false == errors.Is(readerErr, departedCause) {
        t.Fatalf("expected the probing caller's own cause, got %v", readerErr)
    }

    if _, readerErr := splitter.Reader(testRuntime()); nil != readerErr {
        t.Fatalf("expected the next read to probe the replica, got %v", readerErr)
    }

    if 2 != replica.openCount {
        t.Fatalf("expected the replica probed by the next live reader, got %d dials", replica.openCount)
    }
}

func TestReadWriteSplitter_AReservationGivenBackLeavesALaterDateStanding(t *testing.T) {
    registry, registryErr := NewManagerRegistry(
        []ProviderDefinition{
            ProviderDefinition{Name: "primary", Provider: &fakeProvider{}, Parameters: testConnectionParameters, IsDefault: true},
        },
        ManagerRegistryConfig{Logger: &fakeLogger{}},
    )
    if nil != registryErr {
        t.Fatalf("registry: %v", registryErr)
    }

    lapsedAt := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
    reservedAt := lapsedAt.Add(time.Hour)
    laterAt := reservedAt.Add(time.Second)

    splitter := NewReadWriteSplitter(registry, "primary", SplitterConfig{ReplicaNames: []string{"replica"}, ReplicaRetryInterval: bound.Of(time.Hour)})
    splitter.unreachableReplicas["replica"] = unreachableReplica{failedAt: laterAt, err: errors.New("replica is down")}

    splitter.releaseProbe("replica", probeReservation{reserved: true, failedAt: lapsedAt, reservedAt: reservedAt})

    if false == splitter.unreachableReplicas["replica"].failedAt.Equal(laterAt) {
        t.Fatalf("expected the later date to stand, got %v", splitter.unreachableReplicas["replica"].failedAt)
    }

    splitter.releaseProbe("replica", probeReservation{reserved: true, failedAt: lapsedAt, reservedAt: laterAt})

    if false == splitter.unreachableReplicas["replica"].failedAt.Equal(lapsedAt) {
        t.Fatalf("expected the reservation given back, got %v", splitter.unreachableReplicas["replica"].failedAt)
    }
}
