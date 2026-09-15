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

    registrar.Use(NewTimingMiddleware(kernelInstance.Clock()))
    registrar.Use(NewCatalogJournalFlushMiddleware())
}

/* NewCatalogJournalFlushMiddleware flushes the request-scoped trail before an uncommitted response is sent. Failures can then affect the response. Its count header reports entries successfully removed by this flush; later scope cleanup is not included. */
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

            stagedBeforeFlush := len(trail.Entries())
            summary := trail.Summary()

            flushErr := trail.Flush(runtimeInstance.Context())
            if nil != flushErr {
                return response, flushErr
            }

            writtenCount := stagedBeforeFlush - len(trail.Entries())

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
