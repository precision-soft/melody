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

/* errInvalidLimit is one value rather than a sentence built per branch: every way of asking for a limit this door cannot serve — not a number, zero, negative — is the same mistake and gets the same words. */
var errInvalidLimit = errors.New("limit must be a positive whole number")

const (
    /* historyLimitParameter is the query key a caller narrows the listing with. */
    historyLimitParameter = "limit"

    /* historyDefaultLimit is what a caller who asked for nothing gets. It is small on purpose: this door answers "what has the catalogue looked like lately", and a caller who wants more says so. */
    historyDefaultLimit = 10

    /* historyMaximumLimit caps what any caller can ask for, and the cap is the door's own rather than the repository's. The archive grows for the life of a volume — one row per scheduled reading, for ever — so a limit taken from the request without a ceiling is an unauthenticated caller choosing how much of it this process loads into memory at once. */
    historyMaximumLimit = 100
)

/* ApiHistoryHandler answers the archive of catalogue readings, newest first.

   The reading half of the second database: the scheduled refresh writes one row per reading and this is what a reader asks to see how the catalogue moved. It is a read of the archive alone — it takes no reading and writes nothing — so a caller cannot fill the archive by asking to see it. */
func ApiHistoryHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        reportService, resolveErr := melodycontainer.FromResolverByType[*reporting.CatalogReportService](runtimeInstance.Container())
        if nil != resolveErr {
            return nil, resolveErr
        }

        limit, limitErr := historyLimitOf(request)
        if nil != limitErr {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, limitErr.Error()), nil
        }

        readingList, readErr := reportService.RecentReadings(runtimeInstance.Context(), limit)
        if nil != readErr {
            return nil, readErr
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

/* historyLimitOf reads the caller's limit, or answers the default when they gave none.

   The value is read with StringAt rather than with the string accessor beside it, and the reason is a finding this repository already carries rather than caution: that accessor PANICS on a repeated key, with the rationale that the key is the programmer's — true where it is written, false at a door where the SHAPE of the parameter is chosen by the client. Through a public door a repeated ?limit= would be an unauthenticated 500. The first value is what a repeated key answers here, which is also what the framework's own Input door decided for the same reason. */
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
