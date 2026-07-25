package debug

import (
    "encoding/json"
    "fmt"
    "testing"

    "github.com/precision-soft/melody/v2/container"
    httpcontract "github.com/precision-soft/melody/v2/http/contract"
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
    middleware := make([]httpcontract.Middleware, 0, middlewareCount)

    for index := 0; index < middlewareCount; index++ {
        middleware = append(
            middleware,
            func(next httpcontract.Handler) httpcontract.Handler {
                return next
            },
        )
    }

    return NewMiddlewareCommand(
        func() []httpcontract.Middleware {
            return middleware
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
