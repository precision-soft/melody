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

/* errInvalidLimit is the one refusal of every limit this door cannot serve: not a number, zero or negative. */
var errInvalidLimit = errors.New("limit must be a positive whole number")

const (
    historyLimitParameter = "limit"

    /* historyDefaultLimit is what a caller who asked for nothing gets: the recent readings. */
    historyDefaultLimit = 10

    /* historyMaximumLimit is the door's own cap: the archive grows for the life of a volume, and a limit without a ceiling would let an unauthenticated caller choose how much of it this process loads at once. */
    historyMaximumLimit = 100
)

/* ApiHistoryHandler answers the archive of catalogue readings, newest first. It reads the archive alone and writes nothing, so a caller cannot fill the archive by asking to see it. */
func ApiHistoryHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        reportService, resolveErr := melodycontainer.FromResolverByType[*reporting.CatalogReportService](runtimeInstance.Container())
        if nil != resolveErr {
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "the reading archive is unavailable", resolveErr), nil
        }

        limit, limitErr := historyLimitOf(request)
        if nil != limitErr {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, limitErr.Error()), nil
        }

        /* a failure of the archive is rendered through the presenter, the envelope every sibling read door answers a repository failure in */
        readingList, readErr := reportService.RecentReadings(runtimeInstance, limit)
        if nil != readErr {
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "the reading archive is unavailable", readErr), nil
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

/* historyLimitOf reads the caller's limit, or answers the default when they gave none. It reads the first value with StringAt, so a repeated key at this public door answers like a single one. */
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
