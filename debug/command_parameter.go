package debug

import (
    "fmt"
    "sort"
    "strings"
    "time"

    clicontract "github.com/precision-soft/melody/cli/contract"
    "github.com/precision-soft/melody/cli/output"
    "github.com/precision-soft/melody/config"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
)

const (
    redactedValuePlaceholder = "********"
    redactedEmptyPlaceholder = "(empty)"
)

type ParameterCommand struct {
}

func (instance *ParameterCommand) Name() string {
    return "debug:parameters"
}

func (instance *ParameterCommand) Description() string {
    return "List all kernel parameters (raw + resolved)"
}

func (instance *ParameterCommand) Flags() []clicontract.Flag {
    return output.DebugFlags()
}

func (instance *ParameterCommand) Run(
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

    applicationConfiguration := config.ConfigMustFromContainer(runtimeInstance.Container())

    names := applicationConfiguration.Names()
    if 0 == len(names) {
        if output.FormatTable == option.Format {
            builder := output.NewTableBuilder()
            builder.AddSummaryLine("PARAMETERS: 0 total")
            envelope.Table = builder.Build()
        } else {
            envelope.Data = output.NewListPayload(
                []parameterListItem{},
                0,
                option.Limit,
                option.Offset,
            )
        }

        envelope.Meta.DurationMilliseconds = time.Since(startedAt).Milliseconds()

        return output.Render(commandContext.Writer, envelope, option)
    }

    keys := make([]string, 0, len(names))
    for _, name := range names {
        keys = append(keys, name)
    }

    sort.Strings(keys)

    environmentAliases := make(map[string][]string)

    for _, name := range keys {
        parameter := applicationConfiguration.Get(name)
        if nil == parameter {
            continue
        }

        environmentKey := parameter.EnvironmentKey()
        if "" == environmentKey {
            continue
        }

        aliasNames, exists := environmentAliases[environmentKey]
        if false == exists {
            aliasNames = []string{}
        }

        aliasNames = append(aliasNames, name)

        environmentAliases[environmentKey] = aliasNames
    }

    items := make([]parameterListItem, 0, len(keys))

    for _, key := range keys {
        parameter := applicationConfiguration.Get(key)
        if nil == parameter {
            continue
        }

        aliases := ""

        environmentKey := parameter.EnvironmentKey()
        if "" != environmentKey {
            aliasNames, exists := environmentAliases[environmentKey]

            if true == exists && 1 < len(aliasNames) && key == environmentKey {
                continue
            }

            if true == exists && 0 < len(aliasNames) {
                filteredAliases := make([]string, 0, len(aliasNames))

                for _, aliasName := range aliasNames {
                    if aliasName == key {
                        continue
                    }

                    filteredAliases = append(filteredAliases, aliasName)
                }

                if 0 < len(filteredAliases) {
                    aliases = strings.Join(filteredAliases, ", ")
                }
            }
        }

        isSecret := parameter.IsSecret()

        items = append(
            items,
            parameterListItem{
                Name:             key,
                EnvironmentKey:   parameter.EnvironmentKey(),
                EnvironmentValue: redactedParameterValue(parameter.EnvironmentValue(), isSecret),
                ValueString:      redactedParameterValue(parameter.Value(), isSecret),
                IsDefault:        parameter.IsDefault(),
                IsSecret:         isSecret,
                Aliases:          aliases,
            },
        )
    }

    output.ApplySortOrder(items, option.Order)

    total := len(items)
    items = output.WindowItems(items, option.Limit, option.Offset)

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
            []string{"parameter", "environmentKey", "environmentValue", "value", "default", "secret", "aliases"},
        )

        for _, item := range items {
            block.AddRow(
                item.Name,
                item.EnvironmentKey,
                item.EnvironmentValue,
                item.ValueString,
                fmt.Sprintf("%t", item.IsDefault),
                fmt.Sprintf("%t", item.IsSecret),
                item.Aliases,
            )
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

    return output.Render(commandContext.Writer, envelope, option)
}

/* redactedParameterValue keeps a parameter declared as a secret out of the rendered output while still reporting whether it carries a value at all, which is what an operator runs this command to find out. The length is withheld along with the value: on a short credential it narrows the search meaningfully. */
func redactedParameterValue(value any, isSecret bool) string {
    formattedValue := fmt.Sprintf("%v", value)

    if false == isSecret {
        return formattedValue
    }

    if "" == formattedValue {
        return redactedEmptyPlaceholder
    }

    return redactedValuePlaceholder
}

type parameterListItem struct {
    Name             string `json:"name"`
    EnvironmentKey   string `json:"environmentKey"`
    EnvironmentValue string `json:"environmentValue"`
    ValueString      string `json:"value"`
    IsDefault        bool   `json:"isDefault"`
    IsSecret         bool   `json:"isSecret"`
    Aliases          string `json:"aliases"`
}

var _ clicontract.Command = (*ParameterCommand)(nil)
