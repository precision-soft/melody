package bunorm

import (
    "bytes"
    "fmt"
    "strings"
    "sync"
    "testing"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
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

    resetDiagnosticsRoutedTo(first, nil)

    _ = schema.SafeQuery("SELECT 1", []any{42})

    if 1 != len(second.captured()) {
        t.Fatalf("the hand-back for the first logger took the channel away from the second: %d records", len(second.captured()))
    }

    resetDiagnosticsRoutedTo(second, nil)

    _ = schema.SafeQuery("SELECT 1", []any{42})

    if 1 != len(second.captured()) {
        t.Fatalf("the hand-back for the live logger did not take the channel back: %d records", len(second.captured()))
    }
}

type sliceCarryingLogger struct {
    records []string
}

func (instance sliceCarryingLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
}
func (instance sliceCarryingLogger) Debug(message string, context loggingcontract.Context)     {}
func (instance sliceCarryingLogger) Info(message string, context loggingcontract.Context)      {}
func (instance sliceCarryingLogger) Warning(message string, context loggingcontract.Context)   {}
func (instance sliceCarryingLogger) Error(message string, context loggingcontract.Context)     {}
func (instance sliceCarryingLogger) Emergency(message string, context loggingcontract.Context) {}

func (instance sliceCarryingLogger) Enabled(level loggingcontract.Level) bool {
    return true
}

func (instance sliceCarryingLogger) CauseChainDepth() int {
    return 8
}

func TestRouteDiagnostics_ALoggerWhoseTypeIsNotComparableRoutesTwiceAndHandsBackThroughTheDestinationItRouted(t *testing.T) {
    t.Cleanup(ResetDiagnostics)

    logger := sliceCarryingLogger{records: make([]string, 0)}

    RouteDiagnostics(logger)
    routed := routeDiagnosticsTo(logger)

    if nil == routed || routed != bunDiagnosticsTarget.Load() {
        t.Fatal("expected the second routing to answer the live target")
    }

    resetDiagnosticsRoutedTo(logger, routed)

    if nil != bunDiagnosticsTarget.Load() {
        t.Fatal("expected the hand-back to reach the destination the routing answered")
    }
}

func TestResetDiagnosticsRoutedTo_TwoValueLoggersOfEqualContentAreNotOneLogger(t *testing.T) {
    t.Cleanup(ResetDiagnostics)

    first := sliceCarryingLogger{records: make([]string, 0)}
    second := sliceCarryingLogger{records: make([]string, 0)}

    routedFirst := routeDiagnosticsTo(first)
    routedSecond := routeDiagnosticsTo(second)

    if routedFirst == routedSecond {
        t.Fatal("expected a logger without identity to be routed afresh rather than deduplicated against another of equal content")
    }

    resetDiagnosticsRoutedTo(first, routedFirst)

    if routedSecond != bunDiagnosticsTarget.Load() {
        t.Fatal("the hand-back for the first logger took the channel away from the second, which holds it")
    }
}

type mapCarryingLogger struct {
    mutex  *sync.Mutex
    counts map[string]int
}

func (instance mapCarryingLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.counts[message]++
}

func (instance mapCarryingLogger) Debug(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelDebug, message, context)
}
func (instance mapCarryingLogger) Info(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelInfo, message, context)
}
func (instance mapCarryingLogger) Warning(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelWarning, message, context)
}
func (instance mapCarryingLogger) Error(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelError, message, context)
}
func (instance mapCarryingLogger) Emergency(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelEmergency, message, context)
}

func (instance mapCarryingLogger) Enabled(level loggingcontract.Level) bool {
    return true
}

func (instance mapCarryingLogger) CauseChainDepth() int {
    return 8
}

func TestRouteDiagnostics_ALoggerWithoutIdentityIsNeverReadWhileItWrites(t *testing.T) {
    t.Cleanup(ResetDiagnostics)

    logger := mapCarryingLogger{mutex: &sync.Mutex{}, counts: make(map[string]int)}
    routed := routeDiagnosticsTo(logger)

    var writers sync.WaitGroup
    stop := make(chan struct{})

    writers.Add(1)
    go func() {
        defer writers.Done()

        for {
            select {
            case <-stop:
                return
            default:
                logger.Warning("bun diagnostic", nil)
            }
        }
    }()

    for round := 0; round < 200; round++ {
        routeDiagnosticsTo(logger)
        resetDiagnosticsRoutedTo(logger, routed)
    }

    close(stop)
    writers.Wait()
}

func TestRegistryLogger_ForwardsTheWrappedLoggersThreshold(t *testing.T) {
    filtered := newRegistryLogger(&errorOnlyLogger{})

    if true == filtered.Enabled(loggingcontract.LevelWarning) || false == filtered.Enabled(loggingcontract.LevelError) {
        t.Fatal("expected the wrapped logger's threshold to hold through the registry's logger")
    }

    if false == newRegistryLogger(&capturingDiagnosticLogger{}).Enabled(loggingcontract.LevelDebug) {
        t.Fatal("expected a logger enabled at debug to stay enabled through the registry's logger")
    }
}

func TestRegistryLogger_ForwardsTheWrappedLoggersCauseChainDepth(t *testing.T) {
    configured := newRegistryLogger(logging.NewJsonLogger(&bytes.Buffer{}, logging.JsonLoggerConfig{CauseChainDepth: bound.Of(2)}))
    if 2 != configured.CauseChainDepth() {
        t.Fatalf("expected the wrapped logger's depth of 2 through the registry's logger, got %d", configured.CauseChainDepth())
    }

    if 8 != newRegistryLogger(&capturingDiagnosticLogger{}).CauseChainDepth() {
        t.Fatal("expected a logger without a depth to be read as the default")
    }
}

func TestRegistryLogger_RoutesBunsRecordsToTheWrappedLogger(t *testing.T) {
    t.Cleanup(ResetDiagnostics)

    inner := &capturingDiagnosticLogger{}
    RouteDiagnostics(newRegistryLogger(inner))

    _ = schema.SafeQuery("SELECT 1", []any{42})

    if 1 != len(inner.captured()) {
        t.Fatalf("expected the record on the wrapped logger, got %d", len(inner.captured()))
    }
}

func TestRouteDiagnostics_ARetiredRegistryLoggerRoutesNothing(t *testing.T) {
    t.Cleanup(ResetDiagnostics)

    owned := newRegistryLogger(&capturingDiagnosticLogger{})
    owned.retired.Store(true)

    if nil != routeDiagnosticsTo(owned) || nil != bunDiagnosticsTarget.Load() {
        t.Fatal("expected a retired registry logger to route nothing")
    }
}
