package debug

import (
	"fmt"
	"sort"
	"strings"
	"time"

	clicontract "github.com/precision-soft/melody/cli/contract"
	"github.com/precision-soft/melody/cli/output"
	"github.com/precision-soft/melody/http"
	runtimecontract "github.com/precision-soft/melody/runtime/contract"
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
	commandContext *clicontract.CommandContext,
) error {
	startedAt := time.Now()

	option := output.NormalizeOption(
		output.ParseOptionFromCommand(commandContext),
	)

	meta := output.NewMeta(
		instance.Name(),
		commandContext.Args().Slice(),
		option,
		startedAt,
		time.Duration(0),
		output.Version{},
	)

	envelope := output.NewEnvelope(meta)

	router := http.RouterMustFromContainer(runtimeInstance.Container())
	routes := router.RouteDefinitions()

	items := make([]routeListItem, 0, len(routes))

	for _, routeDefinition := range routes {
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
				Host:    host,
				Schemes: schemes,
				Locales: locales,
			},
		)
	}

	sort.Slice(items, func(leftIndex int, rightIndex int) bool {
		left := items[leftIndex]
		right := items[rightIndex]

		if left.Pattern == right.Pattern {
			return left.Methods < right.Methods
		}

		return left.Pattern < right.Pattern
	})

	if output.FormatTable == option.Format {
		builder := output.NewTableBuilder()

		builder.AddSummaryLine(
			fmt.Sprintf(
				"ROUTES: %d total",
				len(items),
			),
		)

		block := builder.AddBlock(
			"ROUTES",
			[]string{"methods", "pattern", "name", "host", "schemes", "locales"},
		)

		for _, item := range items {
			block.AddRow(
				item.Methods,
				item.Pattern,
				item.Name,
				item.Host,
				item.Schemes,
				item.Locales,
			)
		}

		envelope.Table = builder.Build()
	} else {
		envelope.Data = output.NewListPayload(
			items,
			len(items),
			option.Limit,
			option.Offset,
		)
	}

	envelope.Meta.DurationMilliseconds = time.Since(startedAt).Milliseconds()

	return output.Render(commandContext.Writer, envelope, option)
}

type routeListItem struct {
	Methods string `json:"methods"`
	Pattern string `json:"pattern"`
	Name    string `json:"name"`
	Host    string `json:"host"`
	Schemes string `json:"schemes"`
	Locales string `json:"locales"`
}

var _ clicontract.Command = (*RouterCommand)(nil)
