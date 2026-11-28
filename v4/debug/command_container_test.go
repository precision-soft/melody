package debug

import (
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "os"
    "os/exec"
    "slices"
    "strings"
    "testing"
    "time"
    "unicode/utf8"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/cli/output"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
)

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

func TestWrapFixedWidth_KeepsRunesIntactAtBoundary(t *testing.T) {
    /* the leading ASCII byte puts every 2 byte rune on an odd offset, so the even width lands mid rune */
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

func TestResolveErrorContextJson_RedactsStackOnMarshalFailure(t *testing.T) {
    contextValue := exceptioncontract.Context{
        "stack":      "SECRET_STACK_TRACE",
        "panicStack": "SECRET_PANIC_STACK",
        "channel":    make(chan int), /* not JSON marshalable, so it forces the fmt fallback */
    }

    resolveErr := exception.NewError("boom", contextValue, nil)

    result := resolveErrorContextJson(
        resolveErr,
        output.Options{
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
    runtimeInstance := newTestRuntime(container.NewContainer(container.ContainerConfig{}))

    rendered, runErr := runContainerCommand(runtimeInstance,
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
            serviceContainer := container.NewContainer(container.ContainerConfig{})
            serviceContainer.MustRegister("broken.service.name", provider, container.RegisterOptions{})

            runtimeInstance := newTestRuntime(serviceContainer)

            rendered, runErr := runContainerCommand(runtimeInstance,
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
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    serviceContainer.MustRegister(
        "working.service.name",
        func(resolver containercontract.Resolver) (*brokenService, error) {
            return &brokenService{}, nil
        },
        container.RegisterOptions{},
    )

    runtimeInstance := newTestRuntime(serviceContainer)

    _, runErr := runContainerCommand(runtimeInstance,
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
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    for index := 0; index < serviceCount; index++ {
        serviceContainer.MustRegister(
            fmt.Sprintf("service.%02d", index),
            func(resolver containercontract.Resolver) (*brokenService, error) {
                return &brokenService{}, nil
            },
            container.RegisterOptions{TypeRegistrationDisabled: true},
        )
    }

    return newTestRuntime(serviceContainer)
}

func containerCommandNameList(t *testing.T, arguments []string) []string {
    t.Helper()

    rendered, runErr := runContainerCommand(newContainerListTestRuntime(10),
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

/* the cyclic context probe runs in a child process: its stack overflow is a fatal error no recover contains */
const errorContextProbeEnvironmentVariable = "MELODY_DEBUG_ERROR_CONTEXT_PROBE"

func TestMain(mainInstance *testing.M) {
    probeName := os.Getenv(errorContextProbeEnvironmentVariable)
    if "" != probeName {
        runErrorContextProbe(probeName)

        os.Exit(0)
    }

    os.Exit(mainInstance.Run())
}

func fullVerbosityTableOptions() output.Options {
    return output.Options{
        Format:         output.FormatTable,
        VerbosityLevel: 3,
    }
}

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
        resolveErrorContextJson(exception.NewError("boom", newCyclicErrorContext(), nil), fullVerbosityTableOptions())

    case "cyclicSliceContext":
        resolveErrorContextJson(exception.NewError("boom", newCyclicSliceErrorContext(), nil), fullVerbosityTableOptions())

    case "cyclicNamedMapContext":
        resolveErrorContextJson(exception.NewError("boom", newCyclicNamedMapErrorContext(), nil), fullVerbosityTableOptions())

    case "cyclicNamedSliceContext":
        resolveErrorContextJson(exception.NewError("boom", newCyclicNamedSliceErrorContext(), nil), fullVerbosityTableOptions())

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

func TestResolveErrorContextJson_SelfReferentialMapContextTerminates(t *testing.T) {
    assertErrorContextProbeExitsCleanly(t, "cyclicMapContext", 30*time.Second)
}

func TestResolveErrorContextJson_SelfReferentialSliceContextTerminates(t *testing.T) {
    assertErrorContextProbeExitsCleanly(t, "cyclicSliceContext", 30*time.Second)
}

func TestResolveErrorContextJson_RendersTheCycleAsAMarker(t *testing.T) {
    result := resolveErrorContextJson(exception.NewError("boom", newCyclicErrorContext(), nil), fullVerbosityTableOptions())

    /* encoding/json escapes the marker's angle brackets */
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

    /* encoding/json escapes the marker's angle brackets */
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

func newCyclicNamedMapErrorContext() exceptioncontract.Context {
    inner := exceptioncontract.Context{
        "serviceName": "broken.service",
    }
    inner["self"] = inner

    return exceptioncontract.Context{
        "payload": inner,
    }
}

func TestResolveErrorContextJson_SelfReferentialNamedMapContextTerminates(t *testing.T) {
    assertErrorContextProbeExitsCleanly(t, "cyclicNamedMapContext", 30*time.Second)
}

func TestResolveErrorContextJson_RendersTheNamedMapCycleAsAMarker(t *testing.T) {
    result := resolveErrorContextJson(exception.NewError("boom", newCyclicNamedMapErrorContext(), nil), fullVerbosityTableOptions())

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

func TestResolveErrorContextJson_RedactsInsideANamedNestedMapOnMarshalFailure(t *testing.T) {
    contextValue := exceptioncontract.Context{
        "detail": exceptioncontract.Context{
            "stack": "SECRET_NESTED_STACK",
        },
        "channel": make(chan int), /* not JSON marshalable, so it forces the fmt fallback */
    }

    result := resolveErrorContextJson(
        exception.NewError("boom", contextValue, nil),
        output.Options{
            Format:         output.FormatTable,
            VerbosityLevel: 0,
        },
    )

    if true == strings.Contains(result, "SECRET_NESTED_STACK") {
        t.Fatalf("nested stack value leaked into the fallback output: %q", result)
    }
}

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
        output.Options{
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
        output.Options{
            Format:         output.FormatTable,
            VerbosityLevel: 0,
        },
    )
    if false == strings.HasSuffix(tableResult, "...") {
        t.Fatalf("expected the table format to stay truncated, got %q", tableResult)
    }
}

func TestContainerCommand_ReportsTheCauseChainOfAFailedBuild(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    serviceContainer.MustRegister(
        "broken.service.name",
        func(resolver containercontract.Resolver) (*brokenService, error) {
            return nil, exception.NewError(
                "service build failed",
                nil,
                errors.New("connection refused"),
            )
        },
        container.RegisterOptions{},
    )

    runtimeInstance := newTestRuntime(serviceContainer)

    rendered, _ := runContainerCommand(runtimeInstance,
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

    tableRendered, _ := runContainerCommand(newTestRuntime(serviceContainer),
        []string{"broken.service.name"},
    )

    if false == strings.Contains(tableRendered, "caused by") || false == strings.Contains(tableRendered, "connection refused") {
        t.Fatalf("expected the cause line in the table, got %q", tableRendered)
    }

    listTableRendered, _ := runContainerCommand(newTestRuntime(serviceContainer),
        []string{"--build"},
    )

    if false == strings.Contains(listTableRendered, "caused by: connection refused") {
        t.Fatalf("expected the cause line in the list table at the default verbosity, got %q", listTableRendered)
    }
}

func TestLimitErrorLinesByVerbosity_CutsTheMessageAndTheContextAndKeepsEveryCause(t *testing.T) {
    message := []string{"build failed", "second line of the message"}
    causes := []string{"caused by: dial refused", "caused by: connection refused"}
    contextLines := []string{"{\"host\":\"redis\"}"}

    lines := limitErrorLinesByVerbosity(message, causes, contextLines, 0)

    expected := []string{"build failed", "caused by: dial refused", "caused by: connection refused ..."}
    if false == slices.Equal(expected, lines) {
        t.Fatalf("expected %q at the default verbosity, got %q", expected, lines)
    }

    lines = limitErrorLinesByVerbosity(message, causes, contextLines, 1)

    expected = []string{"build failed", "second line of the message", "caused by: dial refused", "caused by: connection refused ..."}
    if false == slices.Equal(expected, lines) {
        t.Fatalf("expected the whole message and the context cut at verbosity one, got %q", lines)
    }

    lines = limitErrorLinesByVerbosity(message, causes, contextLines, 3)

    expected = []string{"build failed", "second line of the message", "caused by: dial refused", "caused by: connection refused", "{\"host\":\"redis\"}"}
    if false == slices.Equal(expected, lines) {
        t.Fatalf("expected every line at the highest verbosity, got %q", lines)
    }

    whole := limitErrorLinesByVerbosity([]string{"build failed"}, causes, nil, 0)
    if false == slices.Equal([]string{"build failed", "caused by: dial refused", "caused by: connection refused"}, whole) {
        t.Fatalf("expected no cut marker when nothing was left out, got %q", whole)
    }
}

func TestResolveErrorContextJson_ReadsAnHttpExceptionContext(t *testing.T) {
    httpException := exception.NewHttpException(503, "backend down")
    httpException.SetContextValue("backend", "redis")

    result := resolveErrorContextJson(httpException, fullVerbosityTableOptions())

    if false == strings.Contains(result, "redis") {
        t.Fatalf("expected the http exception context, got %q", result)
    }
}

func TestResolveErrorContextJson_ReadsTheFirstContextBelowAnEmptyProvider(t *testing.T) {
    inner := exception.NewError("dial", exceptioncontract.Context{"host": "redis"}, nil)

    result := resolveErrorContextJson(exception.NewHttpError(503, exception.HttpErrorConfig{Message: "backend down", Cause: inner}), fullVerbosityTableOptions())

    if "{\"host\":\"redis\"}" != result {
        t.Fatalf("expected the inner context below the empty provider, got %q", result)
    }

    outer := exception.NewHttpError(503, exception.HttpErrorConfig{Message: "backend down", Cause: inner})
    outer.SetContextValue("outer", "yes")

    result = resolveErrorContextJson(outer, fullVerbosityTableOptions())

    if "{\"outer\":\"yes\"}" != result {
        t.Fatalf("expected the nearest context that has keys to win, got %q", result)
    }
}

func TestContainerCommand_ScopesTheSummarySplitToTheShownWindow(t *testing.T) {
    rendered, runErr := runContainerCommand(newContainerListTestRuntime(10),
        []string{"--limit=2", "--build"},
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    if false == strings.Contains(rendered, "SERVICES: 10 total | 2 shown | 2 ok | 0 error") {
        t.Fatalf("expected the shown count before the split, got %q", rendered)
    }
}

type namedAnyList []any

func newCyclicNamedSliceErrorContext() exceptioncontract.Context {
    payload := make(namedAnyList, 2)
    payload[0] = "first"
    payload[1] = payload

    return exceptioncontract.Context{
        "payload": payload,
    }
}

func TestResolveErrorContextJson_SelfReferentialNamedSliceContextTerminates(t *testing.T) {
    assertErrorContextProbeExitsCleanly(t, "cyclicNamedSliceContext", 30*time.Second)
}

func TestResolveErrorContextJson_RendersTheNamedSliceCycleAsAMarker(t *testing.T) {
    result := resolveErrorContextJson(exception.NewError("boom", newCyclicNamedSliceErrorContext(), nil), fullVerbosityTableOptions())

    encodedMarker, markerErr := json.Marshal(errorContextCycleMarker)
    if nil != markerErr {
        t.Fatalf("could not encode the cycle marker: %v", markerErr)
    }

    if false == strings.Contains(result, string(encodedMarker)) {
        t.Fatalf("expected the closed loop to be rendered as %s, got %q", encodedMarker, result)
    }
}

func TestBuildContainerServiceTableRows_HealthyService_OccupiesOneRow(t *testing.T) {
    rows := buildContainerServiceTableRows(
        containerServiceListItem{
            Name:     "service.mailer",
            TypeName: "*mailer.Mailer",
        },
        output.Options{Format: output.FormatTable},
    )

    if 1 != len(rows) {
        t.Fatalf("expected one row, got %v", rows)
    }

    if "service.mailer" != rows[0][0] || "*mailer.Mailer" != rows[0][1] || "" != rows[0][2] {
        t.Fatalf("unexpected row %v", rows[0])
    }
}

func TestBuildContainerServiceTableRows_FailingService_ReplacesTheTypeAndRepeatsNothing(t *testing.T) {
    rows := buildContainerServiceTableRows(
        containerServiceListItem{
            Name:            "service.mailer",
            TypeName:        "*mailer.Mailer",
            ErrorString:     "dial refused",
            ErrorCauseChain: []string{"connection refused"},
        },
        output.Options{Format: output.FormatTable, VerbosityLevel: 3},
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

    whole := limitLinesByVerbosity([]string{"one"}, 1)

    if 1 != len(whole) || "one" != whole[0] {
        t.Fatalf("expected a short error to be kept whole, got %v", whole)
    }

    unlimited := limitLinesByVerbosity(lines, 3)

    if len(lines) != len(unlimited) {
        t.Fatalf("expected every line at the highest verbosity, got %v", unlimited)
    }
}

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

    joinedChain := resolveErrorCauseChain(errors.Join(errors.New("first cause"), errors.New("second cause")))
    if 2 != len(joinedChain) || "first cause" != joinedChain[0] || "second cause" != joinedChain[1] {
        t.Fatalf("expected both branches of a joined failure below the head, got %v", joinedChain)
    }
}

func TestResolveErrorContextJson_FullVerbosityShowsTheNoiseKeys(t *testing.T) {
    contextValue := exceptioncontract.Context{
        "stack":       "FULL_STACK_TRACE",
        "serviceName": "app.pool",
    }

    resolveErr := exception.NewError("boom", contextValue, nil)

    fullResult := resolveErrorContextJson(
        resolveErr,
        output.Options{
            Format:         output.FormatTable,
            VerbosityLevel: 3,
        },
    )

    if false == strings.Contains(fullResult, "FULL_STACK_TRACE") {
        t.Fatalf("expected -vvv to show the stack key, got %q", fullResult)
    }

    plainResult := resolveErrorContextJson(
        resolveErr,
        output.Options{
            Format:         output.FormatTable,
            VerbosityLevel: 2,
        },
    )

    if true == strings.Contains(plainResult, "FULL_STACK_TRACE") {
        t.Fatalf("expected the drop to stand below full verbosity, got %q", plainResult)
    }
}

func TestContainerCommand_DefaultListingGroupsTheLifetimes(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    serviceContainer.MustRegister(
        "grouped.container.service",
        func(resolver containercontract.Resolver) (string, error) {
            return "value", nil
        },
        container.RegisterOptions{},
    )

    serviceContainer.MustRegisterScoped(
        "grouped.scoped.service",
        func(resolver containercontract.Resolver) (int, error) {
            return 7, nil
        },
        container.RegisterOptions{},
    )

    rendered, runErr := runContainerCommand(newTestRuntime(serviceContainer),
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

func TestContainerCommand_SingleScopedServiceResolvesThroughTheRunScope(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    serviceContainer.MustRegisterScoped(
        "single.scoped.service",
        func(resolver containercontract.Resolver) (string, error) {
            return "scoped value", nil
        },
        container.RegisterOptions{},
    )

    rendered, runErr := runContainerCommand(newTestRuntime(serviceContainer),
        []string{"--format=json", "single.scoped.service"},
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    if true == strings.Contains(rendered, "debug.notFound") {
        t.Fatalf("expected the scoped service to resolve, got %q", rendered)
    }

    scopedDecoded := struct {
        Data struct {
            Lifetime string `json:"lifetime"`
        } `json:"data"`
    }{}
    if decodeErr := json.Unmarshal([]byte(rendered), &scopedDecoded); nil != decodeErr {
        t.Fatalf("failed to decode the rendered envelope: %v, rendered %q", decodeErr, rendered)
    }

    if "scoped" != scopedDecoded.Data.Lifetime {
        t.Fatalf("expected the scoped lifetime in the details, got %q", rendered)
    }
}

func TestContainerCommand_AFailingScopedServiceIsNotReportedAsMissing(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    serviceContainer.MustRegisterScoped(
        "failing.scoped.service",
        func(resolver containercontract.Resolver) (*brokenService, error) {
            return nil, exception.NewError("scoped dependency unavailable", nil, nil)
        },
        container.RegisterOptions{},
    )

    rendered, _ := runContainerCommand(newTestRuntime(serviceContainer),
        []string{"--format=json", "failing.scoped.service"},
    )

    if false == strings.Contains(rendered, "debug.buildFailed") {
        t.Fatalf("expected the scoped failure to be diagnosed as buildFailed, got %q", rendered)
    }

    if true == strings.Contains(rendered, "debug.notFound") {
        t.Fatalf("expected the present registration not to be reported as missing, got %q", rendered)
    }
}

type nameOnlyTestContainer struct {
    containercontract.Container
}

func TestContainerCommand_ListsNamesWithAWarningWithoutTheDescriptionsDoor(t *testing.T) {
    inner := container.NewContainer(container.ContainerConfig{})
    inner.MustRegister(
        "nameonly.service",
        func(resolver containercontract.Resolver) (string, error) {
            return "value", nil
        },
        container.RegisterOptions{},
    )

    rendered, runErr := runContainerCommand(newTestRuntime(&nameOnlyTestContainer{Container: inner}),
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

func TestContainerCommand_DefaultListingRunsNoProvider(t *testing.T) {
    buildCount := 0

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    serviceContainer.MustRegister(
        "counted.service",
        func(resolver containercontract.Resolver) (string, error) {
            buildCount = buildCount + 1

            return "built", nil
        },
        container.RegisterOptions{},
    )

    runtimeInstance := newTestRuntime(serviceContainer)

    if _, listErr := runContainerCommand(runtimeInstance, []string{"--format=json"}); nil != listErr {
        t.Fatalf("expected no error, got %v", listErr)
    }

    if 0 != buildCount {
        t.Fatalf("expected the bare listing to run no provider, ran %d times", buildCount)
    }

    if _, buildErr := runContainerCommand(runtimeInstance, []string{"--format=json", "--build"}); nil != buildErr {
        t.Fatalf("expected no error, got %v", buildErr)
    }

    if 1 != buildCount {
        t.Fatalf("expected the sweep to run the provider once, ran %d times", buildCount)
    }
}

func TestContainerCommand_BuildSweepReportsItsFailuresInTheEnvelope(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    serviceContainer.MustRegister(
        "sweep.healthy.service",
        func(resolver containercontract.Resolver) (*sweepHealthyService, error) {
            return &sweepHealthyService{}, nil
        },
        container.RegisterOptions{},
    )
    serviceContainer.MustRegister(
        "sweep.broken.service",
        func(resolver containercontract.Resolver) (*sweepBrokenService, error) {
            return nil, exception.NewError("service build failed", nil, errors.New("connection refused"))
        },
        container.RegisterOptions{},
    )

    rendered, runErr := runContainerCommand(newTestRuntime(serviceContainer),
        []string{"--format=json", "--build"},
    )

    if nil == runErr {
        t.Fatalf("expected the sweep to fail the run over a service that does not build, got %q", rendered)
    }

    exitErr := (*exception.ExitError)(nil)
    if false == errors.As(runErr, &exitErr) {
        t.Fatalf("expected the failure to carry the exit code, got %v", runErr)
    }

    if 1 != exitErr.ExitCode() {
        t.Fatalf("expected exit code 1, got %d", exitErr.ExitCode())
    }

    decoded := struct {
        Error struct {
            Code    string         `json:"code"`
            Details map[string]any `json:"details"`
            Cause   struct {
                Message string `json:"message"`
            } `json:"cause"`
        } `json:"error"`
    }{}
    if decodeErr := json.Unmarshal([]byte(rendered), &decoded); nil != decodeErr {
        t.Fatalf("failed to decode the rendered envelope: %v, rendered %q", decodeErr, rendered)
    }

    if "debug.buildFailed" != decoded.Error.Code {
        t.Fatalf("expected the build-failed code, got %q in %q", decoded.Error.Code, rendered)
    }

    if float64(1) != decoded.Error.Details["failedCount"] {
        t.Fatalf("expected the failure count in the details, got %#v", decoded.Error.Details["failedCount"])
    }

    if "service build failed" != decoded.Error.Cause.Message {
        t.Fatalf("expected the first failure as the cause, got %q", decoded.Error.Cause.Message)
    }

    healthyContainer := container.NewContainer(container.ContainerConfig{})
    healthyContainer.MustRegister(
        "sweep.healthy.service",
        func(resolver containercontract.Resolver) (*sweepHealthyService, error) {
            return &sweepHealthyService{}, nil
        },
        container.RegisterOptions{},
    )

    if _, healthyErr := runContainerCommand(newTestRuntime(healthyContainer),
        []string{"--format=json", "--build"},
    ); nil != healthyErr {
        t.Fatalf("expected a sweep without failures to succeed, got %v", healthyErr)
    }
}

type sweepHealthyService struct{}

type sweepBrokenService struct{}

func TestContainerCommand_JsonItemFieldsKeepOneTypeAcrossRows(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    serviceContainer.MustRegister(
        "shape.healthy.service",
        func(resolver containercontract.Resolver) (*sweepHealthyService, error) {
            return &sweepHealthyService{}, nil
        },
        container.RegisterOptions{},
    )
    serviceContainer.MustRegister(
        "shape.broken.service",
        func(resolver containercontract.Resolver) (*sweepBrokenService, error) {
            return nil, exception.NewError("service build failed", nil, errors.New("connection refused"))
        },
        container.RegisterOptions{},
    )

    rendered, _ := runContainerCommand(newTestRuntime(serviceContainer),
        []string{"--format=json", "--build"},
    )

    decoded := struct {
        Data struct {
            Items []struct {
                Name             string    `json:"name"`
                ErrorCauseChain  *[]string `json:"errorCauseChain"`
                ErrorContextJson *string   `json:"errorContextJson"`
            } `json:"items"`
        } `json:"data"`
    }{}
    if decodeErr := json.Unmarshal([]byte(rendered), &decoded); nil != decodeErr {
        t.Fatalf("failed to decode the rendered envelope: %v, rendered %q", decodeErr, rendered)
    }

    if 2 != len(decoded.Data.Items) {
        t.Fatalf("expected both services in the document, got %d in %q", len(decoded.Data.Items), rendered)
    }

    for _, item := range decoded.Data.Items {
        if nil == item.ErrorCauseChain {
            t.Fatalf("%s: expected an array, got a json null in %q", item.Name, rendered)
        }

        if nil == item.ErrorContextJson {
            t.Fatalf("%s: expected a string, got a json null in %q", item.Name, rendered)
        }

        parsed := map[string]any{}
        if parseErr := json.Unmarshal([]byte(*item.ErrorContextJson), &parsed); nil != parseErr {
            t.Fatalf("%s: expected a parseable context document, got %q: %v", item.Name, *item.ErrorContextJson, parseErr)
        }
    }

    plainContainer := container.NewContainer(container.ContainerConfig{})
    plainContainer.MustRegister(
        "shape.plain.service",
        func(resolver containercontract.Resolver) (*sweepBrokenService, error) {
            return nil, errors.New("connection refused")
        },
        container.RegisterOptions{},
    )

    plainTableRendered, _ := runContainerCommand(newTestRuntime(plainContainer),
        []string{"--verbosity=2", "--build", "--table-width=400"},
    )

    if true == strings.Contains(plainTableRendered, "{}") {
        t.Fatalf("expected the table to keep the empty cell for a failure carrying no context, got %q", plainTableRendered)
    }

    plainJsonRendered, _ := runContainerCommand(newTestRuntime(plainContainer),
        []string{"--format=json", "--build"},
    )

    plainJsonDecoded := struct {
        Data struct {
            Items []struct {
                ErrorContextJson *string `json:"errorContextJson"`
            } `json:"items"`
        } `json:"data"`
    }{}
    if decodeErr := json.Unmarshal([]byte(plainJsonRendered), &plainJsonDecoded); nil != decodeErr {
        t.Fatalf("failed to decode the rendered envelope: %v, rendered %q", decodeErr, plainJsonRendered)
    }

    if 1 != len(plainJsonDecoded.Data.Items) {
        t.Fatalf("expected the failing service in the document, got %d in %q", len(plainJsonDecoded.Data.Items), plainJsonRendered)
    }

    if nil == plainJsonDecoded.Data.Items[0].ErrorContextJson || "{}" != *plainJsonDecoded.Data.Items[0].ErrorContextJson {
        t.Fatalf("expected the json document to answer an empty context object, got %q", plainJsonRendered)
    }

    plainDecoded := struct {
        Data struct {
            Items []struct {
                ErrorCauseChain *[]string `json:"errorCauseChain"`
            } `json:"items"`
        } `json:"data"`
    }{}
    if decodeErr := json.Unmarshal([]byte(plainJsonRendered), &plainDecoded); nil != decodeErr {
        t.Fatalf("failed to decode the rendered envelope: %v, rendered %q", decodeErr, plainJsonRendered)
    }

    if 1 != len(plainDecoded.Data.Items) || nil == plainDecoded.Data.Items[0].ErrorCauseChain {
        t.Fatalf("expected an array on a failure carrying no causes, got %q", plainJsonRendered)
    }
}

func TestResolveErrorContextJson_AContextTheEncoderRefusesStaysParseableJson(t *testing.T) {
    resolveErr := exception.NewError(
        "boot failed",
        exceptioncontract.Context{
            "listener": make(chan struct{}),
        },
        errors.New("connection refused"),
    )

    jsonOption := output.Options{Format: output.FormatJson}

    rendered := resolveErrorContextJson(resolveErr, jsonOption)

    parsed := map[string]any{}
    if parseErr := json.Unmarshal([]byte(rendered), &parsed); nil != parseErr {
        t.Fatalf("expected a parseable context document, got %q: %v", rendered, parseErr)
    }

    raw, hasRaw := parsed["raw"].(string)
    if false == hasRaw {
        t.Fatalf("expected the rendering to travel under a key that says what it is, got %q", rendered)
    }

    if false == strings.Contains(raw, "listener") {
        t.Fatalf("expected the rendering to name the culprit, got %q", rendered)
    }

    tableRendered := resolveErrorContextJson(resolveErr, output.Options{Format: output.FormatTable, TableMaxWidth: bound.Of(400)})
    if true == strings.HasPrefix(tableRendered, "{\"raw\"") {
        t.Fatalf("expected the table to keep the bare rendering, got %q", tableRendered)
    }
    if false == strings.Contains(tableRendered, "listener") {
        t.Fatalf("expected the table rendering to name the culprit, got %q", tableRendered)
    }
}

func TestContainerCommand_BuildSweepReachesAScopedRegistrationTheNameListCannotSee(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    serviceContainer.MustRegisterScoped(
        "sweep.scoped.only.service",
        func(resolver containercontract.Resolver) (*sweepHealthyService, error) {
            return &sweepHealthyService{}, nil
        },
        container.RegisterOptions{},
    )

    for _, name := range serviceContainer.Names() {
        if "sweep.scoped.only.service" == name {
            t.Fatalf("the fixture is vacuous: Names() already reports the scoped registration")
        }
    }

    rendered, runErr := runContainerCommand(newTestRuntime(serviceContainer),
        []string{"--format=json", "--build"},
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    if false == strings.Contains(rendered, "sweep.scoped.only.service") {
        t.Fatalf("expected the sweep to list the scoped registration, got %q", rendered)
    }
}

func TestContainerCommand_TheTeardownBlockNamesTheWaveAndTheUnorderedServices(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{TeardownSequential: true})

    serviceContainer.MustRegister(
        "teardown.storage",
        func(resolver containercontract.Resolver) (*teardownProbeStorage, error) {
            return &teardownProbeStorage{}, nil
        },
        container.RegisterOptions{},
    )

    serviceContainer.MustRegister(
        "teardown.holder",
        func(resolver containercontract.Resolver) (*teardownProbeHolder, error) {
            storage, resolveErr := container.FromResolver[*teardownProbeStorage](resolver.Runtime().Context(), resolver, "teardown.storage")
            if nil != resolveErr {
                return nil, resolveErr
            }

            return &teardownProbeHolder{storage: storage}, nil
        },
        container.RegisterOptions{},
    )

    serviceContainer.MustRegister(
        "teardown.unordered",
        func(resolver containercontract.Resolver) (*teardownProbeStorage, error) {
            return &teardownProbeStorage{}, nil
        },
        container.RegisterOptions{TypeRegistrationDisabled: true},
    )

    if _, resolveErr := container.FromResolver[*teardownProbeHolder](t.Context(), serviceContainer, "teardown.holder"); nil != resolveErr {
        t.Fatalf("unexpected resolve error: %v", resolveErr)
    }

    if _, resolveErr := container.FromResolver[*teardownProbeStorage](t.Context(), serviceContainer, "teardown.unordered"); nil != resolveErr {
        t.Fatalf("unexpected resolve error: %v", resolveErr)
    }

    rendered, runErr := runContainerCommand(newTestRuntime(serviceContainer),
        []string{"--format=table", "--table-width=400"},
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    teardownRows := debugTableBlockRow(rendered, "TEARDOWN (SEQUENTIAL)")
    if 0 == len(teardownRows) {
        t.Fatalf("expected the sequential teardown block, got %q", rendered)
    }

    orderingByNode := make(map[string]string, len(teardownRows))
    waveByNode := make(map[string]string, len(teardownRows))

    for _, row := range teardownRows {
        waveByNode[row[1]] = row[0]
        orderingByNode[row[1]] = row[3]
    }

    if "proved" != orderingByNode["service:teardown.holder"] {
        t.Fatalf("expected the resolving service to carry a proved ordering, got %v", orderingByNode)
    }

    if "none" != orderingByNode["service:teardown.unordered"] {
        t.Fatalf("expected the service nothing orders to read none, got %v", orderingByNode)
    }

    if "0" != waveByNode["service:teardown.holder"] || "1" != waveByNode["service:teardown.storage"] {
        t.Fatalf("expected the dependency one wave past its dependent, got %v", waveByNode)
    }
}

type teardownProbeStorage struct{}

func (instance *teardownProbeStorage) CloseWithContext(closeContext context.Context) error {
    return nil
}

type teardownProbeHolder struct {
    storage *teardownProbeStorage
}

func (instance *teardownProbeHolder) CloseWithContext(closeContext context.Context) error {
    return nil
}

type teardownViewStorage struct {
    label string
}

func (instance *teardownViewStorage) CloseWithContext(closeContext context.Context) error {
    return nil
}

type teardownViewHolder struct {
    storage *teardownViewStorage
}

func (instance *teardownViewHolder) CloseWithContext(closeContext context.Context) error {
    return nil
}

func newTeardownViewTestContainer(t *testing.T) containercontract.Container {
    t.Helper()

    serviceContainer := container.NewContainer(container.ContainerConfig{})

    serviceContainer.MustRegister(
        "view.storage",
        func(resolver containercontract.Resolver) (*teardownViewStorage, error) {
            return &teardownViewStorage{label: "storage"}, nil
        },
        container.RegisterOptions{},
    )

    serviceContainer.MustRegister(
        "view.holder",
        func(resolver containercontract.Resolver) (*teardownViewHolder, error) {
            storage, resolveErr := container.FromResolver[*teardownViewStorage](resolver.Runtime().Context(), resolver, "view.storage")
            if nil != resolveErr {
                return nil, resolveErr
            }

            return &teardownViewHolder{storage: storage}, nil
        },
        container.RegisterOptions{},
    )

    serviceContainer.MustRegister(
        "view.unordered",
        func(resolver containercontract.Resolver) (*teardownViewStorage, error) {
            return &teardownViewStorage{label: "unordered"}, nil
        },
        container.RegisterOptions{TypeRegistrationDisabled: true},
    )

    serviceContainer.MustRegister(
        "view.unbuilt",
        func(resolver containercontract.Resolver) (*teardownViewStorage, error) {
            return &teardownViewStorage{label: "unbuilt"}, nil
        },
        container.RegisterOptions{TypeRegistrationDisabled: true},
    )

    if _, resolveErr := container.FromResolver[*teardownViewHolder](t.Context(), serviceContainer, "view.holder"); nil != resolveErr {
        t.Fatalf("unexpected resolve error: %v", resolveErr)
    }

    if _, resolveErr := container.FromResolver[*teardownViewStorage](t.Context(), serviceContainer, "view.unordered"); nil != resolveErr {
        t.Fatalf("unexpected resolve error: %v", resolveErr)
    }

    return serviceContainer
}

func teardownRowsByNode(t *testing.T, rendered string) map[string][]string {
    t.Helper()

    rows := debugTableBlockRow(rendered, "TEARDOWN (DEPENDENCY WAVES)")

    byNode := make(map[string][]string, len(rows))
    for _, row := range rows {
        if 6 != len(row) {
            t.Fatalf("expected six cells per teardown row, got %v", row)
        }

        byNode[row[1]] = row
    }

    return byNode
}

func TestContainerCommand_TheTeardownBlockReadsTheOrderingOnBothSidesOfANode(t *testing.T) {
    rendered, runErr := runContainerCommand(newTestRuntime(newTeardownViewTestContainer(t)),
        []string{"--format=table", "--table-width=400"},
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    byNode := teardownRowsByNode(t, rendered)

    storage, listed := byNode["service:view.storage"]
    if false == listed {
        t.Fatalf("expected the storage in the teardown block, got %q", rendered)
    }

    if "proved" != storage[3] || "service:view.holder" != storage[4] || "" != storage[2] {
        t.Fatalf("expected the storage ordered by the holder that resolved it, closed after it and before nothing, got %v", storage)
    }

    holder := byNode["service:view.holder"]
    if "proved" != holder[3] || "service:view.storage" != holder[2] || "" != holder[4] {
        t.Fatalf("expected the holder closed before its storage and after nothing, got %v", holder)
    }

    unordered := byNode["service:view.unordered"]
    if "none" != unordered[3] || "" != unordered[2] || "" != unordered[4] {
        t.Fatalf("expected the service nothing orders to read none on both sides, got %v", unordered)
    }

    if _, listed := byNode["service:view.unbuilt"]; true == listed {
        t.Fatalf("expected a registration never built to be absent from the plan, got %v", byNode)
    }
}

func TestContainerCommand_TheTeardownBlockRendersOnTheBuildSweep(t *testing.T) {
    rendered, runErr := runContainerCommand(newTestRuntime(newTeardownViewTestContainer(t)),
        []string{"--format=table", "--table-width=400", "--build"},
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    byNode := teardownRowsByNode(t, rendered)

    if 4 != len(byNode) {
        t.Fatalf("expected the sweep, which builds every service, to list all four in the teardown block, got %v", byNode)
    }

    if "none" != byNode["service:view.unbuilt"][3] {
        t.Fatalf("expected the service the sweep built, which resolved nothing, to read none, got %v", byNode["service:view.unbuilt"])
    }
}

func TestContainerCommand_TheTeardownBlockRendersTheOneRowOfASingleService(t *testing.T) {
    rendered, runErr := runContainerCommand(newTestRuntime(newTeardownViewTestContainer(t)),
        []string{"--format=table", "--table-width=400", "view.storage"},
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    byNode := teardownRowsByNode(t, rendered)

    if 1 != len(byNode) {
        t.Fatalf("expected exactly the one row of the service asked about, got %v", byNode)
    }

    storage := byNode["service:view.storage"]
    if "1" != storage[0] || "proved" != storage[3] || "service:view.holder" != storage[4] {
        t.Fatalf("expected the storage in wave one, ordered by its holder, got %v", storage)
    }
}

func TestContainerCommand_TheTeardownBlockKeepsToTheWindowAndTheGlobalWave(t *testing.T) {
    rendered, runErr := runContainerCommand(newTestRuntime(newTeardownViewTestContainer(t)),
        []string{"--format=table", "--table-width=400", "--limit=1", "--offset=1"},
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    listedRows := debugTableBlockRow(rendered, "SERVICES (CONTAINER)")
    if 1 != len(listedRows) {
        t.Fatalf("expected the window to show one service, got %v", listedRows)
    }

    byNode := teardownRowsByNode(t, rendered)

    shownNode := "service:" + listedRows[0][0]
    if 1 != len(byNode) {
        t.Fatalf("expected the teardown block windowed to the one shown service %s, got %v", shownNode, byNode)
    }

    if _, listed := byNode[shownNode]; false == listed {
        t.Fatalf("expected the teardown block to carry the shown service %s, got %v", shownNode, byNode)
    }

    if "service:view.storage" == shownNode && "1" != byNode[shownNode][0] {
        t.Fatalf("expected the wave of the whole plan, not of the window, got %v", byNode[shownNode])
    }
}

type containerCommandTeardownTestEnvelope struct {
    Data struct {
        Items []struct {
            Name     string `json:"name"`
            Teardown *struct {
                Wave         int      `json:"wave"`
                ClosedBefore []string `json:"closedBefore"`
                ClosedAfter  []string `json:"closedAfter"`
                Ordering     string   `json:"ordering"`
            } `json:"teardown"`
        } `json:"items"`
    } `json:"data"`
}

func TestContainerCommand_TheJsonDocumentCarriesTheTeardownBesideEachBuiltService(t *testing.T) {
    rendered, runErr := runContainerCommand(newTestRuntime(newTeardownViewTestContainer(t)),
        []string{"--format=json"},
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    var envelope containerCommandTeardownTestEnvelope
    if unmarshalErr := json.Unmarshal([]byte(rendered), &envelope); nil != unmarshalErr {
        t.Fatalf("expected a json document, got %v over %q", unmarshalErr, rendered)
    }

    byName := make(map[string]*struct {
        Wave         int      `json:"wave"`
        ClosedBefore []string `json:"closedBefore"`
        ClosedAfter  []string `json:"closedAfter"`
        Ordering     string   `json:"ordering"`
    }, len(envelope.Data.Items))

    for _, item := range envelope.Data.Items {
        byName[item.Name] = item.Teardown
    }

    storage := byName["view.storage"]
    if nil == storage || 1 != storage.Wave || "proved" != storage.Ordering || 1 != len(storage.ClosedAfter) || "service:view.holder" != storage.ClosedAfter[0] || 0 != len(storage.ClosedBefore) {
        t.Fatalf("expected the storage in wave one, closed after its holder, got %+v", storage)
    }

    holder := byName["view.holder"]
    if nil == holder || 0 != holder.Wave || 1 != len(holder.ClosedBefore) || "service:view.storage" != holder.ClosedBefore[0] {
        t.Fatalf("expected the holder in wave zero, closed before its storage, got %+v", holder)
    }

    if unordered := byName["view.unordered"]; nil == unordered || "none" != unordered.Ordering {
        t.Fatalf("expected the service nothing orders to read none, got %+v", unordered)
    }

    if nil != byName["view.unbuilt"] {
        t.Fatalf("expected the registration never built to carry no teardown key, got %+v", byName["view.unbuilt"])
    }

    if false == strings.Contains(rendered, `"teardown":{`) || true == strings.Contains(rendered, `"teardown":null`) {
        t.Fatalf("expected the teardown key present on built services and absent, not null, on the rest, got %q", rendered)
    }
}

type containerCommandAliasTestEnvelope struct {
    Data struct {
        Items []struct {
            Name     string `json:"name"`
            Lifetime string `json:"lifetime"`
            Teardown *struct {
                Wave     int    `json:"wave"`
                Node     string `json:"node"`
                Ordering string `json:"ordering"`
            } `json:"teardown"`
        } `json:"items"`
    } `json:"data"`
}

func TestContainerCommand_AnAliasOfOneInstanceCarriesTheTeardownOfTheNodeItWasCollapsedOnto(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    serviceContainer.MustRegister(
        "view.canonical",
        func(_ containercontract.Resolver) (*teardownViewStorage, error) { return &teardownViewStorage{label: "shared"}, nil },
        container.RegisterOptions{TypeRegistrationDisabled: true},
    )

    serviceContainer.MustRegister(
        "view.alias",
        func(resolver containercontract.Resolver) (*teardownViewStorage, error) {
            return container.FromResolver[*teardownViewStorage](resolver.Runtime().Context(), resolver, "view.canonical")
        },
        container.RegisterOptions{TypeRegistrationDisabled: true},
    )

    if _, resolveErr := container.FromResolver[*teardownViewStorage](t.Context(), serviceContainer, "view.alias"); nil != resolveErr {
        t.Fatalf("resolve: %v", resolveErr)
    }

    rendered, runErr := runContainerCommand(newTestRuntime(serviceContainer), []string{"--format=json"})
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    var envelope containerCommandAliasTestEnvelope
    if unmarshalErr := json.Unmarshal([]byte(rendered), &envelope); nil != unmarshalErr {
        t.Fatalf("expected a json document, got %v over %q", unmarshalErr, rendered)
    }

    for _, item := range envelope.Data.Items {
        if nil == item.Teardown {
            t.Fatalf("expected %s to carry the teardown of the instance it names, got no key", item.Name)
        }

        if "service:view.alias" == item.Teardown.Node || "none" != item.Teardown.Ordering {
            t.Fatalf("expected %s to answer with the node the plan collapsed the instance onto, unordered, got %+v", item.Name, item.Teardown)
        }
    }

    rendered, runErr = runContainerCommand(newTestRuntime(serviceContainer), []string{"--format=table", "--limit=1"})
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    rows := debugTableBlockRow(rendered, "TEARDOWN (DEPENDENCY WAVES)")
    if 1 != len(rows) || false == strings.Contains(rows[0][1], "service:view.alias") || false == strings.Contains(rows[0][1], "service:view.canonical") {
        t.Fatalf("expected the one row of the window to name the node and its alias, got %v", rows)
    }
}

func TestContainerCommand_AScopedTwinOfABuiltServiceCarriesNoTeardown(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    serviceContainer.MustRegister(
        "view.shared",
        func(_ containercontract.Resolver) (*teardownViewStorage, error) { return &teardownViewStorage{label: "container"}, nil },
        container.RegisterOptions{TypeRegistrationDisabled: true},
    )

    if _, resolveErr := container.FromResolver[*teardownViewStorage](t.Context(), serviceContainer, "view.shared"); nil != resolveErr {
        t.Fatalf("resolve: %v", resolveErr)
    }

    serviceContainer.MustRegisterScoped(
        "view.shared",
        func(_ containercontract.Resolver) (*teardownViewStorage, error) { return &teardownViewStorage{label: "scoped"}, nil },
        container.RegisterOptions{TypeRegistrationDisabled: true, ReplacesContainerService: true},
    )

    rendered, runErr := runContainerCommand(newTestRuntime(serviceContainer), []string{"--format=json"})
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    var envelope containerCommandAliasTestEnvelope
    if unmarshalErr := json.Unmarshal([]byte(rendered), &envelope); nil != unmarshalErr {
        t.Fatalf("expected a json document, got %v over %q", unmarshalErr, rendered)
    }

    seen := 0

    for _, item := range envelope.Data.Items {
        if "view.shared" != item.Name {
            continue
        }

        seen = seen + 1

        if containercontract.ServiceLifetimeScoped == item.Lifetime && nil != item.Teardown {
            t.Fatalf("expected the scoped twin to carry no teardown, got %+v", item.Teardown)
        }

        if containercontract.ServiceLifetimeContainer == item.Lifetime && nil == item.Teardown {
            t.Fatalf("expected the built container service to keep its teardown")
        }
    }

    if 2 != seen {
        t.Fatalf("expected both twins listed, got %d", seen)
    }
}

func TestContainerCommand_TheTeardownBlockNamesACycleRemainder(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    shared := &teardownViewStorage{label: "shared"}

    serviceContainer.MustRegister(
        "cycle.b",
        func(_ containercontract.Resolver) (*teardownViewStorage, error) { return shared, nil },
        container.RegisterOptions{TypeRegistrationDisabled: true},
    )

    serviceContainer.MustRegister(
        "cycle.a",
        func(resolver containercontract.Resolver) (*teardownViewStorage, error) {
            if _, resolveErr := container.FromResolver[*teardownViewStorage](resolver.Runtime().Context(), resolver, "cycle.b"); nil != resolveErr {
                return nil, resolveErr
            }

            return &teardownViewStorage{label: "a"}, nil
        },
        container.RegisterOptions{TypeRegistrationDisabled: true},
    )

    serviceContainer.MustRegister(
        "cycle.c",
        func(resolver containercontract.Resolver) (*teardownViewStorage, error) {
            if _, resolveErr := container.FromResolver[*teardownViewStorage](resolver.Runtime().Context(), resolver, "cycle.a"); nil != resolveErr {
                return nil, resolveErr
            }

            return shared, nil
        },
        container.RegisterOptions{TypeRegistrationDisabled: true},
    )

    if _, resolveErr := container.FromResolver[*teardownViewStorage](t.Context(), serviceContainer, "cycle.c"); nil != resolveErr {
        t.Fatalf("resolve: %v", resolveErr)
    }

    rendered, runErr := runContainerCommand(newTestRuntime(serviceContainer), []string{"--format=table"})
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    rows := debugTableBlockRow(rendered, "TEARDOWN (DEPENDENCY WAVES)")
    if 2 != len(rows) {
        t.Fatalf("expected the two members of the remainder, got %v", rows)
    }

    for _, row := range rows {
        if "cycle" != row[3] {
            t.Fatalf("expected the ordering of a cycle member to read cycle, got %v", row)
        }
    }
}

type viewPairA struct {
    other *viewPairB
}

func (instance *viewPairA) CloseWithContext(closeContext context.Context) error { return nil }

type viewPairB struct {
    other *viewPairA
}

func (instance *viewPairB) CloseWithContext(closeContext context.Context) error { return nil }

func TestContainerCommand_TheTeardownBlockNamesTheSerialGroup(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    a := &viewPairA{}
    b := &viewPairB{other: a}
    a.other = b

    serviceContainer.MustRegister("pair.a", func(_ containercontract.Resolver) (*viewPairA, error) { return a, nil }, container.RegisterOptions{})
    serviceContainer.MustRegister("pair.b", func(_ containercontract.Resolver) (*viewPairB, error) { return b, nil }, container.RegisterOptions{})
    serviceContainer.MustRegister("pair.alone", func(_ containercontract.Resolver) (*teardownViewStorage, error) { return &teardownViewStorage{label: "alone"}, nil }, container.RegisterOptions{})

    container.MustFromResolver[*viewPairA](t.Context(), serviceContainer, "pair.a")
    container.MustFromResolver[*viewPairB](t.Context(), serviceContainer, "pair.b")
    container.MustFromResolver[*teardownViewStorage](t.Context(), serviceContainer, "pair.alone")

    rendered, runErr := runContainerCommand(newTestRuntime(serviceContainer),
        []string{"--format=table", "--table-width=400"},
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    rows := debugTableBlockRow(rendered, "TEARDOWN (DEPENDENCY WAVES)")

    groupOf := make(map[string]string, len(rows))
    for _, row := range rows {
        if 6 != len(row) {
            t.Fatalf("expected six cells per teardown row, got %v", row)
        }

        groupOf[row[1]] = row[5]
    }

    if "" == groupOf["service:pair.a"] || groupOf["service:pair.a"] != groupOf["service:pair.b"] {
        t.Fatalf("expected the pair to share a group figure, got %v", groupOf)
    }

    if "" != groupOf["service:pair.alone"] {
        t.Fatalf("expected the service in no group to print an empty group cell, got %v", groupOf)
    }

    renderedJson, jsonErr := runContainerCommand(newTestRuntime(serviceContainer),
        []string{"--format=json"},
    )
    if nil != jsonErr {
        t.Fatalf("expected no error, got %v", jsonErr)
    }

    if false == strings.Contains(renderedJson, `"group":1`) || false == strings.Contains(renderedJson, `"group":0`) {
        t.Fatalf("expected the json document to carry the group figure beside each built service, got %q", renderedJson)
    }
}

func TestContainerCommand_TheTeardownBlockNamesATypeAliasByTheTypesOwnString(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    serviceContainer.MustRegister(
        "view.typed",
        func(_ containercontract.Resolver) (*teardownViewStorage, error) { return &teardownViewStorage{label: "typed"}, nil },
        container.RegisterOptions{},
    )

    if _, resolveErr := container.FromResolverByType[*teardownViewStorage](t.Context(), serviceContainer); nil != resolveErr {
        t.Fatalf("resolve by type: %v", resolveErr)
    }

    rendered, runErr := runContainerCommand(newTestRuntime(serviceContainer), []string{"--format=table"})
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    if false == strings.Contains(rendered, "service:view.typed (also type:*debug.teardownViewStorage)") {
        t.Fatalf("expected the type alias to be named by the type's own string, got %q", rendered)
    }

    if true == strings.Contains(rendered, "\x00") || true == strings.Contains(rendered, "precision-soft/melody/v4/debug\x00") {
        t.Fatalf("expected no raw identity key in the block, got %q", rendered)
    }
}

func newTwinRegistrationContainer(t *testing.T) containercontract.Container {
    t.Helper()

    serviceContainer := container.NewContainer(container.ContainerConfig{})

    serviceContainer.MustRegister(
        "view.shared",
        func(_ containercontract.Resolver) (*teardownViewStorage, error) { return &teardownViewStorage{label: "container"}, nil },
        container.RegisterOptions{TypeRegistrationDisabled: true},
    )

    serviceContainer.MustRegister(
        "view.other",
        func(_ containercontract.Resolver) (*teardownViewStorage, error) { return &teardownViewStorage{label: "other"}, nil },
        container.RegisterOptions{TypeRegistrationDisabled: true},
    )

    for _, name := range []string{"view.shared", "view.other"} {
        if _, resolveErr := container.FromResolver[*teardownViewStorage](t.Context(), serviceContainer, name); nil != resolveErr {
            t.Fatalf("resolve %s: %v", name, resolveErr)
        }
    }

    serviceContainer.MustRegisterScoped(
        "view.shared",
        func(_ containercontract.Resolver) (*teardownViewStorage, error) { return &teardownViewStorage{label: "scoped"}, nil },
        container.RegisterOptions{TypeRegistrationDisabled: true, ReplacesContainerService: true},
    )

    return serviceContainer
}

func TestContainerCommand_BuildSweepKeepsTheContainerTwinsTeardownAndCarriesEachLifetime(t *testing.T) {
    rendered, runErr := runContainerCommand(newTestRuntime(newTwinRegistrationContainer(t)), []string{"--build", "--format=json"})
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    var envelope containerCommandAliasTestEnvelope
    if unmarshalErr := json.Unmarshal([]byte(rendered), &envelope); nil != unmarshalErr {
        t.Fatalf("expected a json document, got %v over %q", unmarshalErr, rendered)
    }

    lifetimesSeen := map[string]bool{}

    for _, item := range envelope.Data.Items {
        if "view.shared" != item.Name {
            continue
        }

        lifetimesSeen[item.Lifetime] = true

        if containercontract.ServiceLifetimeContainer == item.Lifetime && nil == item.Teardown {
            t.Fatalf("expected the built container twin to keep its teardown on the --build sweep")
        }

        if containercontract.ServiceLifetimeScoped == item.Lifetime && nil != item.Teardown {
            t.Fatalf("expected the scoped twin to carry no teardown on the --build sweep, got %+v", item.Teardown)
        }
    }

    if false == lifetimesSeen[containercontract.ServiceLifetimeContainer] || false == lifetimesSeen[containercontract.ServiceLifetimeScoped] {
        t.Fatalf("expected the sweep to list the twin under both lifetimes, got %v", lifetimesSeen)
    }
}

func TestContainerCommand_AWindowHoldingOnlyTheScopedTwinRendersNoTeardownRow(t *testing.T) {
    for _, arguments := range [][]string{
        {"--format=table", "--limit=1", "--offset=2"},
        {"--build", "--format=table", "--limit=1", "--offset=2"},
    } {
        rendered, runErr := runContainerCommand(newTestRuntime(newTwinRegistrationContainer(t)), arguments)
        if nil != runErr {
            t.Fatalf("%v: expected no error, got %v", arguments, runErr)
        }

        if false == strings.Contains(rendered, "view.shared") {
            t.Fatalf("%v: expected the window to hold the scoped twin, got %q", arguments, rendered)
        }

        if true == strings.Contains(rendered, "TEARDOWN") {
            t.Fatalf("%v: expected no teardown block for a window holding only the scoped registration, got %q", arguments, rendered)
        }
    }
}

func TestContainerCommand_TheSingleDoorOnATwinNameAnswersScopedWithoutTheContainerTwinsRow(t *testing.T) {
    rendered, runErr := runContainerCommand(newTestRuntime(newTwinRegistrationContainer(t)), []string{"view.shared", "--format=table"})
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    if false == strings.Contains(rendered, "| lifetime | scoped") {
        t.Fatalf("expected the door to answer the scoped registration, got %q", rendered)
    }

    if true == strings.Contains(rendered, "TEARDOWN") {
        t.Fatalf("expected no teardown block beside a scoped registration, got %q", rendered)
    }
}

func sanitizeErrorContextValue(value any) any {
    return sanitizeErrorContextValueTracked(value, map[errorContextVisitKey]struct{}{}, 0, false)
}

type maskingErrorContextMap map[string]any

func (instance maskingErrorContextMap) MarshalJSON() ([]byte, error) {
    return []byte(`{"masked":true,"trace":"frames"}`), nil
}

func TestSanitizeErrorContextValue_RendersADefinedMapThroughItsOwnMarshalerThenWalksIt(t *testing.T) {
    sanitized := sanitizeErrorContextValue(map[string]any{
        "value": maskingErrorContextMap{"secret": "x"},
    })

    encoded, marshalErr := json.Marshal(sanitized)
    if nil != marshalErr {
        t.Fatalf("the sanitized context must stay marshalable: %v", marshalErr)
    }

    if `{"value":{"masked":true}}` != string(encoded) {
        t.Fatalf("expected the map rendered through its own MarshalJSON with the noise key dropped, got %s", encoded)
    }
}

type failingMaskingErrorContextMap map[string]any

func (instance failingMaskingErrorContextMap) MarshalJSON() ([]byte, error) {
    return nil, errors.New("the masking method could not render")
}

type panickingMaskingErrorContextMap map[string]any

func (instance panickingMaskingErrorContextMap) MarshalJSON() ([]byte, error) {
    panic("the masking method panicked")
}

func TestSanitizeErrorContextValue_RendersAMapWhoseOwnMarshalerFailsAsTheMarkerNotInTheClear(t *testing.T) {
    for name, value := range map[string]any{
        "failing":   failingMaskingErrorContextMap{"secret": "hunter2"},
        "panicking": panickingMaskingErrorContextMap{"secret": "hunter2"},
    } {
        sanitized := sanitizeErrorContextValue(map[string]any{"value": value})

        encoded, marshalErr := json.Marshal(sanitized)
        if nil != marshalErr {
            t.Fatalf("%s: the sanitized context must stay marshalable: %v", name, marshalErr)
        }

        if true == strings.Contains(string(encoded), "hunter2") || false == strings.Contains(string(encoded), "marshal failed") {
            t.Fatalf("%s: expected the failure marker in place of the value, got %s", name, encoded)
        }
    }
}

func TestContainerCommand_TheZeroValueRefusesThroughTheEnvelope(t *testing.T) {
    rendered, runErr := runDebugCommand(&ContainerCommand{}, newTestRuntime(container.NewContainer(container.ContainerConfig{})), []string{"--format=json"})

    var exitError *exception.ExitError
    if false == errors.As(runErr, &exitError) {
        t.Fatalf("expected an exit error for the zero value, got %v", runErr)
    }

    if false == strings.Contains(rendered, "debug.containerNil") {
        t.Fatalf("expected the refusal in the document, got %s", rendered)
    }
}
