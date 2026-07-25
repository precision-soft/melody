package debug

import (
    "encoding/json"
    "fmt"
    nethttp "net/http"
    "strings"
    "testing"

    "github.com/precision-soft/melody/container"
    containercontract "github.com/precision-soft/melody/container/contract"
    "github.com/precision-soft/melody/http"
    httpcontract "github.com/precision-soft/melody/http/contract"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
)

type routerCommandTestEnvelope struct {
    Data struct {
        Items []struct {
            Pattern string `json:"pattern"`
        } `json:"items"`
        Total  int `json:"total"`
        Limit  int `json:"limit"`
        Offset int `json:"offset"`
    } `json:"data"`
}

func newRouterTestRuntime(routeCount int) *testRuntime {
    router := http.NewRouter()

    for index := 0; index < routeCount; index++ {
        router.HandleNamed(
            fmt.Sprintf("route.%02d", index),
            nethttp.MethodGet,
            fmt.Sprintf("/route/%02d", index),
            func(
                runtimeInstance runtimecontract.Runtime,
                writer nethttp.ResponseWriter,
                request httpcontract.Request,
            ) (httpcontract.Response, error) {
                return nil, nil
            },
        )
    }

    serviceContainer := container.NewContainer()

    serviceContainer.MustRegister(
        http.ServiceRouter,
        func(resolver containercontract.Resolver) (httpcontract.Router, error) {
            return router, nil
        },
    )

    return newTestRuntime(serviceContainer)
}

func decodeRouterCommandEnvelope(t *testing.T, rendered string) routerCommandTestEnvelope {
    t.Helper()

    envelope := routerCommandTestEnvelope{}

    decodeErr := json.Unmarshal([]byte(rendered), &envelope)
    if nil != decodeErr {
        t.Fatalf("failed to decode the rendered envelope: %v, rendered %q", decodeErr, rendered)
    }

    return envelope
}

