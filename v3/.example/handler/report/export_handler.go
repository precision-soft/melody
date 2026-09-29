package report

import (
    "bytes"
    "encoding/csv"
    nethttp "net/http"
    "strconv"
    "time"

    "github.com/precision-soft/melody/v3/.example/presenter"
    "github.com/precision-soft/melody/v3/.example/reporting"
    "github.com/precision-soft/melody/v3/.example/repository"
    melodyclock "github.com/precision-soft/melody/v3/clock"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* catalogReadingsCsvHeader names the columns in the order every row writes them */
var catalogReadingsCsvHeader = []string{"taken_at", "headline", "product_count", "journal_count", "payload"}

/* ApiExportHandler answers the same readings as the history door, newest first and under the same limit, as a csv document a spreadsheet opens: the archive is read into memory, so the file is built here and named through the framework's Content-Disposition builder. */
func ApiExportHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        reportService, resolveErr := melodycontainer.FromResolverByType[*reporting.CatalogReportService](runtimeInstance.Container())
        if nil != resolveErr {
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "the reading archive is unavailable", resolveErr), nil
        }

        limit, limitErr := historyLimitOf(request)
        if nil != limitErr {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, limitErr.Error()), nil
        }

        readingList, readErr := reportService.RecentReadings(runtimeInstance, limit)
        if nil != readErr {
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "the reading archive is unavailable", readErr), nil
        }

        document, renderErr := catalogReadingsCsv(readingList)
        if nil != renderErr {
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "the reading archive could not be exported", renderErr), nil
        }

        exportedAt := melodyclock.ClockMustFromResolver(runtimeInstance.Container()).Now()

        response := melodyhttp.NewResponse(nethttp.StatusOK, document)
        response.SetHeaders(nethttp.Header{
            "Content-Type":        []string{"text/csv; charset=utf-8"},
            "Content-Disposition": []string{melodyhttp.BuildContentDisposition("attachment", catalogReadingsFileName(exportedAt))},
        })

        return response, nil
    }
}

func catalogReadingsFileName(exportedAt time.Time) string {
    return "catalog-readings-" + exportedAt.UTC().Format("2006-01-02") + ".csv"
}

/* catalogReadingsCsv writes the header and one row per reading */
func catalogReadingsCsv(readingList []*repository.CatalogReadingRecord) ([]byte, error) {
    buffer := &bytes.Buffer{}
    csvWriter := csv.NewWriter(buffer)

    if writeErr := csvWriter.Write(catalogReadingsCsvHeader); nil != writeErr {
        return nil, writeErr
    }

    for _, reading := range readingList {
        if nil == reading {
            continue
        }

        row := []string{
            reading.TakenAt.UTC().Format(time.RFC3339),
            spreadsheetSafeCell(reading.Headline),
            strconv.Itoa(reading.ProductCount),
            strconv.Itoa(reading.JournalCount),
            spreadsheetSafeCell(reading.Payload),
        }

        if writeErr := csvWriter.Write(row); nil != writeErr {
            return nil, writeErr
        }
    }

    csvWriter.Flush()
    if flushErr := csvWriter.Error(); nil != flushErr {
        return nil, flushErr
    }

    return buffer.Bytes(), nil
}

/* spreadsheetSafeCell keeps a text cell from being read as a formula: a spreadsheet evaluates a cell that starts with =, +, -, @, a tab or a carriage return, so such a cell is prefixed with an apostrophe, which the spreadsheet shows as text */
func spreadsheetSafeCell(value string) string {
    if "" == value {
        return value
    }

    switch value[0] {
    case '=', '+', '-', '@', '\t', '\r':
        return "'" + value
    }

    return value
}
