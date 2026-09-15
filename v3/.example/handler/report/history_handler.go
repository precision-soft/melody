package report

import (
    "errors"
    nethttp "net/http"
    "strconv"
    "time"

    "github.com/precision-soft/melody/v3/.example/presenter"
    "github.com/precision-soft/melody/v3/.example/reporting"
    melodybag "github.com/precision-soft/melody/v3/bag"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

var errInvalidLimit = errors.New("limit must be a positive whole number")

const (

    historyLimitParameter = "limit"

    historyDefaultLimit = 10

    historyMaximumLimit = 100
)

/* ApiHistoryHandler answers the archive of catalogue readings, newest first.

   The reading half of the second database: the scheduled refresh writes one row per reading and this is what a reader asks to see how the catalogue moved. It is a read of the archive alone — it takes no reading and writes nothing — so a caller cannot fill the archive by asking to see it. */
func ApiHistoryHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        reportService, resolveErr := melodycontainer.FromResolverByType[*reporting.CatalogReportService](runtimeInstance.Container())
        if nil != resolveErr {
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "reading history is unavailable", resolveErr), nil
        }

        limit, limitErr := historyLimitOf(request)
        if nil != limitErr {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, limitErr.Error()), nil
        }

        readingList, readErr := reportService.RecentReadings(runtimeInstance, limit)
        if nil != readErr {
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "failed to load reading history", readErr), nil
        }

        payload := make([]map[string]any, 0, len(readingList))
        for _, reading := range readingList {
            payload = append(payload, map[string]any{
                "taken_at":      reading.TakenAt.UTC().Format(time.RFC3339),
                "headline":      reading.Headline,
                "payload":       reading.Payload,
                "product_count": reading.ProductCount,
                "journal_count": reading.JournalCount,
            })
        }

        return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, map[string]any{
            "readings": payload,
            "limit":    limit,
        }), nil
    }
}

func historyLimitOf(request melodyhttpcontract.Request) (int, error) {
    raw, present, indexErr := melodybag.StringAt(request.Query(), historyLimitParameter, 0)
    if nil != indexErr {
        return 0, errInvalidLimit
    }

    if false == present || "" == raw {
        return historyDefaultLimit, nil
    }

    limit, parseErr := strconv.Atoi(raw)
    if nil != parseErr {
        return 0, errInvalidLimit
    }

    if 0 >= limit {
        return 0, errInvalidLimit
    }

    if historyMaximumLimit < limit {
        return historyMaximumLimit, nil
    }

    return limit, nil
}
