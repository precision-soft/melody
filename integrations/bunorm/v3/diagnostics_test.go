package bunorm

import (
    "errors"
    "fmt"
    "strings"
    "testing"

    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    "github.com/uptrace/bun/schema"
)

func TestRouteDiagnostics_SendsBunsOwnChannelToTheJournal(t *testing.T) {
    logger := &capturingDiagnosticLogger{}

    RouteDiagnostics(logger)
    t.Cleanup(ResetDiagnostics)

    _ = schema.SafeQuery("SELECT 1", []any{42})

    records := logger.captured()
    if 0 == len(records) {
        t.Fatal("bun's own diagnostic did not reach the journal")
    }

    if "bun diagnostic" != records[0].message {
        t.Fatalf("message = %q, want %q", records[0].message, "bun diagnostic")
    }

    if loggingcontract.LevelWarning != records[0].level {
        t.Fatalf("level = %v, want warning", records[0].level)
    }

    line, present := records[0].context["line"]
    if false == present {
        t.Fatalf("the record does not carry the line: %v", records[0].context)
    }

    if false == strings.Contains(fmt.Sprintf("%v", line), "placeholders") {
        t.Fatalf("line = %v, want bun's own wording", line)
    }
}

func TestRouteDiagnostics_ASecondRoutingTakesTheChannelBack(t *testing.T) {
    firstLifecycle := &capturingDiagnosticLogger{}
    secondLifecycle := &capturingDiagnosticLogger{}

    RouteDiagnostics(firstLifecycle)
    RouteDiagnostics(secondLifecycle)
    t.Cleanup(ResetDiagnostics)

    _ = schema.SafeQuery("SELECT 1", []any{42})

    if 0 == len(secondLifecycle.captured()) {
        t.Fatal("the second routing did not take bun's channel: the record never arrived")
    }

    if 0 != len(firstLifecycle.captured()) {
        t.Fatalf("the first routing still holds bun's channel: %v", firstLifecycle.captured())
    }
}

func TestResetDiagnostics_HandsTheChannelBack(t *testing.T) {
    logger := &capturingDiagnosticLogger{}

    RouteDiagnostics(logger)

    ResetDiagnostics()

    _ = schema.SafeQuery("SELECT 1", []any{42})

    if 0 != len(logger.captured()) {
        t.Fatalf("the routed logger still holds bun's channel after the hand-back: %v", logger.captured())
    }
}

func TestRouteDiagnostics_ANilLoggerLeavesTheDestinationWhereItWas(t *testing.T) {
    logger := &capturingDiagnosticLogger{}
    RouteDiagnostics(logger)
    t.Cleanup(ResetDiagnostics)

    RouteDiagnostics(nil)

    _ = schema.SafeQuery("SELECT 1", []any{42})

    if 0 == len(logger.captured()) {
        t.Fatal("a nil logger replaced the destination that was already installed")
    }
}

func TestRouteDiagnostics_ATypedNilLoggerLeavesTheDestinationWhereItWas(t *testing.T) {
    logger := &capturingDiagnosticLogger{}
    RouteDiagnostics(logger)
    t.Cleanup(ResetDiagnostics)

    var typedNil *capturingDiagnosticLogger
    RouteDiagnostics(typedNil)

    _ = schema.SafeQuery("SELECT 1", []any{42})

    if 0 == len(logger.captured()) {
        t.Fatal("a typed nil logger replaced the destination that was already installed")
    }
}

func TestRouteDiagnostics_ARoutingOnTheSameLoggerInstallsNothing(t *testing.T) {
    logger := &capturingDiagnosticLogger{}
    t.Cleanup(ResetDiagnostics)

    RouteDiagnostics(logger)
    installed := bunDiagnosticsTarget.Load()
    if nil == installed {
        t.Fatal("the first routing installed nothing")
    }

    RouteDiagnostics(logger)
    RouteDiagnostics(logger)

    if installed != bunDiagnosticsTarget.Load() {
        t.Fatal("a routing on the same logger replaced the live destination")
    }

    _ = schema.SafeQuery("SELECT 1", []any{42})

    if 1 != len(logger.captured()) {
        t.Fatalf("expected exactly one record on the routed logger, got %d", len(logger.captured()))
    }
}

