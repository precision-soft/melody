package outbox

import (
    "context"
    nethttp "net/http"

    outboxintegration "github.com/precision-soft/melody/integrations/outbox/v3"
    "github.com/precision-soft/melody/v3/.example/message"
    "github.com/precision-soft/melody/v3/.example/presenter"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    bun "github.com/uptrace/bun"
)

/* EnqueueHandler writes a demo message to the outbox inside a transaction — in real use the same
transaction also carries the business change, so the message is published if and only if the business
write commits. It does NOT publish; the relay does that later. */
func EnqueueHandler(database *bun.DB, store *outboxintegration.Store) melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        reference := queryString(request, "reference")
        if "" == reference {
            reference = "demo"
        }

        text := queryString(request, "text")
        if "" == text {
            text = "hello from the outbox"
        }

        enqueueErr := database.RunInTx(runtimeInstance.Context(), nil, func(ctx context.Context, tx bun.Tx) error {
            /* a real handler performs its business write on tx here; the outbox write shares the same
               transaction so the two commit atomically */
            return store.Enqueue(ctx, tx, message.OutboxDemo{Reference: reference, Text: text})
        })
        if nil != enqueueErr {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusInternalServerError, "could not enqueue the outbox message"), nil
        }

        return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, map[string]any{
            "enqueued":  true,
            "reference": reference,
        }), nil
    }
}

/* RelayHandler drains one batch of due outbox rows to the transport and reports how many were published,
standing in for the relay loop a scheduler (cron) or the outbox:relay command would run continuously. */
func RelayHandler(relay *outboxintegration.Relay) melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        published, runErr := relay.RunOnce(runtimeInstance)
        if nil != runErr {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusInternalServerError, "outbox relay failed"), nil
        }

        return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, map[string]any{
            "published": published,
        }), nil
    }
}

/* StatusHandler reports the outbox row counts by status so the pending → sent (or dead) transition the
relay drives is observable. */
func StatusHandler(database *bun.DB) melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        rows, queryErr := database.QueryContext(runtimeInstance.Context(), "SELECT status, COUNT(*) FROM melody_outbox GROUP BY status")
        if nil != queryErr {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusInternalServerError, "could not read the outbox status"), nil
        }
        defer rows.Close()

        counts := map[string]int{}
        for rows.Next() {
            var status string
            var count int
            if scanErr := rows.Scan(&status, &count); nil != scanErr {
                return presenter.ApiError(runtimeInstance, request, nethttp.StatusInternalServerError, "could not read the outbox status"), nil
            }

            counts[status] = count
        }

        return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, counts), nil
    }
}

/* queryString reads a query parameter as a string, handling the bag's []string storage. */
func queryString(request melodyhttpcontract.Request, name string) string {
    value, exists := request.Query().Get(name)
    if false == exists {
        return ""
    }

    switch typed := value.(type) {
    case string:
        return typed
    case []string:
        if 0 == len(typed) {
            return ""
        }

        return typed[0]
    default:
        return ""
    }
}
