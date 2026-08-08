package debug

import (
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "os"
    "os/exec"
    "strings"
    "testing"
    "time"
    "unicode/utf8"

    "github.com/precision-soft/melody/cli/output"
    "github.com/precision-soft/melody/container"
    containercontract "github.com/precision-soft/melody/container/contract"
    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
)

/* truncateTableCellValue must not split a multibyte UTF-8 rune when it cuts an over-long cell */
func TestTruncateTableCellValue_KeepsRunesIntactOnMultibyteOverflow(t *testing.T) {
    value := strings.Repeat("ș", 115) /* 230 bytes, over the 220 byte cap; the 217 byte cut lands mid rune */

    result := truncateTableCellValue(value)

    if false == utf8.ValidString(result) {
        t.Fatalf("truncated cell is not valid UTF-8: %q", result)
    }
    if false == strings.HasSuffix(result, "...") {
        t.Fatalf("truncated cell must end with an ellipsis, got %q", result)
    }
    if len(result) > 220 {
        t.Fatalf("truncated cell exceeds the byte cap: %d bytes", len(result))
    }
}

/* wrapFixedWidth must not split a multibyte UTF-8 rune at the wrap boundary */
func TestWrapFixedWidth_KeepsRunesIntactAtBoundary(t *testing.T) {
    /* the leading ASCII byte shifts every 2 byte rune onto an odd offset, so the even width boundary lands mid rune on the unpatched code */
    value := "a" + strings.Repeat("ș", 60) /* 121 bytes */

    lines := wrapFixedWidth(value, 80)

    for index, line := range lines {
        if false == utf8.ValidString(line) {
            t.Fatalf("wrapped line %d is not valid UTF-8: %q", index, line)
        }
    }

    if strings.Join(lines, "") != value {
        t.Fatalf("wrapped lines do not reconstruct the original value")
    }
}

/* resolveErrorContextJson must strip stack keys even when marshalling fails and it falls back to fmt */
func TestResolveErrorContextJson_RedactsStackOnMarshalFailure(t *testing.T) {
    contextValue := exceptioncontract.Context{
        "stack":      "SECRET_STACK_TRACE",
        "panicStack": "SECRET_PANIC_STACK",
        "channel":    make(chan int), /* not JSON marshalable, so it forces the fmt fallback */
    }

    resolveErr := exception.NewError("boom", contextValue, nil)

    result := resolveErrorContextJson(
        resolveErr,
        output.Option{
            Format:         output.FormatTable,
            VerbosityLevel: 0,
        },
    )

    if true == strings.Contains(result, "SECRET_STACK_TRACE") {
        t.Fatalf("stack value leaked into the fallback output: %q", result)
    }
    if true == strings.Contains(result, "SECRET_PANIC_STACK") {
        t.Fatalf("panicStack value leaked into the fallback output: %q", result)
    }
}

type brokenService struct {
}

func TestContainerCommand_FailsWhenTheRequestedServiceIsMissing(t *testing.T) {
    runtimeInstance := newTestRuntime(container.NewContainer())

    rendered, runErr := runDebugCommand(
        &ContainerCommand{},
        runtimeInstance,
        []string{"--format=json", "missing.service.name"},
    )

    var exitError *exception.ExitError
    if false == errors.As(runErr, &exitError) {
        t.Fatalf("expected an exit error for a missing service, got %v", runErr)
    }
    if 0 == exitError.ExitCode() {
        t.Fatalf("expected a non-zero exit code")
    }
    if false == strings.Contains(rendered, "debug.notFound") {
        t.Fatalf("expected a notFound envelope error, got %q", rendered)
    }
}

func TestContainerCommand_DoesNotReportARegisteredServiceThatFailsToBuildAsMissing(t *testing.T) {
    providers := map[string]any{
        "returnsAnError": func(resolver containercontract.Resolver) (*brokenService, error) {
            return nil, exception.NewError("dependency unavailable", nil, nil)
        },
        "panics": func(resolver containercontract.Resolver) (*brokenService, error) {
            exception.Panic(
                exception.NewError("dependency unavailable", nil, nil),
            )

            return nil, nil
        },
    }

    for name, provider := range providers {
        t.Run(name, func(t *testing.T) {
            serviceContainer := container.NewContainer()
            serviceContainer.MustRegister("broken.service.name", provider)

            runtimeInstance := newTestRuntime(serviceContainer)

            rendered, runErr := runDebugCommand(
                &ContainerCommand{},
                runtimeInstance,
                []string{"--format=json", "broken.service.name"},
            )

            var exitError *exception.ExitError
            if false == errors.As(runErr, &exitError) {
                t.Fatalf("expected an exit error for a service that fails to build, got %v", runErr)
            }
            if true == strings.Contains(rendered, "debug.notFound") {
                t.Fatalf("a registered service that fails to build must not be reported as missing, got %q", rendered)
            }
            if false == strings.Contains(rendered, "debug.buildFailed") {
                t.Fatalf("expected a buildFailed envelope error, got %q", rendered)
            }
        })
    }
}

