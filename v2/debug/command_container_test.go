package debug

import (
    "encoding/json"
    "errors"
    "fmt"
    "strings"
    "testing"
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
