package cli

import (
    "strconv"
    "time"

    "github.com/precision-soft/melody/.example/repository"
    melodyclicontract "github.com/precision-soft/melody/cli/contract"
    melodyclock "github.com/precision-soft/melody/clock"
    melodyexception "github.com/precision-soft/melody/exception"
    melodyruntimecontract "github.com/precision-soft/melody/runtime/contract"
)

type CatalogNoteCommand struct{}

func NewCatalogNoteCommand() *CatalogNoteCommand {
    return &CatalogNoteCommand{}
}

func (instance *CatalogNoteCommand) Name() string {
    return "catalog:note"
}

func (instance *CatalogNoteCommand) Description() string {
    return "appends a catalogue note to the database and prints the latest ones"
}

func (instance *CatalogNoteCommand) Flags() []melodyclicontract.Flag {
    return []melodyclicontract.Flag{
        &melodyclicontract.IntFlag{
            Name:  "limit",
            Usage: "how many notes to print",
            Value: 5,
        },
    }
}

/* Run writes one note and prints the most recent ones. The command is registered whether or not the database is configured, so the command surface does not change between environments; without a database it fails with the reason instead of being quietly absent. */
func (instance *CatalogNoteCommand) Run(runtimeInstance melodyruntimecontract.Runtime, commandContext *melodyclicontract.CommandContext) error {
    if false == runtimeInstance.Container().Has(repository.ServiceCatalogNoteRepository) {
        return melodyexception.NewError(
            "the example has no database configured, so there is nowhere to keep a catalogue note",
            nil,
            nil,
        )
    }

    noteRepository := repository.MustGetCatalogNoteRepository(runtimeInstance.Container())
    clockInstance := melodyclock.ClockMustFromResolver(runtimeInstance.Container())

    ctx := runtimeInstance.Context()

    ensureSchemaErr := noteRepository.EnsureSchema(ctx)
    if nil != ensureSchemaErr {
        return ensureSchemaErr
    }

    _, appendErr := noteRepository.Append(ctx, "catalogue note from the command line", clockInstance.Now().UTC().Truncate(time.Second))
    if nil != appendErr {
        return appendErr
    }

    notes, latestErr := noteRepository.Latest(ctx, commandContext.Int("limit"))
    if nil != latestErr {
        return latestErr
    }

    headers := []string{
        "ID",
        "NOTE",
        "RECORDED_AT",
    }

    rows := make([][]string, 0, len(notes))
    for _, note := range notes {
        if nil == note {
            continue
        }

        rows = append(rows, []string{
            strconv.FormatInt(note.Id, 10),
            note.Note,
            note.RecordedAt.UTC().Format(time.RFC3339),
        })
    }

    /* the same table helper product:list prints through, so both commands render alike */
    printTable(headers, rows)

    return nil
}

var _ melodyclicontract.Command = (*CatalogNoteCommand)(nil)
