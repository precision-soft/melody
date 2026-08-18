package cli

import (
    "fmt"
    "strconv"
    "time"

    "github.com/precision-soft/melody/.example/repository"
    melodyclicontract "github.com/precision-soft/melody/cli/contract"
    melodyoutput "github.com/precision-soft/melody/cli/output"
    melodyexception "github.com/precision-soft/melody/exception"
    melodyruntimecontract "github.com/precision-soft/melody/runtime/contract"
)

type CatalogJournalCommand struct{}

func NewCatalogJournalCommand() *CatalogJournalCommand {
    return &CatalogJournalCommand{}
}

func (instance *CatalogJournalCommand) Name() string {
    return "catalog:journal"
}

func (instance *CatalogJournalCommand) Description() string {
    return "prints the most recent changes made to the nomenclature"
}

func (instance *CatalogJournalCommand) Flags() []melodyclicontract.Flag {
    return melodyoutput.StandardFlags()
}

type catalogJournalListItem struct {
    Id         int64  `json:"id"`
    Actor      string `json:"actor"`
    Action     string `json:"action"`
    Subject    string `json:"subject"`
    SubjectId  string `json:"subjectId"`
    RecordedAt string `json:"recordedAt"`
}

/* Run prints what has happened to the nomenclature, newest first; --order desc reads the same entries oldest first. The command is registered whether or not the database is configured, so the command surface does not change between environments; without a database it fails with the reason instead of being quietly absent. */
func (instance *CatalogJournalCommand) Run(runtimeInstance melodyruntimecontract.Runtime, commandContext *melodyclicontract.CommandContext) error {
    if false == runtimeInstance.Container().Has(repository.ServiceCatalogJournalRepository) {
        return melodyexception.NewError(
            "the example has no journal database configured, so no journal of the changes is kept",
            nil,
            nil,
        )
    }

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

    journalRepository := repository.MustGetCatalogJournalRepository(runtimeInstance.Container())

    /* the repository is asked for everything and the standard window is applied here, so --limit and --offset behave exactly as they do on every other enveloped command */
    entries, latestErr := journalRepository.Latest(runtimeInstance.Context(), 0)
    if nil != latestErr {
        return latestErr
    }

    items := make([]catalogJournalListItem, 0, len(entries))

    for _, entry := range entries {
        if nil == entry {
            continue
        }

        items = append(items, catalogJournalListItem{
            Id:         entry.Id,
            Actor:      entry.Actor,
            Action:     entry.Action,
            Subject:    entry.Subject,
            SubjectId:  entry.SubjectId,
            RecordedAt: entry.RecordedAt.UTC().Format(time.RFC3339),
        })
    }

    melodyoutput.ApplySortOrder(items, option.Order)

    total := len(items)
    items = melodyoutput.WindowItems(items, option.Limit, option.Offset)

    if melodyoutput.FormatTable == option.Format {
        builder := melodyoutput.NewTableBuilder()

        summary := fmt.Sprintf("JOURNAL: %d total", total)
        if len(items) != total {
            summary = fmt.Sprintf("%s | %d shown", summary, len(items))
        }
        builder.AddSummaryLine(summary)

        block := builder.AddBlock(
            "JOURNAL",
            []string{"ID", "ACTOR", "ACTION", "SUBJECT", "SUBJECT_ID", "RECORDED_AT"},
        )

        for _, item := range items {
            block.AddRow(
                strconv.FormatInt(item.Id, 10),
                item.Actor,
                item.Action,
                item.Subject,
                item.SubjectId,
                item.RecordedAt,
            )
        }

        envelope.Table = builder.Build()
    } else {
        envelope.Data = melodyoutput.NewListPayload(
            items,
            total,
            option.Limit,
            option.Offset,
        )
    }

    envelope.Meta.DurationMilliseconds = time.Since(startedAt).Milliseconds()

    return melodyoutput.Render(commandContext.Writer, envelope, option)
}

var _ melodyclicontract.Command = (*CatalogJournalCommand)(nil)
