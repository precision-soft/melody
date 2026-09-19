package bunorm

import (
    "fmt"
    "strings"
    "sync"
    "testing"

    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    "github.com/uptrace/bun/schema"
)

/* bun's own diagnostics reach the application's journal instead of standard error. They are the developer's declaration mistakes — here a query carrying an argument with nowhere to put it — and written to standard error as unstructured text they are invisible to a deployment whose journal is a json file.

   The warning is provoked through bun's public surface rather than by calling its logger, because what has to be proven is that bun uses the destination this package installs, not that a *log.Logger writes where it was pointed. It goes through the EXPORTED door: the once now guards only the forwarder, and the destination behind it is replaced on every call, so a second run of this test proves exactly what the first did. */
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

/* TestRouteDiagnostics_ASecondRoutingTakesTheChannelBack is the guard the whole retargeting exists for. A process that builds, closes and rebuilds its application routes twice; under the old shape the first routing owned bun's channel for the life of the process, so the SECOND lifecycle's diagnostics were dropped into the first lifecycle's logger — closed by then, or the emergency fallback of a registry wired before its application had a logger at all.

   The assertion is two-sided on purpose: the second logger must receive the record AND the first must not. A one-sided check passes on a forwarder that writes to both. */
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

/* TestResetDiagnostics_HandsTheChannelBack pins what a teardown gets. The registry calls it from its own Close, while the logger it routed to is still alive, so no record is written into a journal that is closing; afterwards the line belongs on standard error, which is where bun puts it when nobody routes it at all.

   What is asserted is that the routed logger stops receiving. The fallback's own destination is standard error by construction — asserting on the process console would be asserting on the test runner's output, not on this package. */
func TestResetDiagnostics_HandsTheChannelBack(t *testing.T) {
    logger := &capturingDiagnosticLogger{}

    RouteDiagnostics(logger)

    ResetDiagnostics()

    _ = schema.SafeQuery("SELECT 1", []any{42})

    if 0 != len(logger.captured()) {
        t.Fatalf("the routed logger still holds bun's channel after the hand-back: %v", logger.captured())
    }
}

/* a provider that could not resolve a logger takes nothing on the way past: without the guard it would install an adapter over nothing, and bun's diagnostics would go from standard error to nowhere at all — a destination strictly worse than the one it replaced. */
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

/* TestRouteDiagnostics_ATypedNilLoggerLeavesTheDestinationWhereItWas is the same guard for the nil that a plain nil comparison lets through. A resolver answering a nil pointer of its own logger type produces a non-nil interface, so without the typed-nil reading the destination would be replaced by a receiver whose first record panics — inside bun's own logging call, one frame from a query.

   The double dereferences its receiver on every method, which is what lets the guard die: a double whose methods tolerate a nil receiver would pass with the guard removed. */
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

/* the providers route on every open; a routing on the logger already installed must install nothing, or every open allocated a fresh writer for the same journal and replaced the live one for no change */
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

/* the once installs the forwarder, not a destination, so a routing after a hand-back reaches the journal again through the same forwarder: the once never needs resetting */
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

/* the hand-back a teardown asks for is scoped to its own logger: when another logger holds the channel, nothing happens */
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

/* sliceCarryingLogger is a logger held by value whose dynamic type is not comparable — the shape of a test double or an integrator's adapter that keeps its records in a slice — so `==` between two Logger interfaces holding it panics at runtime */
type sliceCarryingLogger struct {
    records []string
}

func (instance sliceCarryingLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {}
func (instance sliceCarryingLogger) Debug(message string, context loggingcontract.Context)     {}
func (instance sliceCarryingLogger) Info(message string, context loggingcontract.Context)      {}
func (instance sliceCarryingLogger) Warning(message string, context loggingcontract.Context)   {}
func (instance sliceCarryingLogger) Error(message string, context loggingcontract.Context)     {}
func (instance sliceCarryingLogger) Emergency(message string, context loggingcontract.Context) {}

/* routing bun's diagnostics to a logger whose dynamic type is not comparable panicked on the SECOND routing — the first found no live target to compare against — and at a registry's Close through the hand-back, for a logger that had routed fine once: a bare comparison of two interfaces is a runtime panic on an incomparable dynamic type. Such a logger has no identity, so the hand-back goes through the destination the routing answered */
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

/* two distinct loggers of equal content, both without identity, are two loggers: read by content they were one, so the Close of the registry holding the second took the channel the first had routed */
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

/* mapCarryingLogger writes its own map under its own mutex on every record — the shape of a logger that keeps counters — so a comparison that read its content would read the map under no lock while a record is being written */
type mapCarryingLogger struct {
    mutex  *sync.Mutex
    counts map[string]int
}

func (instance mapCarryingLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.counts[message]++
}

func (instance mapCarryingLogger) Debug(message string, context loggingcontract.Context)   { instance.Log(loggingcontract.LevelDebug, message, context) }
func (instance mapCarryingLogger) Info(message string, context loggingcontract.Context)    { instance.Log(loggingcontract.LevelInfo, message, context) }
func (instance mapCarryingLogger) Warning(message string, context loggingcontract.Context) { instance.Log(loggingcontract.LevelWarning, message, context) }
func (instance mapCarryingLogger) Error(message string, context loggingcontract.Context)   { instance.Log(loggingcontract.LevelError, message, context) }
func (instance mapCarryingLogger) Emergency(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelEmergency, message, context)
}

/* a routing and a hand-back on a logger without identity read none of its content: read by content, the comparison walked the logger's map while its own Log was writing it, a data race the detector reported on every run. Proven under -race, the shape a mutation cannot separate. */
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
