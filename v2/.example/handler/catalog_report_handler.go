package handler

import (
    nethttp "net/http"
    "time"

    "github.com/precision-soft/melody/v2/.example/presenter"
    "github.com/precision-soft/melody/v2/.example/repository"
    "github.com/precision-soft/melody/v2/.example/service"
    melodyhttpcontract "github.com/precision-soft/melody/v2/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

const catalogReportEntryLimit = 10

type catalogJournalEntryPayload struct {
    Id         int64  `json:"id"`
    Actor      string `json:"actor"`
    Action     string `json:"action"`
    Subject    string `json:"subject"`
    SubjectId  string `json:"subjectId"`
    RecordedAt string `json:"recordedAt"`
}

/* CatalogReportHandler renders the clock-stamped reading of the catalogue, served from the redis cache once it is warm, together with the most recent changes the journal recorded.

The counts come from the cached reading and the entries are read live: the numbers are what the report is for and are worth remembering for a while, while the list of who changed what is short, cheap and only useful when it is current. Without a database there is no journal and the list is simply empty. */
func CatalogReportHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        reportService := service.MustGetCatalogReportService(runtimeInstance.Container())

        report, reportErr := reportService.Report(runtimeInstance.Context())
        if nil != reportErr {
            return nil, reportErr
        }

        entries, entriesErr := latestJournalEntries(runtimeInstance)
        if nil != entriesErr {
            return nil, entriesErr
        }

        return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, map[string]any{
            "recordedAt": report.RecordedAt.UTC().Format(time.RFC3339),
            "payload":    report.Payload,
            "fromCache":  report.FromCache,
            "journal":    entries,
        }), nil
    }
}

func latestJournalEntries(runtimeInstance melodyruntimecontract.Runtime) ([]catalogJournalEntryPayload, error) {
    payload := make([]catalogJournalEntryPayload, 0, catalogReportEntryLimit)

    if false == runtimeInstance.Container().Has(repository.ServiceCatalogJournalRepository) {
        return payload, nil
    }

    journalRepository := repository.MustGetCatalogJournalRepository(runtimeInstance.Container())

    entries, latestErr := journalRepository.Latest(runtimeInstance.Context(), catalogReportEntryLimit)
    if nil != latestErr {
        return nil, latestErr
    }

    for _, entry := range entries {
        if nil == entry {
            continue
        }

        payload = append(payload, catalogJournalEntryPayload{
            Id:         entry.Id,
            Actor:      entry.Actor,
            Action:     entry.Action,
            Subject:    entry.Subject,
            SubjectId:  entry.SubjectId,
            RecordedAt: entry.RecordedAt.UTC().Format(time.RFC3339),
        })
    }

    return payload, nil
}