func TestRouterCommand_AppliesLimitAndOffsetToTheRenderedItems(t *testing.T) {
    runtimeInstance := newRouterTestRuntime(10)

    rendered, runErr := runDebugCommand(
        &RouterCommand{},
        runtimeInstance,
        []string{"--format=json", "--limit=2", "--offset=4"},
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    envelope := decodeRouterCommandEnvelope(t, rendered)

    if 10 != envelope.Data.Total {
        t.Fatalf("expected total 10, got %d", envelope.Data.Total)
    }
    if 2 != len(envelope.Data.Items) {
        t.Fatalf("expected 2 items for --limit=2, got %d", len(envelope.Data.Items))
    }
    if "/route/04" != envelope.Data.Items[0].Pattern {
        t.Fatalf("expected the window to start at --offset=4, got %q", envelope.Data.Items[0].Pattern)
    }
    if "/route/05" != envelope.Data.Items[1].Pattern {
        t.Fatalf("expected the second windowed item, got %q", envelope.Data.Items[1].Pattern)
    }
}

func TestRouterCommand_ReturnsNoItemsForAnOffsetPastTheTotal(t *testing.T) {
    runtimeInstance := newRouterTestRuntime(10)

    rendered, runErr := runDebugCommand(
        &RouterCommand{},
        runtimeInstance,
        []string{"--format=json", "--offset=25"},
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    envelope := decodeRouterCommandEnvelope(t, rendered)

    if 10 != envelope.Data.Total {
        t.Fatalf("expected total 10, got %d", envelope.Data.Total)
    }
    if 0 != len(envelope.Data.Items) {
        t.Fatalf("expected no items past the total, got %d", len(envelope.Data.Items))
    }
}

func TestRouterCommand_ReturnsEveryItemWithoutALimit(t *testing.T) {
    runtimeInstance := newRouterTestRuntime(10)

    rendered, runErr := runDebugCommand(
        &RouterCommand{},
        runtimeInstance,
        []string{"--format=json"},
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    envelope := decodeRouterCommandEnvelope(t, rendered)

    if 10 != len(envelope.Data.Items) {
        t.Fatalf("expected 10 items without a limit, got %d", len(envelope.Data.Items))
    }
}

func TestRouterCommand_WalksEveryItemExactlyOnceWhenPaging(t *testing.T) {
    runtimeInstance := newRouterTestRuntime(10)

    seen := map[string]int{}
    offset := 0

    for pageIndex := 0; pageIndex < 10; pageIndex++ {
        rendered, runErr := runDebugCommand(
            &RouterCommand{},
            runtimeInstance,
            []string{"--format=json", "--limit=3", fmt.Sprintf("--offset=%d", offset)},
        )
        if nil != runErr {
            t.Fatalf("expected no error, got %v", runErr)
        }

        envelope := decodeRouterCommandEnvelope(t, rendered)

        if 0 == len(envelope.Data.Items) {
            break
        }

        for _, item := range envelope.Data.Items {
            seen[item.Pattern] = seen[item.Pattern] + 1
        }

        offset = offset + 3
    }

    if 10 != len(seen) {
        t.Fatalf("expected paging to walk 10 distinct routes, got %d", len(seen))
    }

    for pattern, count := range seen {
        if 1 != count {
            t.Fatalf("route %q was returned %d times while paging", pattern, count)
        }
    }
}

func routerCommandPatternList(envelope routerCommandTestEnvelope) []string {
    patterns := make([]string, 0, len(envelope.Data.Items))
    for _, item := range envelope.Data.Items {
        patterns = append(patterns, item.Pattern)
    }

    return patterns
}

func TestRouterCommand_ReversesTheItemsForADescendingOrder(t *testing.T) {
    runtimeInstance := newRouterTestRuntime(10)

    ascendingRendered, ascendingErr := runDebugCommand(
        &RouterCommand{},
        runtimeInstance,
        []string{"--format=json", "--order=asc"},
    )
    if nil != ascendingErr {
        t.Fatalf("expected no error, got %v", ascendingErr)
    }

    descendingRendered, descendingErr := runDebugCommand(
        &RouterCommand{},
        runtimeInstance,
        []string{"--format=json", "--order=desc"},
    )
    if nil != descendingErr {
        t.Fatalf("expected no error, got %v", descendingErr)
    }

    ascending := routerCommandPatternList(decodeRouterCommandEnvelope(t, ascendingRendered))
    descending := routerCommandPatternList(decodeRouterCommandEnvelope(t, descendingRendered))

    if len(ascending) != len(descending) {
        t.Fatalf("expected %d items, got %d", len(ascending), len(descending))
    }

    for index, pattern := range ascending {
        expected := descending[len(descending)-1-index]
        if expected != pattern {
            t.Fatalf("expected the descending order to be the reverse of %v, got %v", ascending, descending)
        }
    }
}

func TestRouterCommand_AppliesTheDescendingOrderBeforeTheWindow(t *testing.T) {
    runtimeInstance := newRouterTestRuntime(10)

    rendered, runErr := runDebugCommand(
        &RouterCommand{},
        runtimeInstance,
        []string{"--format=json", "--order=desc", "--limit=3"},
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    patterns := routerCommandPatternList(decodeRouterCommandEnvelope(t, rendered))

    expected := []string{"/route/09", "/route/08", "/route/07"}

    if len(expected) != len(patterns) {
        t.Fatalf("expected %v, got %v", expected, patterns)
    }

    for index, pattern := range expected {
        if pattern != patterns[index] {
            t.Fatalf("expected %v, got %v", expected, patterns)
        }
    }
}

func TestRouterCommand_WalksEveryItemExactlyOnceWhenPagingDescending(t *testing.T) {
    runtimeInstance := newRouterTestRuntime(10)

    seen := map[string]int{}
    offset := 0

    for pageIndex := 0; pageIndex < 10; pageIndex++ {
        rendered, runErr := runDebugCommand(
            &RouterCommand{},
            runtimeInstance,
            []string{"--format=json", "--order=desc", "--limit=3", fmt.Sprintf("--offset=%d", offset)},
        )
        if nil != runErr {
            t.Fatalf("expected no error, got %v", runErr)
        }

        envelope := decodeRouterCommandEnvelope(t, rendered)

        if 0 == len(envelope.Data.Items) {
            break
        }

        for _, item := range envelope.Data.Items {
            seen[item.Pattern] = seen[item.Pattern] + 1
        }

        offset = offset + 3
    }

    if 10 != len(seen) {
        t.Fatalf("expected paging to walk 10 distinct routes, got %d", len(seen))
    }

    for pattern, count := range seen {
        if 1 != count {
            t.Fatalf("route %q was returned %d times while paging descending", pattern, count)
        }
    }
}

func TestRouterCommand_AppliesTheSameWindowInTheTableFormat(t *testing.T) {
    runtimeInstance := newRouterTestRuntime(10)

    rendered, runErr := runDebugCommand(
        &RouterCommand{},
        runtimeInstance,
        []string{"--format=table", "--table-width=400", "--limit=2", "--offset=4"},
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    row := debugTableBlockRow(rendered, "ROUTES")

    if 2 != len(row) {
        t.Fatalf("expected 2 table rows for --limit=2, got %d, rendered %q", len(row), rendered)
    }
    if "/route/04" != row[0][1] {
        t.Fatalf("expected the table window to start at --offset=4, got %q", row[0][1])
    }
    if "/route/05" != row[1][1] {
        t.Fatalf("expected the second windowed table row, got %q", row[1][1])
    }

    if false == strings.Contains(rendered, "ROUTES: 10 total | 2 shown") {
        t.Fatalf("expected the summary to report the shown count, got %q", rendered)
    }
}

func TestRouterCommand_ReportsTheShownCountInTheTableSummaryWhenWindowed(t *testing.T) {
    runtimeInstance := newRouterTestRuntime(10)

    rendered, runErr := runDebugCommand(
        &RouterCommand{},
        runtimeInstance,
        []string{"--format=table", "--limit=3"},
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    if false == strings.Contains(rendered, "ROUTES: 10 total | 3 shown") {
        t.Fatalf("expected the summary to report the shown count, got %q", rendered)
    }
}

func TestRouterCommand_OmitsTheShownCountWhenNothingIsWindowedAway(t *testing.T) {
    runtimeInstance := newRouterTestRuntime(10)

    rendered, runErr := runDebugCommand(
        &RouterCommand{},
        runtimeInstance,
        []string{"--format=table"},
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    if true == strings.Contains(rendered, "shown") {
        t.Fatalf("expected no shown count for an unwindowed list, got %q", rendered)
    }
}
