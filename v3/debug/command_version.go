package debug

import (
    "time"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/cli/output"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* VersionCommand answers with three rows: the application's version, melody's, and the Go runtime's. The application row reads the process-wide declaration made through output.SetApplicationVersion — the composition root's main is where an application hands its version over, from whatever source it keeps it in — and an explicit ApplicationVersion set on the command wins over it. */
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

        /* the rows read the meta, where NewMeta already applied the fallback chain: the command's explicit value, then the process-wide declaration, then nothing */
        if "" != envelope.Meta.Version.Application {
            block.AddRow("application", envelope.Meta.Version.Application)
        } else {
            block.AddRow("application", "<unknown>")
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