func TestRouteDiagnostics_RoutesAgainAfterAHandBack(t *testing.T) {
    logger := &capturingDiagnosticLogger{}
    t.Cleanup(ResetDiagnostics)

    RouteDiagnostics(logger)
    ResetDiagnostics()
    RouteDiagnostics(logger)

    _ = schema.SafeQuery("SELECT 1", []any{42})

    if 1 != len(logger.captured()) {
        t.Fatalf("expected the record to reach the logger routed after the hand-back, got %d", len(logger.captured()))
    }
}

func TestResetDiagnosticsRoutedTo_LeavesAnotherLoggersChannelAlone(t *testing.T) {
    first := &capturingDiagnosticLogger{}
    second := &capturingDiagnosticLogger{}
    t.Cleanup(ResetDiagnostics)

    RouteDiagnostics(first)
    RouteDiagnostics(second)

    resetDiagnosticsRoutedTo(first)

    _ = schema.SafeQuery("SELECT 1", []any{42})

    if 1 != len(second.captured()) {
        t.Fatalf("the hand-back for the first logger took the channel away from the second: %d records", len(second.captured()))
    }

    resetDiagnosticsRoutedTo(second)

    _ = schema.SafeQuery("SELECT 1", []any{42})

    if 1 != len(second.captured()) {
        t.Fatalf("the hand-back for the live logger did not take the channel back: %d records", len(second.captured()))
    }
}

type valueDiagnosticLogger struct {
    loggingcontract.Logger
    labels []string
}

func TestRouteDiagnostics_AValueLoggerWithSliceCanBeRoutedTwice(t *testing.T) {
    ResetDiagnostics()
    t.Cleanup(ResetDiagnostics)
    captured := &capturingDiagnosticLogger{}
    logger := valueDiagnosticLogger{Logger: captured, labels: []string{"database"}}
    RouteDiagnostics(logger)
    RouteDiagnostics(logger)
    _ = schema.SafeQuery("SELECT 1", []any{42})
    if 1 != len(captured.captured()) {
        t.Fatal("diagnostic did not reach the value logger")
    }
}

func TestRegistryDiagnostics_ValueLoggerKeepsOwnedReset(t *testing.T) {
    for _, throughSetter := range []bool{false, true} {
        t.Run(fmt.Sprint(throughSetter), func(t *testing.T) {
            ResetDiagnostics()
            t.Cleanup(ResetDiagnostics)
            captured := &capturingDiagnosticLogger{}
            logger := valueDiagnosticLogger{Logger: captured, labels: []string{"database"}}
            registry, err := NewManagerRegistry(logger, ProviderDefinition{Name: "default", Provider: &fakeProvider{}})
            if nil != err {
                t.Fatal(err)
            }
            if throughSetter {
                if err := registry.SetLogger(logger); nil != err {
                    t.Fatal(err)
                }
            } else {
                RouteDiagnostics(registry.currentLogger())
            }
            installed := bunDiagnosticsTarget.Load()
            RouteDiagnostics(registry.currentLogger())
            if installed != bunDiagnosticsTarget.Load() {
                t.Fatal("registry logger lost its stable routing identity")
            }
            _ = schema.SafeQuery("SELECT 1", []any{42})
            if 1 != len(captured.captured()) {
                t.Fatal("registry logger did not receive the diagnostic")
            }
            if err := registry.Close(); nil != err {
                t.Fatal(err)
            }
            if nil != bunDiagnosticsTarget.Load() {
                t.Fatal("registry shutdown retained its own diagnostic destination")
            }
        })
    }
}

func TestResetDiagnostics_ValueLoggerDoesNotStealAnotherDestination(t *testing.T) {
    ResetDiagnostics()
    t.Cleanup(ResetDiagnostics)
    first := valueDiagnosticLogger{Logger: &capturingDiagnosticLogger{}, labels: []string{"first"}}
    second := valueDiagnosticLogger{Logger: &capturingDiagnosticLogger{}, labels: []string{"second"}}
    RouteDiagnostics(second)
    installed := bunDiagnosticsTarget.Load()
    resetDiagnosticsRoutedTo(first)
    if installed != bunDiagnosticsTarget.Load() {
        t.Fatal("an unidentifiable logger reset another logger's destination")
    }
}

type filteredValueDiagnosticLogger struct {
    valueDiagnosticLogger
}

func (instance filteredValueDiagnosticLogger) Enabled(level loggingcontract.Level) bool {
    return loggingcontract.LevelError == level
}

