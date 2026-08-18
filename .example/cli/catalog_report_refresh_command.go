package cli

import (
    "time"

    "github.com/precision-soft/melody/.example/service"
    melodyclicontract "github.com/precision-soft/melody/cli/contract"
    melodyoutput "github.com/precision-soft/melody/cli/output"
    melodyruntimecontract "github.com/precision-soft/melody/runtime/contract"
)

type CatalogReportRefreshCommand struct{}

func NewCatalogReportRefreshCommand() *CatalogReportRefreshCommand {
    return &CatalogReportRefreshCommand{}
}

func (instance *CatalogReportRefreshCommand) Name() string {
    return "catalog:report:refresh"
}

func (instance *CatalogReportRefreshCommand) Description() string {
    return "takes a fresh reading of the catalogue and leaves it in the cache"
}

func (instance *CatalogReportRefreshCommand) Flags() []melodyclicontract.Flag {
    return melodyoutput.StandardFlags()
}

type catalogReportPayload struct {
    RecordedAt string `json:"recordedAt"`
    Payload    string `json:"payload"`
}

/* Run is what the schedule calls. The report is cheap enough to compute inside a request, but the request that finds a cold cache is the one that pays for it, so the catalogue is read on a timer instead and every request finds a warm answer.

Without a cache backend the reading is still taken and printed: the command reports the state of the catalogue whether or not there is anywhere to leave the answer. */
func (instance *CatalogReportRefreshCommand) Run(runtimeInstance melodyruntimecontract.Runtime, commandContext *melodyclicontract.CommandContext) error {
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

    reportService := service.MustGetCatalogReportService(runtimeInstance.Container())

    report, refreshErr := reportService.Refresh(runtimeInstance.Context())
    if nil != refreshErr {
        return refreshErr
    }

    payload := catalogReportPayload{
        RecordedAt: report.RecordedAt.UTC().Format(time.RFC3339),
        Payload:    report.Payload,
    }

    if melodyoutput.FormatTable == option.Format {
        builder := melodyoutput.NewTableBuilder()

        block := builder.AddBlock(
            "REPORT",
            []string{"RECORDED_AT", "PAYLOAD"},
        )
        block.AddRow(payload.RecordedAt, payload.Payload)

        envelope.Table = builder.Build()
    } else {
        envelope.Data = payload
    }

    envelope.Meta.DurationMilliseconds = time.Since(startedAt).Milliseconds()

    return melodyoutput.Render(commandContext.Writer, envelope, option)
}

var _ melodyclicontract.Command = (*CatalogReportRefreshCommand)(nil)
