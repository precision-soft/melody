package debug

import (
    "time"

    clicontract "github.com/precision-soft/melody/v4/cli/contract"
    "github.com/precision-soft/melody/v4/cli/output"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
)

/* VersionCommand answers three rows: the application's version, melody's and the Go runtime's. The application row reads ApplicationConfig.Version, declared on the root of the command tree. */
type VersionCommand struct{}

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

    option, normalizeOptionErr := output.NormalizeOption(output.ParseOptionFromCommand(commandContext))
    if nil != normalizeOptionErr {
        return normalizeOptionErr
    }

    meta := output.NewMeta(
        instance.Name(),
        commandContext.Args().Slice(),
        option,
        startedAt,
        time.Duration(0),
        output.Version{},
    )

    envelope := output.NewEnvelope(meta)

    if output.FormatTable == option.Format {
        builder := output.NewTableBuilder()

        builder.AddSummaryLine("VERSIONS")

        block := builder.AddBlock(
            "DETAILS",
            []string{"component", "version"},
        )

        if "" != envelope.Meta.Version.Application {
            block.AddRow("application", envelope.Meta.Version.Application)
        } else {
            block.AddRow("application", output.UnknownApplicationVersion)
        }

        block.AddRow("melody", envelope.Meta.Version.Melody)
        block.AddRow("go", envelope.Meta.Version.Go)

        envelope.Table = builder.Build()
    } else {
        envelope.Data = map[string]string{
            "application": envelope.Meta.Version.Application,
            "melody":      envelope.Meta.Version.Melody,
            "go":          envelope.Meta.Version.Go,
        }
    }

    envelope.Meta.DurationMilliseconds = time.Since(startedAt).Milliseconds()

    return output.Render(commandContext.Writer, envelope, option)
}

var _ clicontract.Command = (*VersionCommand)(nil)
