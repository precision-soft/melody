package debug

import (
    "encoding/json"
    "fmt"
    "strings"
    "testing"

    "github.com/precision-soft/melody/container"
    "github.com/precision-soft/melody/exception"
    httpcontract "github.com/precision-soft/melody/http/contract"
    middlewarepipeline "github.com/precision-soft/melody/http/middleware/pipeline"
)

type middlewareCommandTestEnvelope struct {
    Data struct {
        Items []struct {
            Index int    `json:"index"`
            Name  string `json:"name"`
        } `json:"items"`
        Total  int `json:"total"`
        Limit  int `json:"limit"`
        Offset int `json:"offset"`
    } `json:"data"`
}

func newMiddlewareTestCommand(middlewareCount int) *MiddlewareCommand {
    descriptions := make([]middlewarepipeline.MiddlewareDescription, 0, middlewareCount)
    middleware := make([]httpcontract.Middleware, 0, middlewareCount)

    for index := 0; index < middlewareCount; index++ {
        descriptions = append(
            descriptions,
            middlewarepipeline.MiddlewareDescription{
                Name:     fmt.Sprintf("middleware.%02d", index),
                Priority: 0,
            },
        )

        middleware = append(
            middleware,
            func(next httpcontract.Handler) httpcontract.Handler {
                return next
            },
        )
    }

    return NewMiddlewareCommand(
        func() ([]middlewarepipeline.MiddlewareDescription, *middlewarepipeline.MiddlewareBuildReport, error) {
            return descriptions, nil, nil
        },
        func() ([]httpcontract.Middleware, error) {
            return middleware, nil
        },
    )
}

func newMiddlewareTestRuntime() *testRuntime {
    return newTestRuntime(container.NewContainer())
}

func decodeMiddlewareCommandEnvelope(t *testing.T, rendered string) middlewareCommandTestEnvelope {
    t.Helper()

    envelope := middlewareCommandTestEnvelope{}

    decodeErr := json.Unmarshal([]byte(rendered), &envelope)
    if nil != decodeErr {
        t.Fatalf("failed to decode the rendered envelope: %v, rendered %q", decodeErr, rendered)
    }

    return envelope
}

