package subscriber

import (
    "github.com/precision-soft/melody/v3/.example/reporting"
    "github.com/precision-soft/melody/v3/.example/service"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* recordCatalogChange records one change the nomenclature accepted and tells every open page about it, from the event listeners where everything that follows a write is gathered. A change made while serving a request goes into that request's trail, flushed as one batch after the handler chain; a change made outside a request is written at once with an empty request, since only the http kernel installs the request context the trail is built from. The services are resolved per call, since a subscriber is built while the container is still being filled. */
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
