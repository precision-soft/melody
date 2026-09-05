package config

import (
    nethttp "net/http"
    "strconv"

    "github.com/precision-soft/melody/v3/.example/reporting"
    melodyapplicationcontract "github.com/precision-soft/melody/v3/application/contract"
    melodyclockcontract "github.com/precision-soft/melody/v3/clock/contract"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodykernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func (instance *Module) RegisterHttpMiddlewares(kernelInstance melodykernelcontract.Kernel, registrar melodyapplicationcontract.HttpMiddlewareRegistrar) {
    /* the metrics middleware is contributed by the opentelemetry module (see configure.go); this module adds only the example-specific timing and journal-flush middlewares. */
    registrar.Use(NewTimingMiddleware(kernelInstance.Clock()))
    registrar.Use(NewCatalogJournalFlushMiddleware())
}

/* NewCatalogJournalFlushMiddleware writes what the request changed to the nomenclature, once the request is over and while the caller can still be told it failed.

   The scope closes on the way out of the handler, but it closes from a deferred call that runs AFTER the response has gone to the client (http/kernel.go) — so a caller that reads the journal the moment it receives its 201 would be racing the write. Flushing here instead means the write is committed before the status line is sent, and the failure is a failure of the request rather than a line in a log nobody is reading.

   The trail is resolved from the scope, which is also where the event listeners recorded into it. That both resolutions must be the same object is not decoration: were they not, this would flush an empty trail and nothing would be journalled at all.

   The reported count is measured across the flush — what the trail held before, less what it still holds after — rather than simply what it held. That is what makes the header say the write HAPPENED HERE rather than that there was something to write: the trail's own Close would eventually persist the same entries from the scope's teardown, after the response has gone, and a header taken before the flush could not tell the two apart. */
func NewCatalogJournalFlushMiddleware() melodyhttpcontract.Middleware {
    return func(next melodyhttpcontract.Handler) melodyhttpcontract.Handler {
        return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
            response, err := next(runtimeInstance, writer, request)

            trail, trailErr := melodycontainer.FromResolver[*reporting.RequestReportTrail](
                runtimeInstance.Scope(),
                reporting.ServiceRequestReportTrail,
            )
            if nil != trailErr {
                return response, trailErr
            }

            /* the summary describes what the request accumulated, so it is taken while the trail still holds it */
            stagedBeforeFlush := len(trail.Entries())
            summary := trail.Summary()

            flushErr := trail.Flush(runtimeInstance.Context())
            if nil != flushErr {
                return response, flushErr
            }

            writtenCount := stagedBeforeFlush - len(trail.Entries())

            /* an error on the way out already has a response of its own; the headers here describe a request that completed */
            if nil != err {
                return response, err
            }

            if nil != response {
                response.Headers().Set("X-Example-Catalog-Changes", strconv.Itoa(writtenCount))
                response.Headers().Set("X-Example-Request-Trail", summary)
            }

            return response, nil
        }
    }
}

/* NewTimingMiddleware measures how long a request took and reports it in a header.

   The clock is injected rather than read from the wall, which is what makes the header assertable: a frozen clock advanced by the handler under it lets a test state the exact duration the header must carry, and no test can state that against time.Now. */
func NewTimingMiddleware(clockInstance melodyclockcontract.Clock) melodyhttpcontract.Middleware {
    return func(next melodyhttpcontract.Handler) melodyhttpcontract.Handler {
        return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
            startedAt := clockInstance.Now()

            response, err := next(runtimeInstance, writer, request)
            if nil != err {
                return response, err
            }

            duration := clockInstance.Now().Sub(startedAt).Milliseconds()
            if nil != response {
                response.Headers().Set("X-Example-Duration-Ms", strconv.FormatInt(duration, 10))
            }

            return response, nil
        }
    }
}

var _ melodyapplicationcontract.HttpMiddlewareModule = (*Module)(nil)
