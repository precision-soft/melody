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

    "github.com/precision-soft/melody/v2/container"
    containercontract "github.com/precision-soft/melody/v2/container/contract"
    "github.com/precision-soft/melody/v2/exception"
    exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
)

/* @info truncateTableCellValue must not split a multibyte UTF-8 rune when it cuts an over-long cell */
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

/* @info wrapFixedWidth must not split a multibyte UTF-8 rune at the wrap boundary */
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

/* @info resolveErrorContextJson must strip stack keys even when marshalling fails and it falls back to fmt */
func TestResolveErrorContextJson_RedactsStackOnMarshalFailure(t *testing.T) {
    contextValue := exceptioncontract.Context{
        "stack":      "SECRET_STACK_TRACE",
        "panicStack": "SECRET_PANIC_STACK",
        "channel":    make(chan int), /* not JSON marshalable, so it forces the fmt fallback */
    }

    resolveErr := exception.NewError("boom", contextValue, nil)

    result := resolveErrorContextJson(resolveErr, 0)

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
        resolveErrorContextJson(exception.NewError("boom", newCyclicErrorContext(), nil), 3)

    case "cyclicSliceContext":
        resolveErrorContextJson(exception.NewError("boom", newCyclicSliceErrorContext(), nil), 3)

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

/* @info a service-resolution error whose context holds itself must not take the debug:container command — and with it the process — down; the redaction runs before json.Marshal, deliberately, so encoding/json's own cycle detector never gets the chance to turn this into a clean error */
func TestResolveErrorContextJson_SelfReferentialMapContextTerminates(t *testing.T) {
    assertErrorContextProbeExitsCleanly(t, "cyclicMapContext", 30*time.Second)
}

/* @info the same loop closed through a slice element rather than a map key */
func TestResolveErrorContextJson_SelfReferentialSliceContextTerminates(t *testing.T) {
    assertErrorContextProbeExitsCleanly(t, "cyclicSliceContext", 30*time.Second)
}

/* @info the guarded rendering must still be usable: the surviving keys are printed and the point where the loop closed is named, rather than the whole context being dropped */
func TestResolveErrorContextJson_RendersTheCycleAsAMarker(t *testing.T) {
    result := resolveErrorContextJson(exception.NewError("boom", newCyclicErrorContext(), nil), 3)

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

/* @info the guard is scoped to the current path, not to everything the walk has ever seen: one map handed to two sibling keys is not a cycle, and marking the second occurrence would be a silent wrong answer about the operator's own data */
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

/* @info The cycle guard above answers a context that holds itself. It says nothing about one that is merely very deep, and until this bound nothing else did either: the walk descended until the goroutine stack was gone, which is `fatal error: stack overflow` — not a panic, so no recover in the command layer turns it into a reported failure and the process dies rendering a debug page. Measured with the stack capped at 16 MiB it took some five hundred thousand levels; the production cap of a gigabyte scales that up rather than removing it.

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

/* @info the control: an ordinary error context, which nests a handful of levels, must come through untouched. A bound that truncated real contexts would trade a rare fatal error for an everyday loss of the information the page exists to show. */
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
