package debug

import (
    "encoding/json"
    "fmt"
    "sort"
    "strings"
    "time"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/cli/output"
    "github.com/precision-soft/melody/v3/http"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

type RouterCommand struct {
}

func (instance *RouterCommand) Name() string {
    return "debug:router"
}

func (instance *RouterCommand) Description() string {
    return "List all registered HTTP routes"
}

func (instance *RouterCommand) Flags() []clicontract.Flag {
    return output.DebugFlags()
}

func (instance *RouterCommand) Run(
    runtimeInstance runtimecontract.Runtime,
    commandContext clicontract.Context,
) error {
    startedAt := time.Now()

    option := output.NormalizeOption(
        output.ParseOptionFromCommand(commandContext),
    )

    meta := output.NewMeta(
        instance.Name(),
        commandContext.Arguments(),
        option,
        startedAt,
        time.Duration(0),
        output.Version{},
    )

    envelope := output.NewEnvelope(meta)

    router := http.RouterMustFromContainer(runtimeInstance.Container())
    routes := router.RouteDefinitions()

    items := make([]routeListItem, 0, len(routes))

    for index, routeDefinition := range routes {
        methods := strings.Join(routeDefinition.Methods(), ",")
        if "" == methods {
            methods = "ANY"
        }

        name := routeDefinition.Name()
        if "" == name {
            name = "-"
        }

        host := routeDefinition.Host()
        if "" == host {
            host = "*"
        }

        schemes := strings.Join(routeDefinition.Schemes(), ",")
        if "" == schemes {
            schemes = "http,https"
        }

        locales := strings.Join(routeDefinition.Locales(), ",")
        if "" == locales {
            locales = "-"
        }

        items = append(
            items,
            routeListItem{
                Methods: methods,
                Pattern: routeDefinition.Pattern(),
                Name:    name,

                Priority:     routeDefinition.Priority(),
                Order:        index + 1,
                Host:         host,
                Schemes:      schemes,
                Locales:      locales,
                Requirements: routeDefinition.Requirements(),
                Defaults:     routeDefinition.Defaults(),
                Attributes:   serializableRouteAttributes(routeDefinition.Attributes()),
            },
        )
    }

    sort.Slice(items, func(leftIndex int, rightIndex int) bool {
        left := items[leftIndex]
        right := items[rightIndex]

        if left.Pattern == right.Pattern {
            if left.Methods == right.Methods {
                return left.Order < right.Order
            }

            return left.Methods < right.Methods
        }

        return left.Pattern < right.Pattern
    })

    output.ApplySortOrder(items, option.Order)

    total := len(items)
    items = output.WindowItems(items, option.Limit, option.Offset)

    if output.FormatTable == option.Format {
        builder := output.NewTableBuilder()

        summary := fmt.Sprintf(
            "ROUTES: %d total",
            total,
        )

        if len(items) != total {
            summary = fmt.Sprintf(
                "%s | %d shown",
                summary,
                len(items),
            )
        }

        builder.AddSummaryLine(summary)

        columns := []string{"methods", "pattern", "name", "priority", "order", "host", "schemes", "locales"}
        if true == option.Verbose {
            columns = append(columns, "requirements", "defaults", "attributes")
        }

        block := builder.AddBlock(
            "ROUTES",
            columns,
        )

        for _, item := range items {
            cells := []string{
                item.Methods,
                item.Pattern,
                item.Name,
                fmt.Sprintf("%d", item.Priority),
                fmt.Sprintf("%d", item.Order),
                item.Host,
                item.Schemes,
                item.Locales,
            }

            if true == option.Verbose {
                cells = append(
                    cells,
                    renderCompactStringMap(item.Requirements),
                    renderCompactStringMap(item.Defaults),
                    renderCompactAnyMap(item.Attributes),
                )
            }

            block.AddRow(cells...)
        }

        envelope.Table = builder.Build()
    } else {
        envelope.Data = output.NewListPayload(
            items,
            total,
            option.Limit,
            option.Offset,
        )
    }

    envelope.Meta.DurationMilliseconds = time.Since(startedAt).Milliseconds()

    return output.Render(commandContext.Writer(), envelope, option)
}

type routeListItem struct {
    Methods      string            `json:"methods"`
    Pattern      string            `json:"pattern"`
    Name         string            `json:"name"`
    Priority     int               `json:"priority"`
    Order        int               `json:"order"`
    Host         string            `json:"host"`
    Schemes      string            `json:"schemes"`
    Locales      string            `json:"locales"`
    Requirements map[string]string `json:"requirements"`
    Defaults     map[string]string `json:"defaults"`
    Attributes   map[string]any    `json:"attributes"`
}

func renderCompactStringMap(values map[string]string) string {
    if 0 == len(values) {
        return "-"
    }

    keys := make([]string, 0, len(values))
    for key := range values {
        keys = append(keys, key)
    }

    sort.Strings(keys)

    pairs := make([]string, 0, len(keys))
    for _, key := range keys {
        pairs = append(pairs, key+"="+values[key])
    }

    return strings.Join(pairs, ",")
}

func serializableRouteAttributes(values map[string]any) map[string]any {
    if 0 == len(values) {
        return values
    }

    projected := make(map[string]any, len(values))
    for key, value := range values {
        cycleSafeValue := sanitizeErrorContextValueTracked(
            value,
            map[errorContextVisitKey]struct{}{},
            0,
            true,
        )

        if _, marshalErr := json.Marshal(cycleSafeValue); nil != marshalErr {
            projected[key] = fmt.Sprintf("%v", cycleSafeValue)

            continue
        }

        projected[key] = cycleSafeValue
    }

    return projected
}

func renderCompactAnyMap(values map[string]any) string {
    if 0 == len(values) {
        return "-"
    }

    stringValues := make(map[string]string, len(values))
    for key, value := range values {
        stringValues[key] = fmt.Sprintf("%v", value)
    }

    return renderCompactStringMap(stringValues)
}

var _ clicontract.Command = (*RouterCommand)(nil)
