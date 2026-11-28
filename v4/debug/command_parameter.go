package debug

import (
    "fmt"
    "time"

    clicontract "github.com/precision-soft/melody/v4/cli/contract"
    "github.com/precision-soft/melody/v4/cli/output"
    "github.com/precision-soft/melody/v4/config"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal"
)

const (
    redactedValuePlaceholder = "********"
    redactedEmptyPlaceholder = "(empty)"
)

/* ParameterCommandRevealFlagName is the flag that prints a secret parameter's value in clear, refused outside the development environment */
const ParameterCommandRevealFlagName = "reveal"

type ParameterCommand struct {
}

func (instance *ParameterCommand) Name() string {
    return "debug:parameters"
}

func (instance *ParameterCommand) Description() string {
    return "List all kernel parameters (raw + resolved)"
}

func (instance *ParameterCommand) Flags() []clicontract.Flag {
    return append(
        output.DebugFlags(),
        &clicontract.BoolFlag{
            Name:  ParameterCommandRevealFlagName,
            Usage: "print the value of a secret parameter in clear, in the development environment only",
            Value: false,
        },
    )
}

func (instance *ParameterCommand) Run(
    runtimeInstance containercontract.Runtime,
    commandContext *clicontract.CommandContext,
) error {
    startedAt := time.Now()

    option, normalizeOptionErr := output.NormalizeOptions(output.ParseOptionsFromCommand(commandContext))
    if nil != normalizeOptionErr {
        return normalizeOptionErr
    }

    meta := output.NewMeta(
        instance.Name(),
        output.MetaConfig{
            Arguments: commandContext.Args().Slice(),
            Options:    option,
            StartedAt: startedAt,
        },
    )

    envelope := output.NewEnvelope(meta)

    applicationConfiguration := config.ConfigMustFromRuntime(runtimeInstance)

    reveal := commandContext.Bool(ParameterCommandRevealFlagName)
    if true == reveal && config.EnvironmentDevelopment != applicationConfiguration.Kernel().Environment() {
        return exception.NewError(
            "debug:parameters --reveal is refused outside the development environment",
            map[string]any{
                "flag":                ParameterCommandRevealFlagName,
                "environment":         applicationConfiguration.Kernel().Environment(),
                "requiredEnvironment": config.EnvironmentDevelopment,
            },
            nil,
        )
    }

    names := applicationConfiguration.Names()
    if 0 == len(names) {
        if output.FormatTable == option.Format {
            builder := output.NewTableBuilder()
            builder.AddSummaryLine("PARAMETERS: 0 total")
            envelope.Table = builder.Build()
        } else {
            envelope.Data = output.NewListPayload(
                []parameterListItem{},
                output.ListPayloadConfig{
                    Limit:  option.Limit,
                    Offset: option.Offset,
                },
            )
        }

        envelope.Meta.DurationMilliseconds = time.Since(startedAt).Milliseconds()

        return output.Render(commandContext.Writer, envelope, option)
    }

    items := make([]parameterListItem, 0, len(names))

    for _, name := range names {
        parameter := applicationConfiguration.Get(name)
        if true == internal.IsNilInterface(parameter) {
            continue
        }

        isSecret := parameter.IsSecret()
        masked := true == isSecret && false == reveal

        items = append(
            items,
            parameterListItem{
                Name:             name,
                EnvironmentKey:   parameter.EnvironmentKey(),
                Template:         redactedParameterValue(parameter.Template(), masked),
                ValueString:      redactedParameterValue(parameter.Value(), masked),
                IsDefault:        parameter.IsDefault(),
                IsOverridden:     parameter.IsOverridden(),
                IsSecret:         isSecret,
            },
        )
    }

    output.ApplySortOrder(items, option.Order)

    total := len(items)
    windowed, windowItemsErr := output.WindowItems(items, option.Limit, option.Offset)
    if nil != windowItemsErr {
        return windowItemsErr
    }

    items = windowed

    if output.FormatTable == option.Format {
        builder := output.NewTableBuilder()

        summary := fmt.Sprintf(
            "PARAMETERS: %d total",
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
            "PARAMETERS",
            []string{"parameter", "environmentKey", "template", "value", "default", "overridden", "secret"},
        )

        for _, item := range items {
            block.AddRow([]string{
                item.Name,
                item.EnvironmentKey,
                item.Template,
                item.ValueString,
                fmt.Sprintf("%t", item.IsDefault),
                fmt.Sprintf("%t", item.IsOverridden),
                fmt.Sprintf("%t", item.IsSecret),
            })
        }

        envelope.Table = builder.Build()
    } else {
        envelope.Data = output.NewListPayload(
            items,
            output.ListPayloadConfig{
                Total:  total,
                Limit:  option.Limit,
                Offset: option.Offset,
            },
        )
    }

    envelope.Meta.DurationMilliseconds = time.Since(startedAt).Milliseconds()

    return output.Render(commandContext.Writer, envelope, option)
}

/* redactedParameterValue keeps a secret parameter's value, and its length, out of the output while reporting whether it carries one. */
func redactedParameterValue(value any, isSecret bool) string {
    formattedValue := fmt.Sprintf("%v", value)

    if false == isSecret {
        return formattedValue
    }

    /* a nil value carries nothing, though fmt would render a placeholder */
    if true == internal.IsNilInterface(value) {
        return redactedEmptyPlaceholder
    }

    if "" == formattedValue {
        return redactedEmptyPlaceholder
    }

    return redactedValuePlaceholder
}

type parameterListItem struct {
    Name             string `json:"name"`
    EnvironmentKey   string `json:"environmentKey"`
    Template         string `json:"template"`
    ValueString      string `json:"value"`
    IsDefault        bool   `json:"isDefault"`
    IsOverridden     bool   `json:"isOverridden"`
    IsSecret         bool   `json:"isSecret"`
}

var _ clicontract.Command = (*ParameterCommand)(nil)
