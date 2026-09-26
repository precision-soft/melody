package outbox

import (
    "context"
    nethttp "net/http"

    outboxintegration "github.com/precision-soft/melody/integrations/outbox/v3"
    "github.com/precision-soft/melody/v3/.example/message"
    "github.com/precision-soft/melody/v3/.example/presenter"
    melodybag "github.com/precision-soft/melody/v3/bag"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    bun "github.com/uptrace/bun"
)

/* EnqueueHandler writes a notice to the outbox inside a transaction that in real use also carries the business change, so the message is published if and only if that write commits; the relay publishes it later. The store is a container.Lazy handle resolved at the first request. */
func EnqueueHandler(database *bun.DB, store *melodycontainer.LazyService[*outboxintegration.Store]) melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        reference := melodybag.StringOrDefault(request.Query(), "reference", "")
        if "" == reference {
            reference = "unreferenced"
        }

        text := melodybag.StringOrDefault(request.Query(), "text", "")
        if "" == text {
            text = "hello from the outbox"
        }

        storeInstance, resolveErr := store.Resolve()
        if nil != resolveErr {
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "the outbox store is unavailable", resolveErr), nil
        }

        enqueueErr := database.RunInTx(runtimeInstance.Context(), nil, func(ctx context.Context, tx bun.Tx) error {
            /* the business write belongs on tx here, so it and the outbox write commit atomically */
            return storeInstance.Enqueue(ctx, tx, message.OutboxNotice{Reference: reference, Text: text})
        })
        if nil != enqueueErr {
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "could not enqueue the outbox message", enqueueErr), nil
        }

        return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, map[string]any{
            "enqueued":  true,
            "reference": reference,
        }), nil
    }
}

/* RelayHandler drains one batch of due outbox rows to the transport and reports how many were published, standing in for the relay a scheduler or the outbox:relay command runs. The relay is a container.Lazy handle, so the transport opens at the first request. */
func RelayHandler(relay *melodycontainer.LazyService[*outboxintegration.Relay]) melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        relayInstance, resolveErr := relay.Resolve()
        if nil != resolveErr {
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "the outbox relay is unavailable", resolveErr), nil
        }

        published, runErr := relayInstance.RunOnce(runtimeInstance)
        if nil != runErr {
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "outbox relay failed", runErr), nil
        }

        return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, map[string]any{
            "published": published,
        }), nil
    }
}

/* StatusHandler reports the outbox row counts by status. It resolves the lazy store first, so the outbox schema exists before the count query. */
func StatusHandler(database *bun.DB, store *melodycontainer.LazyService[*outboxintegration.Store]) melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        if _, resolveErr := store.Resolve(); nil != resolveErr {
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "the outbox store is unavailable", resolveErr), nil
        }

        rows, queryErr := database.QueryContext(runtimeInstance.Context(), "SELECT status, COUNT(*) FROM melody_outbox GROUP BY status")
        if nil != queryErr {
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "could not read the outbox status", queryErr), nil
        }
        defer rows.Close()

        counts := map[string]int{}
        for rows.Next() {
            var status string
            var count int
            if scanErr := rows.Scan(&status, &count); nil != scanErr {
                return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "could not read the outbox status", scanErr), nil
            }

            counts[status] = count
        }

        /* an iteration error ends the loop like exhaustion, parked on the rows, so it is read here: a connection dropped mid-read is a failure, not a truncated counts map served as a 200 */
        if rowsErr := rows.Err(); nil != rowsErr {
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "could not read the outbox status", rowsErr), nil
        }

        return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, counts), nil
    }
}

