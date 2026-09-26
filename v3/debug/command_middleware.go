package debug

import (
    "fmt"
    "reflect"
    runtimepkg "runtime"
    "sort"
    "time"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/cli/output"
    "github.com/precision-soft/melody/v3/exception"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    middlewarepipeline "github.com/precision-soft/melody/v3/http/middleware/pipeline"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* MiddlewareDescriptionProvider answers what the http pipeline would run without building it: the ordered descriptions, the inactive entries with their reasons, or the refusal the build would answer. */
type MiddlewareDescriptionProvider func() ([]middlewarepipeline.MiddlewareDescription, *middlewarepipeline.MiddlewareBuildReport, error)

/* MiddlewareBuildProvider runs the real build for --build and answers a failure as an error; the command recovers a factory panic around the call. */
type MiddlewareBuildProvider func() ([]httpcontract.Middleware, error)

func NewMiddlewareCommand(
    descriptionProvider MiddlewareDescriptionProvider,
    buildProvider MiddlewareBuildProvider,
) *MiddlewareCommand {
    if nil == descriptionProvider {
        exception.Panic(
            exception.NewError("middleware command created with nil description provider", nil, nil),
        )
    }

    if nil == buildProvider {
        exception.Panic(
            exception.NewError("middleware command created with nil build provider", nil, nil),
        )
    }

    return &MiddlewareCommand{
        descriptionProvider: descriptionProvider,
        buildProvider:       buildProvider,
    }
}

type MiddlewareCommand struct {
    descriptionProvider MiddlewareDescriptionProvider
    buildProvider       MiddlewareBuildProvider
}

func (instance *MiddlewareCommand) Name() string {
    return "debug:middleware"
}

func (instance *MiddlewareCommand) Description() string {
    return "list http middleware in pipeline order"
}

const middlewareCommandBuildFlagName = "build"

func (instance *MiddlewareCommand) Flags() []clicontract.Flag {
    return append(
        output.DebugFlags(),
        &clicontract.BoolFlag{
            Name:  middlewareCommandBuildFlagName,
            Usage: "build the middleware chain and report a failing factory with its cause",
            Value: false,
        },
    )
}

func (instance *MiddlewareCommand) Run(
    _ runtimecontract.Runtime,
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

    /* the zero value is constructible without providers; the refusal travels through the envelope, so the document is written before the command fails */
    if nil == instance.descriptionProvider || nil == instance.buildProvider {
        envelope.SetError("debug.providerNil", "middleware provider is nil", nil, nil)
        envelope.Meta.DurationMilliseconds = time.Since(startedAt).Milliseconds()

        return output.Render(commandContext.Writer(), envelope, option)
    }

    if true == commandContext.Bool(middlewareCommandBuildFlagName) {
        instance.populateBuiltChain(option, &envelope)
    } else {
        instance.populateDescription(option, &envelope)
    }

    envelope.Meta.DurationMilliseconds = time.Since(startedAt).Milliseconds()

    return output.Render(commandContext.Writer(), envelope, option)
}

/* middlewareListItem is one shape for the three documents, and the reason field is always present, empty for an active row. A --build row carries index, function and status only, since the built chain carries nothing else. */
type middlewareListItem struct {
    Index    int    `json:"index"`
    Name     string `json:"name"`
    Priority int    `json:"priority"`
    Function string `json:"function"`
    Status   string `json:"status"`
    Reason   string `json:"reason"`
}

/* populateDescription is the default listing: nothing is built and no factory runs. */
func (instance *MiddlewareCommand) populateDescription(
    option output.Option,
    envelope *output.Envelope,
) {
    descriptions, report, describeErr := instance.descriptionProvider()
    if nil != describeErr {
        envelope.SetError(
            "debug.describeFailed",
            "middleware pipeline cannot be assembled",
            nil,
            output.NewErrorCause(describeErr.Error(), nil),
        )
    }

    items := make([]middlewareListItem, 0, len(descriptions))
    for index, description := range descriptions {
        functionName := description.FunctionName
        if "" == functionName {
            functionName = "-"
        }

        items = append(
            items,
            middlewareListItem{
                Index:    index + 1,
                Name:     description.Name,
                Priority: description.Priority,
                Function: functionName,
                Status:   "active",
            },
        )
    }

    if nil != report {
        inactive := report.Inactive()

        /* the reason makes the comparator total over same-name inactive entries */
        sort.Slice(inactive, func(leftIndex int, rightIndex int) bool {
            if inactive[leftIndex].Name() == inactive[rightIndex].Name() {
                return inactive[leftIndex].Reason() < inactive[rightIndex].Reason()
            }

            return inactive[leftIndex].Name() < inactive[rightIndex].Name()
        })

        for _, inactiveMiddleware := range inactive {
            items = append(
                items,
                middlewareListItem{
                    Name:     inactiveMiddleware.Name(),
                    Function: "-",
                    Status:   "inactive",
                    Reason:   inactiveMiddleware.Reason(),
                },
            )
        }
    }

    output.ApplySortOrder(items, option.Order)

    total := len(items)
    items = output.WindowItems(items, option.Limit, option.Offset)

    if output.FormatTable == option.Format {
        builder := output.NewTableBuilder()

        summary := fmt.Sprintf(
            "MIDDLEWARE: %d total",
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

        block := builder.AddBlock(
            "MIDDLEWARE",
            []string{"index", "name", "priority", "function", "status", "reason"},
        )

        for _, item := range items {
            indexCell := "-"
            if 0 < item.Index {
                indexCell = fmt.Sprintf("%d", item.Index)
            }

            block.AddRow(
                indexCell,
                item.Name,
                fmt.Sprintf("%d", item.Priority),
                item.Function,
                item.Status,
                item.Reason,
            )
        }

        envelope.Table = builder.Build()

        return
    }

    envelope.Data = output.NewListPayload(
        items,
        total,
        option.Limit,
        option.Offset,
    )
}

/* populateBuiltChain runs the real build under a recover, so a panicking factory is rendered as a failure. */
func (instance *MiddlewareCommand) populateBuiltChain(
    option output.Option,
    envelope *output.Envelope,
) {
    middlewares, buildErr := instance.runBuildProviderRecovered()

    if nil != buildErr {
        envelope.SetError(
            "debug.buildFailed",
            "middleware chain failed to build",
            nil,
            output.NewErrorCause(
                buildErr.Error(),
                map[string]any{
                    /* built from the failure with the head dropped, so a joined failure keeps its causes */
                    "causeChain": resolveErrorCauseChain(buildErr),
                },
            ),
        )
    }

    items := make([]middlewareListItem, 0, len(middlewares))
    for index, middlewareValue := range middlewares {
        functionName := "<nil>"
        if nil != middlewareValue {
            functionName = middlewareFunctionName(middlewareValue)
        }

        items = append(
            items,
            middlewareListItem{
                Index:    index + 1,
                Function: functionName,
                Status:   "built",
            },
        )
    }

    output.ApplySortOrder(items, option.Order)

    total := len(items)
    items = output.WindowItems(items, option.Limit, option.Offset)

    if output.FormatTable == option.Format {
        builder := output.NewTableBuilder()

        summary := fmt.Sprintf(
            "MIDDLEWARE: %d total",
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

        block := builder.AddBlock(
            "MIDDLEWARE",
            []string{"index", "middleware"},
        )

        for _, item := range items {
            block.AddRow(
                fmt.Sprintf("%d", item.Index),
                item.Function,
            )
        }

        envelope.Table = builder.Build()

        return
    }

    envelope.Data = output.NewListPayload(
        items,
        total,
        option.Limit,
        option.Offset,
    )
}

func (instance *MiddlewareCommand) runBuildProviderRecovered() (middlewares []httpcontract.Middleware, buildErr error) {
    defer func() {
        recovered := recover()
        if nil == recovered {
            return
        }

        middlewares = nil
        /* the recovered value travels in the message, which is what the envelope cause renders */
        buildErr = exception.NewError(
            fmt.Sprintf("middleware build panicked: %v", recovered),
            nil,
            nil,
        )
    }()

    return instance.buildProvider()
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