func TestRegistryDiagnosticLogger_PreservesLevelFiltering(t *testing.T) {
    logger := filteredValueDiagnosticLogger{valueDiagnosticLogger{Logger: &capturingDiagnosticLogger{}, labels: []string{"filtered"}}}
    wrapped := diagnosticLoggerWithIdentity(logger)
    reporter := wrapped.(loggingcontract.LevelReporter)
    if reporter.Enabled(loggingcontract.LevelWarning) || false == reporter.Enabled(loggingcontract.LevelError) {
        t.Fatal("registry identity wrapper changed level filtering")
    }
    unfiltered := diagnosticLoggerWithIdentity(logger.valueDiagnosticLogger).(loggingcontract.LevelReporter)
    if false == unfiltered.Enabled(loggingcontract.LevelWarning) {
        t.Fatal("logger without level reporting lost its default-enabled behavior")
    }
}

func TestRouteDiagnostics_LoggerWithNestedIncomparableValue(t *testing.T) {
    ResetDiagnostics()
    t.Cleanup(ResetDiagnostics)
    captured := &capturingDiagnosticLogger{}
    logger := struct{ loggingcontract.Logger }{
        Logger: valueDiagnosticLogger{Logger: captured, labels: []string{"nested"}},
    }
    RouteDiagnostics(logger)
    RouteDiagnostics(logger)
    _ = schema.SafeQuery("SELECT 1", []any{42})
    if 1 != len(captured.captured()) {
        t.Fatal("diagnostic did not reach the nested value logger")
    }

    registry, err := NewManagerRegistry(logger, ProviderDefinition{Name: "default", Provider: &fakeProvider{}})
    if nil != err {
        t.Fatal(err)
    }
    RouteDiagnostics(registry.currentLogger())
    installed := bunDiagnosticsTarget.Load()
    RouteDiagnostics(registry.currentLogger())
    if installed != bunDiagnosticsTarget.Load() {
        t.Fatal("nested value logger lost its registry identity")
    }
    if err := registry.Close(); nil != err {
        t.Fatal(err)
    }
    if nil != bunDiagnosticsTarget.Load() {
        t.Fatal("registry did not reset the nested logger destination")
    }
}

func TestRegistryDiagnostics_ClosedRegistryCannotRetakeRouting(t *testing.T) {
    ResetDiagnostics()
    t.Cleanup(ResetDiagnostics)
    original := &capturingDiagnosticLogger{}
    registry, err := NewManagerRegistry(original, ProviderDefinition{Name: "default", Provider: &fakeProvider{}})
    if nil != err {
        t.Fatal(err)
    }
    if err := registry.SetLogger(original); nil != err {
        t.Fatal(err)
    }
    if err := registry.Close(); nil != err {
        t.Fatal(err)
    }

    active := &capturingDiagnosticLogger{}
    RouteDiagnostics(active)
    late := &capturingDiagnosticLogger{}
    setErr := registry.SetLogger(late)
    if false == errors.Is(setErr, ErrManagerRegistryClosed) {
        t.Errorf("closed registry accepted a logger replacement: %v", setErr)
    }
    _ = schema.SafeQuery("SELECT 1", []any{42})
    if 1 != len(active.captured()) || 0 != len(late.captured()) {
        t.Errorf("closed registry stole diagnostics: active=%d late=%d", len(active.captured()), len(late.captured()))
    }
    if original != registry.currentLogger().(*registryDiagnosticLogger).Logger {
        t.Error("closed registry changed its logger")
    }
}

func TestRegistryDiagnostics_ConcurrentReplacementAndCloseLeaveNoDestination(t *testing.T) {
    ResetDiagnostics()
    t.Cleanup(ResetDiagnostics)
    for iteration := 0; iteration < 64; iteration++ {
        registry, err := NewManagerRegistry(&capturingDiagnosticLogger{}, ProviderDefinition{Name: "default", Provider: &fakeProvider{}})
        if nil != err {
            t.Fatal(err)
        }
        start := make(chan struct{})
        replaced := make(chan error, 1)
        closed := make(chan error, 1)
        go func() {
            <-start
            replaced <- registry.SetLogger(&capturingDiagnosticLogger{})
        }()
        go func() {
            <-start
            closed <- registry.Close()
        }()
        close(start)
        if err := <-replaced; nil != err && false == errors.Is(err, ErrManagerRegistryClosed) {
            t.Fatal(err)
        }
        if err := <-closed; nil != err {
            t.Fatal(err)
        }
        if nil != bunDiagnosticsTarget.Load() {
            t.Fatalf("iteration %d: completed shutdown retained a diagnostic destination", iteration)
        }
    }
}