func TestContainerCommand_SucceedsForAResolvableService(t *testing.T) {
    serviceContainer := container.NewContainer()

    serviceContainer.MustRegister(
        "working.service.name",
        func(resolver containercontract.Resolver) (*brokenService, error) {
            return &brokenService{}, nil
        },
    )

    runtimeInstance := newTestRuntime(serviceContainer)

    _, runErr := runDebugCommand(
        &ContainerCommand{},
        runtimeInstance,
        []string{"--format=json", "working.service.name"},
    )
    if nil != runErr {
        t.Fatalf("expected no error for a resolvable service, got %v", runErr)
    }
}

type containerCommandTestEnvelope struct {
    Data struct {
        Items []struct {
            Name string `json:"name"`
        } `json:"items"`
        Total int `json:"total"`
    } `json:"data"`
}

func newContainerListTestRuntime(serviceCount int) *testRuntime {
    serviceContainer := container.NewContainer()

    for index := 0; index < serviceCount; index++ {
        serviceContainer.MustRegister(
            fmt.Sprintf("service.%02d", index),
            func(resolver containercontract.Resolver) (*brokenService, error) {
                return &brokenService{}, nil
            },
            container.WithoutTypeRegistration(),
        )
    }

    return newTestRuntime(serviceContainer)
}