func TestMiddlewareCommand_AppliesLimitAndOffsetToTheRenderedItems(t *testing.T) {
    rendered, runErr := runDebugCommand(
        newMiddlewareTestCommand(10),
        newMiddlewareTestRuntime(),
        []string{"--format=json", "--limit=2", "--offset=4"},
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    envelope := decodeMiddlewareCommandEnvelope(t, rendered)

    if 10 != envelope.Data.Total {
        t.Fatalf("expected total 10, got %d", envelope.Data.Total)
    }
    if 2 != len(envelope.Data.Items) {
        t.Fatalf("expected 2 items for --limit=2, got %d", len(envelope.Data.Items))
    }
    if 5 != envelope.Data.Items[0].Index {
        t.Fatalf("expected the window to start at --offset=4, got index %d", envelope.Data.Items[0].Index)
    }
    if 6 != envelope.Data.Items[1].Index {
        t.Fatalf("expected the second windowed item, got index %d", envelope.Data.Items[1].Index)
    }
}

func TestMiddlewareCommand_ReturnsNoItemsForAnOffsetPastTheTotal(t *testing.T) {
    rendered, runErr := runDebugCommand(
        newMiddlewareTestCommand(10),
        newMiddlewareTestRuntime(),
        []string{"--format=json", "--offset=25"},
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    envelope := decodeMiddlewareCommandEnvelope(t, rendered)

    if 10 != envelope.Data.Total {
        t.Fatalf("expected total 10, got %d", envelope.Data.Total)
    }
    if 0 != len(envelope.Data.Items) {
        t.Fatalf("expected no items past the total, got %d", len(envelope.Data.Items))
    }
}

func TestMiddlewareCommand_ReturnsEveryItemWithoutALimit(t *testing.T) {
    rendered, runErr := runDebugCommand(
        newMiddlewareTestCommand(10),
        newMiddlewareTestRuntime(),
        []string{"--format=json"},
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    envelope := decodeMiddlewareCommandEnvelope(t, rendered)

    if 10 != len(envelope.Data.Items) {
        t.Fatalf("expected 10 items without a limit, got %d", len(envelope.Data.Items))
    }
}

func TestMiddlewareCommand_WalksEveryItemExactlyOnceWhenPaging(t *testing.T) {
    command := newMiddlewareTestCommand(10)

    seen := map[int]int{}
    offset := 0

    for pageIndex := 0; pageIndex < 10; pageIndex++ {
        rendered, runErr := runDebugCommand(
            command,
            newMiddlewareTestRuntime(),
            []string{"--format=json", "--limit=3", fmt.Sprintf("--offset=%d", offset)},
        )
        if nil != runErr {
            t.Fatalf("expected no error, got %v", runErr)
        }

        envelope := decodeMiddlewareCommandEnvelope(t, rendered)

        if 0 == len(envelope.Data.Items) {
            break
        }

        for _, item := range envelope.Data.Items {
            seen[item.Index] = seen[item.Index] + 1
        }

        offset = offset + 3
    }

    if 10 != len(seen) {
        t.Fatalf("expected paging to walk 10 distinct middleware, got %d", len(seen))
    }

    for index, count := range seen {
        if 1 != count {
            t.Fatalf("middleware %d was returned %d times while paging", index, count)
        }
    }
}

func TestMiddlewareCommand_AppliesTheSameWindowInTheTableFormat(t *testing.T) {
    rendered, runErr := runDebugCommand(
        newMiddlewareTestCommand(10),
        newMiddlewareTestRuntime(),
        []string{"--format=table", "--table-width=400", "--limit=2", "--offset=4"},
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    row := debugTableBlockRow(rendered, "MIDDLEWARE")

    if 2 != len(row) {
        t.Fatalf("expected 2 table rows for --limit=2, got %d, rendered %q", len(row), rendered)
    }
    if "5" != row[0][0] {
        t.Fatalf("expected the table window to start at --offset=4, got index %q", row[0][0])
    }
    if "6" != row[1][0] {
        t.Fatalf("expected the second windowed table row, got index %q", row[1][0])
    }
}

func middlewareCommandIndexList(t *testing.T, arguments []string) []int {
    t.Helper()

    rendered, runErr := runDebugCommand(
        newMiddlewareTestCommand(10),
        newMiddlewareTestRuntime(),
        arguments,
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    envelope := decodeMiddlewareCommandEnvelope(t, rendered)

    index := make([]int, 0, len(envelope.Data.Items))
    for _, item := range envelope.Data.Items {
        index = append(index, item.Index)
    }

    return index
}

func assertMiddlewareIndexList(t *testing.T, expected []int, actual []int) {
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

func TestMiddlewareCommand_KeepsTheAscendingOrderByDefault(t *testing.T) {
    assertMiddlewareIndexList(
        t,
        []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
        middlewareCommandIndexList(t, []string{"--format=json"}),
    )
}

func TestMiddlewareCommand_ReversesTheItemsForADescendingOrder(t *testing.T) {
    assertMiddlewareIndexList(
        t,
        []int{10, 9, 8, 7, 6, 5, 4, 3, 2, 1},
        middlewareCommandIndexList(t, []string{"--format=json", "--order=desc"}),
    )
}

func TestMiddlewareCommand_AppliesTheDescendingOrderBeforeTheWindow(t *testing.T) {
    assertMiddlewareIndexList(
        t,
        []int{10, 9, 8},
        middlewareCommandIndexList(t, []string{"--format=json", "--order=desc", "--limit=3"}),
    )

    assertMiddlewareIndexList(
        t,
        []int{7, 6},
        middlewareCommandIndexList(t, []string{"--format=json", "--order=desc", "--limit=2", "--offset=3"}),
    )
}

func TestMiddlewareCommand_WalksEveryItemExactlyOnceWhenPagingDescending(t *testing.T) {
    seen := map[int]int{}
    offset := 0

    for pageIndex := 0; pageIndex < 10; pageIndex++ {
        index := middlewareCommandIndexList(
            t,
            []string{"--format=json", "--order=desc", "--limit=4", fmt.Sprintf("--offset=%d", offset)},
        )

        if 0 == len(index) {
            break
        }

        for _, value := range index {
            seen[value] = seen[value] + 1
        }

        offset = offset + 4
    }

    if 10 != len(seen) {
        t.Fatalf("expected paging to walk 10 distinct middleware, got %d", len(seen))
    }

    for value, count := range seen {
        if 1 != count {
            t.Fatalf("middleware %d was returned %d times while paging descending", value, count)
        }
    }
}

func TestNewMiddlewareCommand_NilProvider_Panics(t *testing.T) {
    assertNilProviderPanics := func(expectedMessage string, construct func()) {
        defer func() {
            recoveredValue := recover()
            if nil == recoveredValue {
                t.Fatalf("expected a panic for a nil provider")
            }

            recoveredError, ok := recoveredValue.(*exception.Error)
            if false == ok || nil == recoveredError {
                t.Fatalf("expected the panic to carry an *exception.Error, got %v", recoveredValue)
            }

            if expectedMessage != recoveredError.Message() {
                t.Fatalf("unexpected message %q", recoveredError.Message())
            }
        }()

        construct()
    }

    assertNilProviderPanics("middleware command created with nil description provider", func() {
        NewMiddlewareCommand(nil, func() ([]httpcontract.Middleware, error) { return nil, nil })
    })

    assertNilProviderPanics("middleware command created with nil build provider", func() {
        NewMiddlewareCommand(
            func() ([]middlewarepipeline.MiddlewareDescription, *middlewarepipeline.MiddlewareBuildReport, error) {
                return nil, nil, nil
            },
            nil,
        )
    })
}

func TestMiddlewareCommand_ZeroValueProvider_ReturnsANamedError(t *testing.T) {
    _, runErr := runDebugCommand(
        &MiddlewareCommand{},
        newTestRuntime(container.NewContainer()),
        []string{"--format=json"},
    )

    if nil == runErr {
        t.Fatalf("expected an error for a nil provider")
    }

    if false == strings.Contains(runErr.Error(), "middleware provider is nil") {
        t.Fatalf("expected the named refusal, got %v", runErr)
    }
}

/* the default listing is a description: nothing is built, so the command has no side effect in the console process that asks — the build is the explicit flag's to run */
func TestMiddlewareCommand_DefaultListingRunsNoBuild(t *testing.T) {
    buildRuns := 0

    command := NewMiddlewareCommand(
        func() ([]middlewarepipeline.MiddlewareDescription, *middlewarepipeline.MiddlewareBuildReport, error) {
            return []middlewarepipeline.MiddlewareDescription{
                {Name: "static", Priority: -1000, FunctionName: "example/static.Middleware"},
            }, nil, nil
        },
        func() ([]httpcontract.Middleware, error) {
            buildRuns = buildRuns + 1

            return nil, nil
        },
    )

    rendered, runErr := runDebugCommand(command, newMiddlewareTestRuntime(), []string{"--format=table", "--table-width=400"})
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    if 0 != buildRuns {
        t.Fatalf("expected the default listing to run no build, ran %d times", buildRuns)
    }

    row := debugTableBlockRow(rendered, "MIDDLEWARE")
    if 1 != len(row) {
        t.Fatalf("expected one row, got %d in %q", len(row), rendered)
    }

    if "static" != row[0][1] || "-1000" != row[0][2] || "example/static.Middleware" != row[0][3] || "active" != row[0][4] {
        t.Fatalf("expected the description columns, got %v", row[0])
    }

    _, buildRunErr := runDebugCommand(command, newMiddlewareTestRuntime(), []string{"--format=json", "--build"})
    if nil != buildRunErr {
        t.Fatalf("expected no error, got %v", buildRunErr)
    }

    if 1 != buildRuns {
        t.Fatalf("expected --build to run the build once, ran %d times", buildRuns)
    }
}

/* an inactive definition is part of the answer: the reason a middleware is off is exactly what the operator lists the pipeline to learn */
func TestMiddlewareCommand_ListsTheInactiveEntriesWithTheirReason(t *testing.T) {
    command := NewMiddlewareCommand(
        func() ([]middlewarepipeline.MiddlewareDescription, *middlewarepipeline.MiddlewareBuildReport, error) {
            report := middlewarepipeline.NewMiddlewareBuildReport(
                "http",
                "prod",
                []string{},
                []*middlewarepipeline.InactiveMiddleware{
                    middlewarepipeline.NewInactiveMiddleware("profiler", "enabled for environments [dev]"),
                },
                nil,
                false,
            )

            return []middlewarepipeline.MiddlewareDescription{{Name: "static", Priority: -1000}}, report, nil
        },
        func() ([]httpcontract.Middleware, error) {
            return nil, nil
        },
    )

    rendered, runErr := runDebugCommand(command, newMiddlewareTestRuntime(), []string{"--format=table", "--table-width=400"})
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    row := debugTableBlockRow(rendered, "MIDDLEWARE")
    if 2 != len(row) {
        t.Fatalf("expected an active and an inactive row, got %d in %q", len(row), rendered)
    }

    if "profiler" != row[1][1] || "inactive" != row[1][4] || "enabled for environments [dev]" != row[1][5] {
        t.Fatalf("expected the inactive row with its reason, got %v", row[1])
    }
}

/* a factory that panics under --build answers as a rendered failure: the command that asks about the chain must survive the chain's worst answer */
func TestMiddlewareCommand_BuildRecoversAPanickingFactory(t *testing.T) {
    command := NewMiddlewareCommand(
        func() ([]middlewarepipeline.MiddlewareDescription, *middlewarepipeline.MiddlewareBuildReport, error) {
            return nil, nil, nil
        },
        func() ([]httpcontract.Middleware, error) {
            panic("factory exploded")
        },
    )

    rendered, runErr := runDebugCommand(command, newMiddlewareTestRuntime(), []string{"--format=json", "--build"})

    if nil == runErr {
        t.Fatalf("expected the rendered failure to carry an error, got nil")
    }

    if false == strings.Contains(rendered, "middleware build panicked") || false == strings.Contains(rendered, "factory exploded") {
        t.Fatalf("expected the recovered panic in the envelope, got %q", rendered)
    }
}

/* a pipeline that cannot be assembled — a cycle, a missing reference — refuses the description with the same error the build answers, rendered instead of thrown */
func TestMiddlewareCommand_RendersTheDescriptionRefusal(t *testing.T) {
    command := NewMiddlewareCommand(
        func() ([]middlewarepipeline.MiddlewareDescription, *middlewarepipeline.MiddlewareBuildReport, error) {
            return nil, nil, exception.NewError("middleware pipeline has a cycle", nil, nil)
        },
        func() ([]httpcontract.Middleware, error) {
            return nil, nil
        },
    )

    rendered, runErr := runDebugCommand(command, newMiddlewareTestRuntime(), []string{"--format=json"})

    if nil == runErr {
        t.Fatalf("expected the refusal to carry an error, got nil")
    }

    if false == strings.Contains(rendered, "debug.describeFailed") || false == strings.Contains(rendered, "middleware pipeline has a cycle") {
        t.Fatalf("expected the refusal in the envelope, got %q", rendered)
    }
}

func TestMiddlewareCommand_SameNameInactiveEntriesKeepTheReasonOrder(t *testing.T) {
    inactiveEntries := make([]*middlewarepipeline.InactiveMiddleware, 0, 16)
    for index := 15; index >= 0; index-- {
        inactiveEntries = append(
            inactiveEntries,
            middlewarepipeline.NewInactiveMiddleware("profiler", fmt.Sprintf("reason %02d", index)),
        )
    }

    command := NewMiddlewareCommand(
        func() ([]middlewarepipeline.MiddlewareDescription, *middlewarepipeline.MiddlewareBuildReport, error) {
            report := middlewarepipeline.NewMiddlewareBuildReport(
                "http",
                "prod",
                []string{},
                inactiveEntries,
                nil,
                false,
            )

            return nil, report, nil
        },
        func() ([]httpcontract.Middleware, error) {
            return nil, nil
        },
    )

    rendered, runErr := runDebugCommand(command, newMiddlewareTestRuntime(), []string{"--format=table", "--table-width=400"})
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    row := debugTableBlockRow(rendered, "MIDDLEWARE")
    if 16 != len(row) {
        t.Fatalf("expected 16 inactive rows, got %d", len(row))
    }

    for index, columns := range row {
        expectedReason := fmt.Sprintf("reason %02d", index)
        if expectedReason != columns[5] {
            t.Fatalf("expected the same-name rows ordered by reason, row %d carries %q", index, columns[5])
        }
    }
}

/* the reason appeared and disappeared with the row: omitempty dropped it from every active middleware, so a consumer keying on it could not tell an active entry from a malformed document, and the three shapes this one struct serves — described-active, described-inactive, built — differed in their key set as well as their values. */
func TestMiddlewareCommand_TheReasonKeyIsPresentOnEveryRow(t *testing.T) {
    rendered, runErr := runDebugCommand(
        NewMiddlewareCommand(
            func() ([]middlewarepipeline.MiddlewareDescription, *middlewarepipeline.MiddlewareBuildReport, error) {
                return []middlewarepipeline.MiddlewareDescription{
                    {Name: "cors", Priority: 100, FunctionName: "cors.Middleware"},
                }, nil, nil
            },
            func() ([]httpcontract.Middleware, error) {
                return nil, nil
            },
        ),
        newTestRuntime(container.NewContainer()),
        []string{"--format=json"},
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    decoded := struct {
        Data struct {
            Items []map[string]any `json:"items"`
        } `json:"data"`
    }{}
    if decodeErr := json.Unmarshal([]byte(rendered), &decoded); nil != decodeErr {
        t.Fatalf("failed to decode the rendered envelope: %v, rendered %q", decodeErr, rendered)
    }

    if 1 != len(decoded.Data.Items) {
        t.Fatalf("expected the one middleware, got %d in %q", len(decoded.Data.Items), rendered)
    }

    reasonValue, hasReason := decoded.Data.Items[0]["reason"]
    if false == hasReason {
        t.Fatalf("expected the reason key on an active row, got %q", rendered)
    }

    if "" != reasonValue {
        t.Fatalf("expected an empty reason on an active row, got %#v", reasonValue)
    }
}
