package cli

import (
    "fmt"
    "time"

    melodyclicontract "github.com/precision-soft/melody/cli/contract"
    melodyoutput "github.com/precision-soft/melody/cli/output"
    melodyconfig "github.com/precision-soft/melody/config"
    melodyhttp "github.com/precision-soft/melody/http"
    melodyruntimecontract "github.com/precision-soft/melody/runtime/contract"
)

type AppInfoCommand struct{}

func NewAppInfoCommand() *AppInfoCommand {
    return &AppInfoCommand{}
}

func (instance *AppInfoCommand) Name() string {
    return "app:info"
}

func (instance *AppInfoCommand) Description() string {
    return "prints application information"
}

func (instance *AppInfoCommand) Flags() []melodyclicontract.Flag {
    return melodyoutput.StandardFlags()
}

type appInfoPayload struct {
    Env             string `json:"env"`
    HttpAddress     string `json:"httpAddress"`
    PublicDir       string `json:"publicDir"`
    StaticIndexFile string `json:"staticIndexFile"`
    Routes          int    `json:"routes"`
    Services        int    `json:"services"`
}

func (instance *AppInfoCommand) Run(runtimeInstance melodyruntimecontract.Runtime, commandContext *melodyclicontract.CommandContext) error {
    startedAt := time.Now()

    option := melodyoutput.NormalizeOption(
        melodyoutput.ParseOptionFromCommand(commandContext),
    )

    meta := melodyoutput.NewMeta(
        instance.Name(),
        commandContext.Args().Slice(),
        option,
        startedAt,
        time.Duration(0),
        melodyoutput.Version{},
    )

    envelope := melodyoutput.NewEnvelope(meta)

    configuration := melodyconfig.ConfigMustFromContainer(runtimeInstance.Container())
    router := melodyhttp.RouterMustFromContainer(runtimeInstance.Container())
    container := runtimeInstance.Container()

    payload := appInfoPayload{
        Env:             configuration.Kernel().Env(),
        HttpAddress:     configuration.Http().Address(),
        PublicDir:       configuration.Http().PublicDir(),
        StaticIndexFile: configuration.Http().StaticIndexFile(),
        Routes:          len(router.RouteDefinitions()),
        Services:        len(container.Names()),
    }

    if melodyoutput.FormatTable == option.Format {
        builder := melodyoutput.NewTableBuilder()
        for _, line := range appInfoSummaryLines(payload) {
            builder.AddSummaryLine(line)
        }

        envelope.Table = builder.Build()
    } else {
        envelope.Data = payload
    }

    envelope.Meta.DurationMilliseconds = time.Since(startedAt).Milliseconds()

    return melodyoutput.Render(commandContext.Writer, envelope, option)
}

/* appInfoSummaryLines renders the values as summary lines rather than a table block: each stays a `name: value` line, which is the shape the live band greps for http_address and the routes count. */
func appInfoSummaryLines(payload appInfoPayload) []string {
    return []string{
        fmt.Sprintf("env: %s", payload.Env),
        fmt.Sprintf("http_address: %s", payload.HttpAddress),
        fmt.Sprintf("public_dir: %s", payload.PublicDir),
        fmt.Sprintf("static_index_file: %s", payload.StaticIndexFile),
        fmt.Sprintf("routes: %d", payload.Routes),
        fmt.Sprintf("services: %d", payload.Services),
    }
}

var _ melodyclicontract.Command = (*AppInfoCommand)(nil)
