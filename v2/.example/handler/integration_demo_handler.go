package handler

import (
    nethttp "net/http"
    "time"

    "github.com/precision-soft/melody/v2/.example/presenter"
    "github.com/precision-soft/melody/v2/.example/repository"
    "github.com/precision-soft/melody/v2/.example/service"
    melodyclock "github.com/precision-soft/melody/v2/clock"
    melodyhttpcontract "github.com/precision-soft/melody/v2/http/contract"
    melodyrueidiscache "github.com/precision-soft/melody/integrations/rueidis/v2/cache"
    melodyruntimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

type cacheDemoPayload struct {
    Key   string `json:"key"`
    Wrote string `json:"wrote"`
    Read  string `json:"read"`
    Hit   bool   `json:"hit"`
}

type databaseDemoPayload struct {
    Id         int64  `json:"id"`
    Note       string `json:"note"`
    RecordedAt string `json:"recordedAt"`
    Total      int    `json:"total"`
}

type rateLimitDemoPayload struct {
    Status string `json:"status"`
}

/* CacheDemoHandler writes a clock-stamped payload through the redis backend and reads it straight back, which is the smallest round trip that proves the connection is live rather than merely configured. The backend is handed in rather than resolved by name, because the configuration package that names it is the one that wires these routes. */
func CacheDemoHandler(backend *melodyrueidiscache.BackendService) melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        clockInstance := melodyclock.ClockMustFromResolver(runtimeInstance.Container())

        key := "demo.stamp"
        wrote := clockInstance.Now().UTC().Format(time.RFC3339Nano)

        setErr := backend.Set(key, []byte(wrote), time.Minute)
        if nil != setErr {
            return nil, setErr
        }

        payload, found, getErr := backend.Get(key)
        if nil != getErr {
            return nil, getErr
        }

        return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, cacheDemoPayload{
            Key:   key,
            Wrote: wrote,
            Read:  string(payload),
            Hit:   found,
        }), nil
    }
}

/* DatabaseDemoHandler creates the demo table when it is absent, appends a clock-stamped note, reads it back by the identifier the insert assigned, and reports how many notes the table holds. The schema is created here rather than by a migration because the example carries no migration runner and the demo owns the one table it uses. */
func DatabaseDemoHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        noteRepository := repository.MustGetCatalogNoteRepository(runtimeInstance.Container())
        clockInstance := melodyclock.ClockMustFromResolver(runtimeInstance.Container())

        ctx := runtimeInstance.Context()

        ensureSchemaErr := noteRepository.EnsureSchema(ctx)
        if nil != ensureSchemaErr {
            return nil, ensureSchemaErr
        }

        recordedAt := clockInstance.Now().UTC().Truncate(time.Second)

        appended, appendErr := noteRepository.Append(ctx, "catalogue read", recordedAt)
        if nil != appendErr {
            return nil, appendErr
        }

        stored, found, findErr := noteRepository.FindById(ctx, appended.Id)
        if nil != findErr {
            return nil, findErr
        }

        if false == found {
            return presenter.ApiError(
                runtimeInstance,
                request,
                nethttp.StatusInternalServerError,
                "the note was written but could not be read back",
            ), nil
        }

        total, countErr := noteRepository.Count(ctx)
        if nil != countErr {
            return nil, countErr
        }

        return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, databaseDemoPayload{
            Id:         stored.Id,
            Note:       stored.Note,
            RecordedAt: stored.RecordedAt.UTC().Format(time.RFC3339),
            Total:      total,
        }), nil
    }
}

/* RateLimitDemoHandler answers whatever the rate-limit middleware in front of it let through. */
func RateLimitDemoHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, rateLimitDemoPayload{
            Status: "allowed",
        }), nil
    }
}

/* ReportDemoHandler renders the clock-stamped catalogue report, which is served from the redis cache once it is warm. */
func ReportDemoHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        reportService := service.MustGetCatalogReportService(runtimeInstance.Container())

        report, reportErr := reportService.Report(runtimeInstance.Context())
        if nil != reportErr {
            return nil, reportErr
        }

        return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, map[string]any{
            "recordedAt": report.RecordedAt.UTC().Format(time.RFC3339),
            "payload":    report.Payload,
            "fromCache":  report.FromCache,
        }), nil
    }
}
