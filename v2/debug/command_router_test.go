package debug

import (
    "encoding/json"
    "fmt"
    nethttp "net/http"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v2/container"
    containercontract "github.com/precision-soft/melody/v2/container/contract"
    "github.com/precision-soft/melody/v2/http"
    httpcontract "github.com/precision-soft/melody/v2/http/contract"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

type routerCommandTestEnvelope struct {
    Data struct {
        Items []struct {
            Pattern string `json:"pattern"`
            Order   int    `json:"order"`
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

/* the dispatch picks the higher priority and breaks ties on the lower registration order; without these two columns two overlapping routes rendered as identical rows and the command could not answer which one answers */
func TestRouterCommand_RendersThePriorityAndTheRegistrationOrder(t *testing.T) {
    router := http.NewRouter()

    handler := func(
        runtimeInstance runtimecontract.Runtime,
        writer nethttp.ResponseWriter,
        request httpcontract.Request,
    ) (httpcontract.Response, error) {
        return nil, nil
    }

    router.HandleWithOptions(
        "/overlap",
        handler,
        http.NewRouteOptions("", []string{nethttp.MethodGet}, "", nil, nil, nil, nil, 0, nil),
    )
    router.HandleWithOptions(
        "/overlap",
        handler,
        http.NewRouteOptions("", []string{nethttp.MethodGet}, "", nil, nil, nil, nil, 10, nil),
    )

    serviceContainer := container.NewContainer()
    serviceContainer.MustRegister(
        http.ServiceRouter,
        func(resolver containercontract.Resolver) (httpcontract.Router, error) {
            return router, nil
        },
    )

    rendered, runErr := runDebugCommand(
        &RouterCommand{},
        newTestRuntime(serviceContainer),
        []string{"--format=table", "--table-width=400"},
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    row := debugTableBlockRow(rendered, "ROUTES")
    if 2 != len(row) {
        t.Fatalf("expected two rows, got %d in %q", len(row), rendered)
    }

    if fmt.Sprintf("%v", row[0]) == fmt.Sprintf("%v", row[1]) {
        t.Fatalf("expected the overlapping routes to render distinguishably, got %v twice", row[0])
    }

    if "0" != row[0][3] || "10" != row[1][3] {
        t.Fatalf("expected the priority column, got %q and %q", row[0][3], row[1][3])
    }

    if "1" != row[0][4] || "2" != row[1][4] {
        t.Fatalf("expected the registration order column, got %q and %q", row[0][4], row[1][4])
    }
}

/* the machine document carries every discriminator the introspection exposes, and the verbose table folds them into columns; a json consumer used to see six fields for a ten-field definition */
func TestRouterCommand_CarriesTheDiscriminatorsInJsonAndVerboseTable(t *testing.T) {
    router := http.NewRouter()

    handler := func(
        runtimeInstance runtimecontract.Runtime,
        writer nethttp.ResponseWriter,
        request httpcontract.Request,
    ) (httpcontract.Response, error) {
        return nil, nil
    }

    router.HandleWithOptions(
        "/detail/:id",
        handler,
        http.NewRouteOptions(
            "detail.route",
            []string{nethttp.MethodGet},
            "",
            nil,
            map[string]string{"id": "[0-9]+"},
            map[string]string{"id": "1"},
            nil,
            5,
            map[string]any{"section": "catalog"},
        ),
    )

    serviceContainer := container.NewContainer()
    serviceContainer.MustRegister(
        http.ServiceRouter,
        func(resolver containercontract.Resolver) (httpcontract.Router, error) {
            return router, nil
        },
    )

    rendered, runErr := runDebugCommand(
        &RouterCommand{},
        newTestRuntime(serviceContainer),
        []string{"--format=json"},
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    envelope := struct {
        Data struct {
            Items []struct {
                Priority     int               `json:"priority"`
                Order        int               `json:"order"`
                Requirements map[string]string `json:"requirements"`
                Defaults     map[string]string `json:"defaults"`
                Attributes   map[string]any    `json:"attributes"`
            } `json:"items"`
        } `json:"data"`
    }{}

    if decodeErr := json.Unmarshal([]byte(rendered), &envelope); nil != decodeErr {
        t.Fatalf("failed to decode the envelope: %v, rendered %q", decodeErr, rendered)
    }

    if 1 != len(envelope.Data.Items) {
        t.Fatalf("expected one item, got %d", len(envelope.Data.Items))
    }

    item := envelope.Data.Items[0]
    if 5 != item.Priority || 1 != item.Order {
        t.Fatalf("expected priority 5 and order 1, got %d and %d", item.Priority, item.Order)
    }

    /* the requirement travels as the compiled anchor form, and the attributes carry the framework's own underscore-prefixed entries beside the caller's */
    if "" == item.Requirements["id"] || "1" != item.Defaults["id"] || "catalog" != item.Attributes["section"] {
        t.Fatalf("expected the discriminator maps to travel, got %+v", item)
    }

    verboseRendered, verboseErr := runDebugCommand(
        &RouterCommand{},
        newTestRuntime(serviceContainer),
        []string{"--format=table", "--table-width=400", "--verbose"},
    )
    if nil != verboseErr {
        t.Fatalf("expected no error, got %v", verboseErr)
    }

    verboseRow := debugTableBlockRow(verboseRendered, "ROUTES")
    if 1 != len(verboseRow) {
        t.Fatalf("expected one verbose row, got %d in %q", len(verboseRow), verboseRendered)
    }

    if false == strings.HasPrefix(verboseRow[0][8], "id=") || "id=1" != verboseRow[0][9] || false == strings.Contains(verboseRow[0][10], "section=catalog") {
        t.Fatalf("expected the compact discriminator cells, got %v", verboseRow[0])
    }
}

func TestRouterCommand_TiedPatternAndMethodsRowsKeepTheRegistrationOrder(t *testing.T) {
    router := http.NewRouter()

    for index := 0; index < 16; index++ {
        router.HandleWithOptions(
            "/tied",
            func(
                runtimeInstance runtimecontract.Runtime,
                writer nethttp.ResponseWriter,
                request httpcontract.Request,
            ) (httpcontract.Response, error) {
                return nil, nil
            },
            http.NewRouteOptions(
                fmt.Sprintf("tied.%02d", index),
                []string{nethttp.MethodGet},
                fmt.Sprintf("host%02d.example.com", index),
                nil,
                nil,
                nil,
                nil,
                0,
                nil,
            ),
        )
    }

    serviceContainer := container.NewContainer()

    serviceContainer.MustRegister(
        http.ServiceRouter,
        func(resolver containercontract.Resolver) (httpcontract.Router, error) {
            return router, nil
        },
    )

    rendered, runErr := runDebugCommand(
        &RouterCommand{},
        newTestRuntime(serviceContainer),
        []string{"--format=json"},
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    envelope := decodeRouterCommandEnvelope(t, rendered)
    if 16 != len(envelope.Data.Items) {
        t.Fatalf("expected 16 rows, got %d", len(envelope.Data.Items))
    }

    for index, item := range envelope.Data.Items {
        if index+1 != item.Order {
            t.Fatalf("expected the tied rows in registration order, row %d carries order %d", index, item.Order)
        }
    }
}

/* a route attribute is arbitrary any from userland, and one value the encoder cannot represent made the whole envelope fail to marshal: the printer has no fallback, so the command answered ZERO bytes and the caller was left with an empty stream indistinguishable from a missing binary. The value that cannot be represented degrades to the rendering the verbose table already prints; every value that CAN be represented keeps its json type, or the methods attribute would arrive as a string where the consumer keyed a list. */
func TestRouterCommand_KeepsTheDocumentWhenAnAttributeCannotBeSerialized(t *testing.T) {
    router := http.NewRouter()
    router.HandleWithOptions(
        "/probe",
        func(
            runtimeInstance runtimecontract.Runtime,
            writer nethttp.ResponseWriter,
            request httpcontract.Request,
        ) (httpcontract.Response, error) {
            return nil, nil
        },
        http.NewRouteOptions(
            "probe.route",
            []string{nethttp.MethodGet},
            "",
            nil,
            nil,
            nil,
            nil,
            0,
            map[string]any{
                "handlerHook":   func() {},
                "allowedRoles":  []string{"admin", "auditor"},
                "requiresLogin": true,
            },
        ),
    )

    serviceContainer := container.NewContainer()
    serviceContainer.MustRegister(
        http.ServiceRouter,
        func(resolver containercontract.Resolver) (httpcontract.Router, error) {
            return router, nil
        },
    )

    rendered, runErr := runDebugCommand(
        &RouterCommand{},
        newTestRuntime(serviceContainer),
        []string{"--format=json"},
    )
    if nil != runErr {
        t.Fatalf("expected the document to be produced, got %v", runErr)
    }

    decoded := struct {
        Data struct {
            Items []struct {
                Attributes map[string]any `json:"attributes"`
            } `json:"items"`
        } `json:"data"`
    }{}
    if decodeErr := json.Unmarshal([]byte(rendered), &decoded); nil != decodeErr {
        t.Fatalf("failed to decode the rendered envelope: %v, rendered %q", decodeErr, rendered)
    }

    if 1 != len(decoded.Data.Items) {
        t.Fatalf("expected the one route, got %d in %q", len(decoded.Data.Items), rendered)
    }

    attributes := decoded.Data.Items[0].Attributes

    hookValue, hookIsText := attributes["handlerHook"].(string)
    if false == hookIsText || false == strings.HasPrefix(hookValue, "0x") {
        t.Fatalf("expected the unrepresentable attribute to degrade to its rendering, got %#v", attributes["handlerHook"])
    }

    /* the list must stay a list: folding every value to text would have been the cheaper repair and the wrong one */
    roles, rolesAreList := attributes["allowedRoles"].([]any)
    if false == rolesAreList || 2 != len(roles) {
        t.Fatalf("expected the serializable list to keep its type, got %#v", attributes["allowedRoles"])
    }

    if true != attributes["requiresLogin"] {
        t.Fatalf("expected the serializable boolean to keep its type, got %#v", attributes["requiresLogin"])
    }
}

/* a self-referential attribute reaches json.Marshal, which answers a cycle error, which used to route the value into the %v fallback — and fmt has no cycle detection, so the command died of a stack overflow no recover in the command layer turns into a reported failure. The walk that command_container.go already carries replaces the cycle with its marker, and the report survives. */
func TestRouterCommand_ACyclicAttributeIsRenderedAsAMarkerRatherThanKillingTheProcess(t *testing.T) {
    cyclicAttribute := map[string]any{"name": "self-referential"}
    cyclicAttribute["self"] = cyclicAttribute

    router := http.NewRouter()
    router.HandleWithOptions(
        "/probe",
        func(
            runtimeInstance runtimecontract.Runtime,
            writer nethttp.ResponseWriter,
            request httpcontract.Request,
        ) (httpcontract.Response, error) {
            return nil, nil
        },
        http.NewRouteOptions(
            "probe.route",
            []string{nethttp.MethodGet},
            "",
            nil,
            nil,
            nil,
            nil,
            0,
            map[string]any{
                "meta": cyclicAttribute,
            },
        ),
    )

    serviceContainer := container.NewContainer()
    serviceContainer.MustRegister(
        http.ServiceRouter,
        func(resolver containercontract.Resolver) (httpcontract.Router, error) {
            return router, nil
        },
    )

    rendered, runErr := runDebugCommand(
        &RouterCommand{},
        newTestRuntime(serviceContainer),
        []string{"--format=json"},
    )
    if nil != runErr {
        t.Fatalf("expected the document to be produced, got %v", runErr)
    }

    decoded := struct {
        Data struct {
            Items []struct {
                Attributes map[string]any `json:"attributes"`
            } `json:"items"`
        } `json:"data"`
    }{}
    if decodeErr := json.Unmarshal([]byte(rendered), &decoded); nil != decodeErr {
        t.Fatalf("failed to decode the rendered envelope: %v, rendered %q", decodeErr, rendered)
    }

    if 1 != len(decoded.Data.Items) {
        t.Fatalf("expected the one route, got %d in %q", len(decoded.Data.Items), rendered)
    }

    meta, metaIsMap := decoded.Data.Items[0].Attributes["meta"].(map[string]any)
    if false == metaIsMap {
        t.Fatalf("expected the cyclic attribute to stay a map, got %#v", decoded.Data.Items[0].Attributes["meta"])
    }

    if "self-referential" != meta["name"] {
        t.Fatalf("expected the rest of the attribute to survive the walk, got %#v", meta)
    }

    if errorContextCycleMarker != meta["self"] {
        t.Fatalf("expected the cycle to be rendered as %q, got %#v", errorContextCycleMarker, meta["self"])
    }
}
