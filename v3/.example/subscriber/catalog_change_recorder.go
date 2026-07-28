package subscriber

import (
    "github.com/precision-soft/melody/v3/.example/reporting"
    "github.com/precision-soft/melody/v3/.example/service"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* recordCatalogChange records one change the nomenclature has just accepted, and tells every open page about it. It is called from the event listeners rather than from the services, because the listeners are already the place where everything that must follow a write is gathered — the cache invalidation lives beside it, and all of it would otherwise have to be repeated at every call site that changes a record.

A change made while serving a request goes into that request's trail, which the flush middleware writes as one batch once the handler chain has returned. Every entry then carries the request that caused it, and a request that changed several records costs one round trip rather than one per change.

A change made outside a request — a scheduled refresh, a console run — is written on the spot instead. The trail is built from the request context, which only the http kernel installs into a scope, so there is nothing to accumulate under and nothing that would later flush it. That is not a degraded path: a console run genuinely has no request to be attributed to, and the journal says so by leaving the request empty.

The services are resolved per call rather than held by the subscriber: a subscriber is built while the container is still being filled, and a service captured then would be the one from before the environment had finished deciding what it could reach. */
func recordCatalogChange(
    runtimeInstance melodyruntimecontract.Runtime,
    action string,
    subject string,
    subjectId string,
) error {
    notifyErr := notifyCatalogChange(runtimeInstance, action, subject, subjectId)
    if nil != notifyErr {
        return notifyErr
    }

    scope := runtimeInstance.Scope()
    if nil != scope {
        trail, trailErr := melodycontainer.FromResolver[*reporting.RequestReportTrail](
            scope,
            reporting.ServiceRequestReportTrail,
        )
        if nil == trailErr {
            trail.Record(service.ActorFromRuntime(runtimeInstance), action, subject, subjectId)

            return nil
        }
    }

    journalService := service.MustGetCatalogJournalService(runtimeInstance.Container())

    return journalService.Record(runtimeInstance, action, subject, subjectId)
}