func containerCommandNameList(t *testing.T, arguments []string) []string {
    t.Helper()

    rendered, runErr := runDebugCommand(
        &ContainerCommand{},
        newContainerListTestRuntime(10),
        arguments,
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    envelope := containerCommandTestEnvelope{}

    decodeErr := json.Unmarshal([]byte(rendered), &envelope)
    if nil != decodeErr {
        t.Fatalf("failed to decode the rendered envelope: %v, rendered %q", decodeErr, rendered)
    }

    if 10 != envelope.Data.Total {
        t.Fatalf("expected total 10, got %d", envelope.Data.Total)
    }

    name := make([]string, 0, len(envelope.Data.Items))
    for _, item := range envelope.Data.Items {
        name = append(name, item.Name)
    }

    return name
}

func assertContainerNameList(t *testing.T, expected []string, actual []string) {
    t.Helper()

    if len(expected) != len(actual) {
        t.Fatalf("expected %v, got %v", expected, actual)
    }

    for position, value := range expected {
        if value != actual[position] {
            t.Fatalf("expected %v, got %v", expected, actual)
        }
    }
}

func TestContainerCommand_KeepsTheAscendingOrderByDefault(t *testing.T) {
    assertContainerNameList(
        t,
        []string{"service.00", "service.01", "service.02"},
        containerCommandNameList(t, []string{"--format=json", "--limit=3"}),
    )
}

func TestContainerCommand_ReversesTheServiceListForADescendingOrder(t *testing.T) {
    ascending := containerCommandNameList(t, []string{"--format=json", "--order=asc"})
    descending := containerCommandNameList(t, []string{"--format=json", "--order=desc"})

    if len(ascending) != len(descending) {
        t.Fatalf("expected %d items, got %d", len(ascending), len(descending))
    }

    for position, value := range ascending {
        if value != descending[len(descending)-1-position] {
            t.Fatalf("expected the descending order to be the reverse of %v, got %v", ascending, descending)
        }
    }
}

func TestContainerCommand_AppliesTheDescendingOrderBeforeTheWindow(t *testing.T) {
    assertContainerNameList(
        t,
        []string{"service.09", "service.08", "service.07"},
        containerCommandNameList(t, []string{"--format=json", "--order=desc", "--limit=3"}),
    )
}

func TestContainerCommand_WalksEveryServiceExactlyOnceWhenPagingDescending(t *testing.T) {
    seen := map[string]int{}
    offset := 0

    for pageIndex := 0; pageIndex < 10; pageIndex++ {
        name := containerCommandNameList(
            t,
            []string{"--format=json", "--order=desc", "--limit=4", fmt.Sprintf("--offset=%d", offset)},
        )

        if 0 == len(name) {
            break
        }

        for _, value := range name {
            seen[value] = seen[value] + 1
        }

        offset = offset + 4
    }

    if 10 != len(seen) {
        t.Fatalf("expected paging to walk 10 distinct services, got %d", len(seen))
    }

    for value, count := range seen {
        if 1 != count {
            t.Fatalf("service %q was returned %d times while paging descending", value, count)
        }
    }
}

/* the reproduction below cannot run inside the test binary: a self-referential error context sends the redaction walk into unbounded recursion, and the resulting stack overflow is a runtime fatal error that no recover contains, so it takes down whatever process runs it. TestMain therefore doubles as a probe entry point — when the environment variable names a probe the process runs that reproduction and exits instead of running the suite — and the tests re-execute this binary, bound it with a deadline and assert on the child's exit status. */
const errorContextProbeEnvironmentVariable = "MELODY_DEBUG_ERROR_CONTEXT_PROBE"

func TestMain(mainInstance *testing.M) {
    probeName := os.Getenv(errorContextProbeEnvironmentVariable)
    if "" != probeName {
        runErrorContextProbe(probeName)

        os.Exit(0)
    }

    os.Exit(mainInstance.Run())
}

func fullVerbosityTableOption() output.Option {
    return output.Option{
        Format:         output.FormatTable,
        VerbosityLevel: 3,
    }
}

/* newCyclicErrorContext builds the context a producer creates by parking a reference to the context inside itself. The self-reference is stored as a plain map[string]any because that is what the walk descends into; the named contract type is converted at the top of resolveErrorContextJson. */
func newCyclicErrorContext() exceptioncontract.Context {
    payload := map[string]any{
        "serviceName": "broken.service",
    }
    payload["self"] = payload

    return exceptioncontract.Context{
        "payload": payload,
    }
}

func newCyclicSliceErrorContext() exceptioncontract.Context {
    payload := make([]any, 2)
    payload[0] = "first"
    payload[1] = payload

    return exceptioncontract.Context{
        "payload": payload,
    }
}

func runErrorContextProbe(probeName string) {
    switch probeName {
    case "cyclicMapContext":
        resolveErrorContextJson(exception.NewError("boom", newCyclicErrorContext(), nil), fullVerbosityTableOption())

    case "cyclicSliceContext":
        resolveErrorContextJson(exception.NewError("boom", newCyclicSliceErrorContext(), nil), fullVerbosityTableOption())

    case "cyclicNamedMapContext":
        resolveErrorContextJson(exception.NewError("boom", newCyclicNamedMapErrorContext(), nil), fullVerbosityTableOption())

    case "cyclicNamedSliceContext":
        resolveErrorContextJson(exception.NewError("boom", newCyclicNamedSliceErrorContext(), nil), fullVerbosityTableOption())

    default:
        os.Exit(97)
    }
}

func assertErrorContextProbeExitsCleanly(t *testing.T, probeName string, budget time.Duration) {
    t.Helper()

    binaryPath, executableErr := os.Executable()
    if nil != executableErr {
        t.Fatalf("could not locate the test binary to re-execute: %v", executableErr)
    }

    ctx, cancel := context.WithTimeout(context.Background(), budget)
    defer cancel()

    command := exec.CommandContext(ctx, binaryPath)
    command.Env = append(os.Environ(), errorContextProbeEnvironmentVariable+"="+probeName)

    combinedOutput, runErr := command.CombinedOutput()

    if nil != ctx.Err() {
        t.Fatalf("the %s probe was still running after %s and had to be killed, so the walk does not terminate; output: %s", probeName, budget, combinedOutput)
    }

    if nil == runErr {
        return
    }

    exitErr := (*exec.ExitError)(nil)
    if true == errors.As(runErr, &exitErr) {
        t.Fatalf("the %s probe died with exit status %d instead of returning; output: %s", probeName, exitErr.ExitCode(), combinedOutput)
    }

    t.Fatalf("could not run the %s probe: %v; output: %s", probeName, runErr, combinedOutput)
}

/* a service-resolution error whose context holds itself must not take the debug:container command — and with it the process — down; the redaction runs before json.Marshal, deliberately, so encoding/json's own cycle detector never gets the chance to turn this into a clean error */
func TestResolveErrorContextJson_SelfReferentialMapContextTerminates(t *testing.T) {
    assertErrorContextProbeExitsCleanly(t, "cyclicMapContext", 30*time.Second)
}

/* the same loop closed through a slice element rather than a map key */
func TestResolveErrorContextJson_SelfReferentialSliceContextTerminates(t *testing.T) {
    assertErrorContextProbeExitsCleanly(t, "cyclicSliceContext", 30*time.Second)
}

/* the guarded rendering must still be usable: the surviving keys are printed and the point where the loop closed is named, rather than the whole context being dropped */
func TestResolveErrorContextJson_RendersTheCycleAsAMarker(t *testing.T) {
    result := resolveErrorContextJson(exception.NewError("boom", newCyclicErrorContext(), nil), fullVerbosityTableOption())

    /* the rendered context is JSON, where encoding/json escapes the marker's angle brackets, so the marker is looked for in the form the encoder actually writes */
    encodedMarker, markerErr := json.Marshal(errorContextCycleMarker)
    if nil != markerErr {
        t.Fatalf("could not encode the cycle marker: %v", markerErr)
    }

    if false == strings.Contains(result, string(encodedMarker)) {
        t.Fatalf("expected the closed loop to be rendered as %s, got %q", encodedMarker, result)
    }
    if false == strings.Contains(result, "broken.service") {
        t.Fatalf("expected the keys around the loop to survive the guard, got %q", result)
    }
}

/* the guard is scoped to the current path, not to everything the walk has ever seen: one map handed to two sibling keys is not a cycle, and marking the second occurrence would be a silent wrong answer about the operator's own data */
func TestSanitizeErrorContextValue_SharedSiblingContainerIsNotACycle(t *testing.T) {
    shared := map[string]any{
        "serviceName": "shared.service",
    }

    sanitized := sanitizeErrorContextValue(map[string]any{
        "first":  shared,
        "second": shared,
    })

    encoded, marshalErr := json.Marshal(sanitized)
    if nil != marshalErr {
        t.Fatalf("the sanitized context must stay marshalable: %v", marshalErr)
    }

    /* the encoder escapes the marker's angle brackets, so the marker is looked for in the form it actually writes */
    encodedMarker, markerErr := json.Marshal(errorContextCycleMarker)
    if nil != markerErr {
        t.Fatalf("could not encode the cycle marker: %v", markerErr)
    }

    if true == strings.Contains(string(encoded), string(encodedMarker)) {
        t.Fatalf("a container reached twice through sibling keys is not a cycle, got %s", encoded)
    }
    if 2 != strings.Count(string(encoded), "shared.service") {
        t.Fatalf("expected both sibling keys to render the shared container, got %s", encoded)
    }
}

/* The cycle guard above answers a context that holds itself. It says nothing about one that is merely very deep, and until this bound nothing else did either: the walk descended until the goroutine stack was gone, which is `fatal error: stack overflow` — not a panic, so no recover in the command layer turns it into a reported failure and the process dies rendering a debug page. Measured with the stack capped at 16 MiB it took some five hundred thousand levels; the production cap of a gigabyte scales that up rather than removing it.

The depth used here is one past the bound rather than half a million, because what has to be pinned is the refusal, not the machine's stack size — a test that needs a real overflow to fail can only fail by killing the test binary. */
func TestSanitizeErrorContextValue_RefusesToDescendPastTheDepthBound(t *testing.T) {
    deepest := map[string]any{"leaf": "value"}

    current := deepest
    for index := 0; index < maximumErrorContextDepth+1; index = index + 1 {
        current = map[string]any{"next": current}
    }

    sanitized, isMap := sanitizeErrorContextValue(current).(map[string]any)
    if false == isMap {
        t.Fatalf("expected the sanitized context to stay a map, got %T", sanitizeErrorContextValue(current))
    }

    for index := 0; index < maximumErrorContextDepth-1; index = index + 1 {
        next, exists := sanitized["next"]
        if false == exists {
            t.Fatalf("the walk stopped at depth %d, well before the bound", index)
        }

        nextMap, isNextMap := next.(map[string]any)
        if false == isNextMap {
            t.Fatalf("the walk stopped at depth %d with %v, well before the bound", index, next)
        }

        sanitized = nextMap
    }

    if errorContextDepthMarker != sanitized["next"] {
        t.Fatalf("expected the subtree past the bound to be replaced with %q, got %v", errorContextDepthMarker, sanitized["next"])
    }
}

/* the control: an ordinary error context, which nests a handful of levels, must come through untouched. A bound that truncated real contexts would trade a rare fatal error for an everyday loss of the information the page exists to show. */
func TestSanitizeErrorContextValue_LeavesAnOrdinaryContextIntact(t *testing.T) {
    sanitized, isMap := sanitizeErrorContextValue(map[string]any{
        "service": "app.pool",
        "cause": map[string]any{
            "driver": "pgsql",
            "attempts": []any{
                map[string]any{"at": "1", "error": "refused"},
            },
        },
    }).(map[string]any)
    if false == isMap {
        t.Fatal("expected a map")
    }

    cause := sanitized["cause"].(map[string]any)
    attempts := cause["attempts"].([]any)
    attempt := attempts[0].(map[string]any)

    if "refused" != attempt["error"] {
        t.Fatalf("an ordinary context did not survive the walk: %v", sanitized)
    }
}

/* newCyclicNamedMapErrorContext parks the self-reference inside a value typed as the framework's own defined context type, the shape that used to slip past the tracked walk entirely: the cycle survived into json.Marshal, whose cycle error routed it to the fmt fallback that has no cycle detection at all */
func newCyclicNamedMapErrorContext() exceptioncontract.Context {
    inner := exceptioncontract.Context{
        "serviceName": "broken.service",
    }
    inner["self"] = inner

    return exceptioncontract.Context{
        "payload": inner,
    }
}

/* a cycle carried by a nested defined-type map must terminate exactly like one carried by a plain map: pre-guard it was a fatal stack overflow inside the fmt fallback, which no recover reaches */
func TestResolveErrorContextJson_SelfReferentialNamedMapContextTerminates(t *testing.T) {
    assertErrorContextProbeExitsCleanly(t, "cyclicNamedMapContext", 30*time.Second)
}

func TestResolveErrorContextJson_RendersTheNamedMapCycleAsAMarker(t *testing.T) {
    result := resolveErrorContextJson(exception.NewError("boom", newCyclicNamedMapErrorContext(), nil), fullVerbosityTableOption())

    encodedMarker, markerErr := json.Marshal(errorContextCycleMarker)
    if nil != markerErr {
        t.Fatalf("could not encode the cycle marker: %v", markerErr)
    }

    if false == strings.Contains(result, string(encodedMarker)) {
        t.Fatalf("expected the closed loop to be rendered as %s, got %q", encodedMarker, result)
    }
    if false == strings.Contains(result, "broken.service") {
        t.Fatalf("expected the keys around the loop to survive the guard, got %q", result)
    }
}

/* the redaction must reach inside a nested defined-type map before the fmt fallback prints it: the assertion on the plain map type failed for the named type, so a marshal failure printed the nested stack in the clear */
func TestResolveErrorContextJson_RedactsInsideANamedNestedMapOnMarshalFailure(t *testing.T) {
    contextValue := exceptioncontract.Context{
        "detail": exceptioncontract.Context{
            "stack": "SECRET_NESTED_STACK",
        },
        "channel": make(chan int), /* not JSON marshalable, so it forces the fmt fallback */
    }

    result := resolveErrorContextJson(
        exception.NewError("boom", contextValue, nil),
        output.Option{
            Format:         output.FormatTable,
            VerbosityLevel: 0,
        },
    )

    if true == strings.Contains(result, "SECRET_NESTED_STACK") {
        t.Fatalf("nested stack value leaked into the fallback output: %q", result)
    }
}

/* the table-cell truncation must not reach the json document: a machine consumer received a cut, unparseable fragment with no sign anything was dropped */
func TestResolveErrorContextJson_DoesNotTruncateTheJsonFormat(t *testing.T) {
    longValue := strings.Repeat("a", 500)
    resolveErr := exception.NewError(
        "boom",
        exceptioncontract.Context{
            "detail": longValue,
        },
        nil,
    )

    jsonResult := resolveErrorContextJson(
        resolveErr,
        output.Option{
            Format:         output.FormatJson,
            VerbosityLevel: 0,
        },
    )

    decoded := map[string]any{}
    if decodeErr := json.Unmarshal([]byte(jsonResult), &decoded); nil != decodeErr {
        t.Fatalf("expected the json format to stay parseable, got %v for %q", decodeErr, jsonResult)
    }
    if longValue != decoded["detail"] {
        t.Fatalf("expected the full value in the json format")
    }

    tableResult := resolveErrorContextJson(
        resolveErr,
        output.Option{
            Format:         output.FormatTable,
            VerbosityLevel: 0,
        },
    )
    if false == strings.HasSuffix(tableResult, "...") {
        t.Fatalf("expected the table format to stay truncated, got %q", tableResult)
    }
}

/* the report names why the build failed, not only that it did: the message of a melody error is its message alone, and the causes below it used to reach neither the table nor the json */
func TestContainerCommand_ReportsTheCauseChainOfAFailedBuild(t *testing.T) {
    serviceContainer := container.NewContainer()
    serviceContainer.MustRegister(
        "broken.service.name",
        func(resolver containercontract.Resolver) (*brokenService, error) {
            return nil, exception.NewError(
                "service build failed",
                nil,
                errors.New("connection refused"),
            )
        },
    )

    runtimeInstance := newTestRuntime(serviceContainer)

    rendered, _ := runDebugCommand(
        &ContainerCommand{},
        runtimeInstance,
        []string{"--format=json", "broken.service.name"},
    )

    decoded := struct {
        Data struct {
            ErrorCauseChain []string `json:"errorCauseChain"`
        } `json:"data"`
        Error struct {
            Cause struct {
                Details map[string]any `json:"details"`
            } `json:"cause"`
        } `json:"error"`
    }{}
    if decodeErr := json.Unmarshal([]byte(rendered), &decoded); nil != decodeErr {
        t.Fatalf("failed to decode the rendered envelope: %v, rendered %q", decodeErr, rendered)
    }

    if 1 != len(decoded.Data.ErrorCauseChain) || "connection refused" != decoded.Data.ErrorCauseChain[0] {
        t.Fatalf("expected the cause chain in the details, got %v", decoded.Data.ErrorCauseChain)
    }

    if nil == decoded.Error.Cause.Details["causeChain"] {
        t.Fatalf("expected the cause chain on the envelope error, got %v", decoded.Error.Cause.Details)
    }

    tableRendered, _ := runDebugCommand(
        &ContainerCommand{},
        newTestRuntime(serviceContainer),
        []string{"broken.service.name"},
    )

    if false == strings.Contains(tableRendered, "caused by") || false == strings.Contains(tableRendered, "connection refused") {
        t.Fatalf("expected the cause line in the table, got %q", tableRendered)
    }

    /* the list view limits error lines by verbosity, so the cause line needs the raised level the operator would use to read a failure; the sweep itself is opt-in, since a bare listing no longer builds */
    listTableRendered, _ := runDebugCommand(
        &ContainerCommand{},
        newTestRuntime(serviceContainer),
        []string{"--verbosity=2", "--build"},
    )

    if false == strings.Contains(listTableRendered, "caused by: connection refused") {
        t.Fatalf("expected the cause line in the list table, got %q", listTableRendered)
    }
}

/* the context is read through the ContextProvider contract: an HttpException in the resolution chain used to contribute nothing */
func TestResolveErrorContextJson_ReadsAnHttpExceptionContext(t *testing.T) {
    httpException := exception.NewHttpException(503, "backend down")
    httpException.SetContextValue("backend", "redis")

    result := resolveErrorContextJson(httpException, fullVerbosityTableOption())

    if false == strings.Contains(result, "redis") {
        t.Fatalf("expected the http exception context, got %q", result)
    }
}

/* the shown count precedes the ok/error split so the split reads as scoped to it: only the windowed services are resolved, and only under --build */
func TestContainerCommand_ScopesTheSummarySplitToTheShownWindow(t *testing.T) {
    rendered, runErr := runDebugCommand(
        &ContainerCommand{},
        newContainerListTestRuntime(10),
        []string{"--limit=2", "--build"},
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    if false == strings.Contains(rendered, "SERVICES: 10 total | 2 shown | 2 ok | 0 error") {
        t.Fatalf("expected the shown count before the split, got %q", rendered)
    }
}

/* namedAnyList is the slice twin of the named-map bypass: a defined type with underlying []any */
type namedAnyList []any

func newCyclicNamedSliceErrorContext() exceptioncontract.Context {
    payload := make(namedAnyList, 2)
    payload[0] = "first"
    payload[1] = payload

    return exceptioncontract.Context{
        "payload": payload,
    }
}

/* the same defined-type bypass closed through a slice element rather than a map key */
func TestResolveErrorContextJson_SelfReferentialNamedSliceContextTerminates(t *testing.T) {
    assertErrorContextProbeExitsCleanly(t, "cyclicNamedSliceContext", 30*time.Second)
}

func TestResolveErrorContextJson_RendersTheNamedSliceCycleAsAMarker(t *testing.T) {
    result := resolveErrorContextJson(exception.NewError("boom", newCyclicNamedSliceErrorContext(), nil), fullVerbosityTableOption())

    encodedMarker, markerErr := json.Marshal(errorContextCycleMarker)
    if nil != markerErr {
        t.Fatalf("could not encode the cycle marker: %v", markerErr)
    }

    if false == strings.Contains(result, string(encodedMarker)) {
        t.Fatalf("expected the closed loop to be rendered as %s, got %q", encodedMarker, result)
    }
}

/* the row builder is what turns one failing service into the lines an operator reads, and no test had entered it. A service that built fine occupies exactly one row; a failing one spreads its error over as many rows as the verbosity allows, with the name and the type printed once so the block reads as one service rather than as several */
func TestBuildContainerServiceTableRows_HealthyService_OccupiesOneRow(t *testing.T) {
    rows := buildContainerServiceTableRows(
        containerServiceListItem{
            Name:     "service.mailer",
            TypeName: "*mailer.Mailer",
        },
        output.Option{Format: output.FormatTable},
    )

    if 1 != len(rows) {
        t.Fatalf("expected one row, got %v", rows)
    }

    if "service.mailer" != rows[0][0] || "*mailer.Mailer" != rows[0][1] || "" != rows[0][2] {
        t.Fatalf("unexpected row %v", rows[0])
    }
}

/* a failing service hides its type behind <error>: the type column reports what was built, and nothing was */
func TestBuildContainerServiceTableRows_FailingService_ReplacesTheTypeAndRepeatsNothing(t *testing.T) {
    rows := buildContainerServiceTableRows(
        containerServiceListItem{
            Name:            "service.mailer",
            TypeName:        "*mailer.Mailer",
            ErrorString:     "dial refused",
            ErrorCauseChain: []string{"connection refused"},
        },
        output.Option{Format: output.FormatTable, VerbosityLevel: 3},
    )

    if 2 > len(rows) {
        t.Fatalf("expected the error to spread over more than one row, got %v", rows)
    }

    if "service.mailer" != rows[0][0] {
        t.Fatalf("expected the name on the first row, got %v", rows[0])
    }

    if "<error>" != rows[0][1] {
        t.Fatalf("expected the type to be replaced on a failing service, got %v", rows[0])
    }

    for index := 1; index < len(rows); index++ {
        if "" != rows[index][0] || "" != rows[index][1] {
            t.Fatalf("expected the continuation rows to repeat neither name nor type, got %v", rows[index])
        }
    }
}

/* the verbosity ladder decides how much of a failure reaches the terminal: the default shows one line, and each level shows more until the third shows everything. A ladder that collapsed to one value would either flood the default output or hide the cause at every verbosity */
func TestErrorMaxLinesForVerbosityLevel_IsAStrictLadder(t *testing.T) {
    expectedList := map[int]int{
        0: 1,
        1: 2,
        2: 4,
        3: 0,
        9: 0,
    }

    for verbosityLevel, expectedMaxLines := range expectedList {
        if expectedMaxLines != errorMaxLinesForVerbosityLevel(verbosityLevel) {
            t.Fatalf(
                "verbosity %d: expected %d lines, got %d",
                verbosityLevel,
                expectedMaxLines,
                errorMaxLinesForVerbosityLevel(verbosityLevel),
            )
        }
    }
}

/* the truncation marks the cut: without the ellipsis the last line shown reads as the whole error, and the operator stops looking exactly where the cause begins */
func TestLimitLinesByVerbosity_MarksTheCutAndKeepsShortErrorsWhole(t *testing.T) {
    lines := []string{"one", "two", "three", "four", "five"}

    limited := limitLinesByVerbosity(lines, 1)

    if 2 != len(limited) {
        t.Fatalf("expected two lines at verbosity 1, got %v", limited)
    }

    if "one" != limited[0] {
        t.Fatalf("expected the first line untouched, got %q", limited[0])
    }

    if false == strings.HasSuffix(limited[1], " ...") {
        t.Fatalf("expected the cut to be marked, got %q", limited[1])
    }

    /* the ellipsis must not appear when nothing was dropped */
    whole := limitLinesByVerbosity([]string{"one"}, 1)

    if 1 != len(whole) || "one" != whole[0] {
        t.Fatalf("expected a short error to be kept whole, got %v", whole)
    }

    unlimited := limitLinesByVerbosity(lines, 3)

    if len(lines) != len(unlimited) {
        t.Fatalf("expected every line at the highest verbosity, got %v", unlimited)
    }
}

/* the drop list is the noise filter of the rendered error context — documented as a filter of noise, not of credentials — and it must catch the spellings the framework itself writes as well as any key that merely contains them; a filter that only matched the exact names would print a producer's "requestStackTrace" in full */
func TestShouldDropErrorContextKey_CatchesEverySpellingAndKeepsTheRest(t *testing.T) {
    droppedKeyList := []string{
        "trace",
        "stack",
        "stackTrace",
        "stacktrace",
        "traceString",
        "trace_string",
        "panicStack",
        "requestStackTrace",
        "SomeTraceValue",
    }

    for _, key := range droppedKeyList {
        if false == shouldDropErrorContextKey(key) {
            t.Fatalf("expected %q to be dropped", key)
        }
    }

    keptKeyList := []string{
        "serviceName",
        "userId",
        "",
        "attempt",
    }

    for _, key := range keptKeyList {
        if true == shouldDropErrorContextKey(key) {
            t.Fatalf("expected %q to be kept", key)
        }
    }
}

/* the cause chain starts one link below the resolution error's own message, because the message alone is what the command already prints; a walk anchored on the error itself would repeat it as its own first cause */
func TestResolveErrorCauseChain_StartsBelowTheErrorItself(t *testing.T) {
    if nil != resolveErrorCauseChain(nil) {
        t.Fatalf("expected no chain for a nil error")
    }

    inner := errors.New("connection refused")

    chain := resolveErrorCauseChain(exception.NewError("could not build the service", nil, inner))

    if 1 != len(chain) {
        t.Fatalf("expected one cause below the message, got %v", chain)
    }

    if "connection refused" != chain[0] {
        t.Fatalf("unexpected cause %q", chain[0])
    }

    if nil != resolveErrorCauseChain(errors.New("no cause below this")) {
        t.Fatalf("expected no chain for an error that wraps nothing")
    }
}


/* the noise filter is a display concern, so full verbosity turns it off: an operator who asks for everything gets the context whole, stack keys included — below that the frames would flood the table, and the drop stands */
func TestResolveErrorContextJson_FullVerbosityShowsTheNoiseKeys(t *testing.T) {
    contextValue := exceptioncontract.Context{
        "stack":       "FULL_STACK_TRACE",
        "serviceName": "app.pool",
    }

    resolveErr := exception.NewError("boom", contextValue, nil)

    fullResult := resolveErrorContextJson(
        resolveErr,
        output.Option{
            Format:         output.FormatTable,
            VerbosityLevel: 3,
        },
    )

    if false == strings.Contains(fullResult, "FULL_STACK_TRACE") {
        t.Fatalf("expected -vvv to show the stack key, got %q", fullResult)
    }

    plainResult := resolveErrorContextJson(
        resolveErr,
        output.Option{
            Format:         output.FormatTable,
            VerbosityLevel: 2,
        },
    )

    if true == strings.Contains(plainResult, "FULL_STACK_TRACE") {
        t.Fatalf("expected the drop to stand below full verbosity, got %q", plainResult)
    }
}

/* the default listing groups the two lifetimes into their own blocks — the user's contract: scoped services read as a family, not as a column — and a scoped registration stops being invisible to the listing */
func TestContainerCommand_DefaultListingGroupsTheLifetimes(t *testing.T) {
    serviceContainer := container.NewContainer()

    serviceContainer.MustRegister(
        "grouped.container.service",
        func(resolver containercontract.Resolver) (string, error) {
            return "value", nil
        },
    )

    serviceContainer.MustRegisterScoped(
        "grouped.scoped.service",
        func(resolver containercontract.Resolver) (int, error) {
            return 7, nil
        },
    )

    rendered, runErr := runDebugCommand(
        &ContainerCommand{},
        newTestRuntime(serviceContainer),
        []string{"--format=table", "--table-width=400"},
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    containerRow := debugTableBlockRow(rendered, "SERVICES (CONTAINER)")
    scopedRow := debugTableBlockRow(rendered, "SERVICES (SCOPED)")

    if 1 != len(containerRow) || "grouped.container.service" != containerRow[0][0] {
        t.Fatalf("expected the container block with its service, got %v in %q", containerRow, rendered)
    }

    if 1 != len(scopedRow) || "grouped.scoped.service" != scopedRow[0][0] {
        t.Fatalf("expected the scoped block with its service, got %v in %q", scopedRow, rendered)
    }

    if "registered" != containerRow[0][1] || "string" != containerRow[0][2] {
        t.Fatalf("expected the unbuilt state and the declared type, got %v", containerRow[0])
    }
}

/* a scoped name asked for by argument builds through the run's own scope — the scope a console command's services live in — and reports its lifetime; it used to answer debug.notFound, the exact failure the diagnosis comment promises to prevent */
func TestContainerCommand_SingleScopedServiceResolvesThroughTheRunScope(t *testing.T) {
    serviceContainer := container.NewContainer()

    serviceContainer.MustRegisterScoped(
        "single.scoped.service",
        func(resolver containercontract.Resolver) (string, error) {
            return "scoped value", nil
        },
    )

    rendered, runErr := runDebugCommand(
        &ContainerCommand{},
        newTestRuntime(serviceContainer),
        []string{"--format=json", "single.scoped.service"},
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    if true == strings.Contains(rendered, "debug.notFound") {
        t.Fatalf("expected the scoped service to resolve, got %q", rendered)
    }

    if false == strings.Contains(rendered, "\"lifetime\": \"scoped\"") {
        t.Fatalf("expected the scoped lifetime in the details, got %q", rendered)
    }
}

/* a scoped name whose provider fails is a wiring problem, not a missing registration: the diagnosis reads both lifetimes before it says notFound */
func TestContainerCommand_AFailingScopedServiceIsNotReportedAsMissing(t *testing.T) {
    serviceContainer := container.NewContainer()

    serviceContainer.MustRegisterScoped(
        "failing.scoped.service",
        func(resolver containercontract.Resolver) (*brokenService, error) {
            return nil, exception.NewError("scoped dependency unavailable", nil, nil)
        },
    )

    rendered, _ := runDebugCommand(
        &ContainerCommand{},
        newTestRuntime(serviceContainer),
        []string{"--format=json", "failing.scoped.service"},
    )

    if false == strings.Contains(rendered, "debug.buildFailed") {
        t.Fatalf("expected the scoped failure to be diagnosed as buildFailed, got %q", rendered)
    }

    if true == strings.Contains(rendered, "debug.notFound") {
        t.Fatalf("expected the present registration not to be reported as missing, got %q", rendered)
    }
}

/* a substituted container without the descriptions door keeps a name listing, with the limitation named instead of silently narrowed */
type nameOnlyTestContainer struct {
    containercontract.Container
}

func TestContainerCommand_ListsNamesWithAWarningWithoutTheDescriptionsDoor(t *testing.T) {
    inner := container.NewContainer()
    inner.MustRegister(
        "nameonly.service",
        func(resolver containercontract.Resolver) (string, error) {
            return "value", nil
        },
    )

    rendered, runErr := runDebugCommand(
        &ContainerCommand{},
        newTestRuntime(&nameOnlyTestContainer{Container: inner}),
        []string{"--format=json"},
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    if false == strings.Contains(rendered, "debug.noDescriptions") {
        t.Fatalf("expected the limitation warning, got %q", rendered)
    }

    if false == strings.Contains(rendered, "nameonly.service") {
        t.Fatalf("expected the name listing to survive, got %q", rendered)
    }
}

/* the default listing is a description: no provider runs, and the sweep is what the explicit flag buys */
func TestContainerCommand_DefaultListingRunsNoProvider(t *testing.T) {
    buildCount := 0

    serviceContainer := container.NewContainer()
    serviceContainer.MustRegister(
        "counted.service",
        func(resolver containercontract.Resolver) (string, error) {
            buildCount = buildCount + 1

            return "built", nil
        },
    )

    runtimeInstance := newTestRuntime(serviceContainer)

    if _, listErr := runDebugCommand(&ContainerCommand{}, runtimeInstance, []string{"--format=json"}); nil != listErr {
        t.Fatalf("expected no error, got %v", listErr)
    }

    if 0 != buildCount {
        t.Fatalf("expected the bare listing to run no provider, ran %d times", buildCount)
    }

    if _, buildErr := runDebugCommand(&ContainerCommand{}, runtimeInstance, []string{"--format=json", "--build"}); nil != buildErr {
        t.Fatalf("expected no error, got %v", buildErr)
    }

    if 1 != buildCount {
        t.Fatalf("expected the sweep to run the provider once, ran %d times", buildCount)
    }
}
