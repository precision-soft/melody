package debug

import (
    "time"

    clicontract "github.com/precision-soft/melody/v2/cli/contract"
    "github.com/precision-soft/melody/v2/cli/output"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
    melodyversion "github.com/precision-soft/melody/v2/version"
)

type VersionCommand struct {
    ApplicationVersion string
}

func (instance *VersionCommand) Name() string {
    return "debug:version"
}

func (instance *VersionCommand) Description() string {
    return "Display application, Melody, and Go runtime versions"
}

func (instance *VersionCommand) Flags() []clicontract.Flag {
    return output.DebugFlags()
}

func (instance *VersionCommand) Run(
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
        output.Version{
            Application: instance.ApplicationVersion,
            Melody:      melodyversion.BuildVersion(),
        },
    )

    envelope := output.NewEnvelope(meta)

    if output.FormatTable == option.Format {
        builder := output.NewTableBuilder()

        builder.AddSummaryLine("VERSIONS")

        block := builder.AddBlock(
            "DETAILS",
            []string{"component", "version"},
        )

        if "" != instance.ApplicationVersion {
            block.AddRow("application", instance.ApplicationVersion)
        } else {
            block.AddRow("application", "<unknown>")
        }

        block.AddRow("melody", melodyversion.BuildVersion())
        block.AddRow("go", envelope.Meta.Version.Go)

        envelope.Table = builder.Build()
    } else {
        envelope.Data = map[string]string{
            "application": instance.ApplicationVersion,
            "melody":      melodyversion.BuildVersion(),
            "go":          envelope.Meta.Version.Go,
        }
    }

    envelope.Meta.DurationMilliseconds = time.Since(startedAt).Milliseconds()

    return output.Render(commandContext.Writer, envelope, option)
}

var _ clicontract.Command = (*VersionCommand)(nil)
