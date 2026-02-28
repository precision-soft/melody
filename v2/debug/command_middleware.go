package debug

import (
    "fmt"
    "reflect"
    runtimepkg "runtime"
    "sort"
    "time"

    clicontract "github.com/precision-soft/melody/v2/cli/contract"
    "github.com/precision-soft/melody/v2/cli/output"
    httpcontract "github.com/precision-soft/melody/v2/http/contract"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

type MiddlewareProvider func() []httpcontract.Middleware

func NewMiddlewareCommand(middlewareProvider MiddlewareProvider) *MiddlewareCommand {
    return &MiddlewareCommand{
        middlewareProvider: middlewareProvider,
    }
}

type MiddlewareCommand struct {
    middlewareProvider MiddlewareProvider
}

func (instance *MiddlewareCommand) Name() string {
    return "debug:middleware"
}

func (instance *MiddlewareCommand) Description() string {
    return "list http middleware in registration order"
}

func (instance *MiddlewareCommand) Flags() []clicontract.Flag {
    return output.DebugFlags()
}

func (instance *MiddlewareCommand) Run(
    _ runtimecontract.Runtime,
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

    middlewares := instance.middlewareProvider()

    items := make([]middlewareListItem, 0, len(middlewares))
    for index, middleware := range middlewares {
        name := "<nil>"
        if nil != middleware {
            name = middlewareFunctionName(middleware)
        }

        items = append(
            items,
            middlewareListItem{
                Index: index + 1,
                Name:  name,
            },
        )
    }

    sort.Slice(items, func(leftIndex int, rightIndex int) bool {
        return items[leftIndex].Index < items[rightIndex].Index
    })

    if output.FormatTable == option.Format {
        builder := output.NewTableBuilder()

        builder.AddSummaryLine(
            fmt.Sprintf(
                "MIDDLEWARE: %d total",
                len(items),
            ),
        )

        block := builder.AddBlock(
            "MIDDLEWARE",
            []string{"index", "middleware"},
        )

        for _, item := range items {
            block.AddRow(
                fmt.Sprintf("%d", item.Index),
                item.Name,
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

type middlewareListItem struct {
    Index int    `json:"index"`
    Name  string `json:"name"`
}

func middlewareFunctionName(middleware httpcontract.Middleware) string {
    value := reflect.ValueOf(middleware)
    if reflect.Func != value.Kind() {
        return "<unknown>"
    }

    pointer := value.Pointer()
    if 0 == pointer {
        return "<unknown>"
    }

    function := runtimepkg.FuncForPC(pointer)
    if nil == function {
        return "<unknown>"
    }

    return function.Name()
}

var _ clicontract.Command = (*MiddlewareCommand)(nil)
