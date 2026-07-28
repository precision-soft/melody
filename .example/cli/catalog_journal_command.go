package cli

import (
    "strconv"
    "time"

    "github.com/precision-soft/melody/.example/repository"
    melodyclicontract "github.com/precision-soft/melody/cli/contract"
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
    return []melodyclicontract.Flag{
        &melodyclicontract.IntFlag{
            Name:  "limit",
            Usage: "how many entries to print",
            Value: 10,
        },
    }
}

/* Run prints what has happened to the nomenclature, most recent first. The command is registered whether or not the database is configured, so the command surface does not change between environments; without a database it fails with the reason instead of being quietly absent. */
func (instance *CatalogJournalCommand) Run(runtimeInstance melodyruntimecontract.Runtime, commandContext *melodyclicontract.CommandContext) error {
    if false == runtimeInstance.Container().Has(repository.ServiceCatalogJournalRepository) {
        return melodyexception.NewError(
            "the example has no database configured, so no journal of the changes is kept",
            nil,
            nil,
        )
    }

    journalRepository := repository.MustGetCatalogJournalRepository(runtimeInstance.Container())

    entries, latestErr := journalRepository.Latest(runtimeInstance.Context(), commandContext.Int("limit"))
    if nil != latestErr {
        return latestErr
    }

    headers := []string{
        "ID",
        "ACTOR",
        "ACTION",
        "SUBJECT",
        "SUBJECT_ID",
        "RECORDED_AT",
    }

    rows := make([][]string, 0, len(entries))
    for _, entry := range entries {
        if nil == entry {
            continue
        }

        rows = append(rows, []string{
            strconv.FormatInt(entry.Id, 10),
            entry.Actor,
            entry.Action,
            entry.Subject,
            entry.SubjectId,
            entry.RecordedAt.UTC().Format(time.RFC3339),
        })
    }

    /* the same table helper product:list prints through, so both commands render alike */
    printTable(headers, rows)

    return nil
}

var _ melodyclicontract.Command = (*CatalogJournalCommand)(nil)
