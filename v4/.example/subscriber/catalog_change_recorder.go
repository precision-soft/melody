package subscriber

import (
    "github.com/precision-soft/melody/v4/.example/reporting"
    "github.com/precision-soft/melody/v4/.example/service"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
)

/* recordCatalogChange records one change the nomenclature or the directory accepted and tells every page open on the topic about it, from the event listeners where everything that follows a write is gathered. A change made while serving a request goes into that request's trail, flushed as one batch after the handler chain; a change made outside a request is written at once with an empty request, since only the http kernel installs the request context the trail is built from. The services are resolved per call, since a subscriber is built while the container is still being filled. */
func recordCatalogChange(
    runtimeInstance melodycontainercontract.Runtime,
    topic string,
    action string,
    subject string,
    subjectId string,
) error {
    notifyErr := notifyCatalogChange(runtimeInstance, topic, action, subject, subjectId)
    if nil != notifyErr {
        return notifyErr
    }

    trail, trailErr := melodyruntime.FromRuntime[*reporting.RequestReportTrail](runtimeInstance, reporting.ServiceRequestReportTrail)
    if nil == trailErr {
        trail.Record(service.ActorFromRuntime(runtimeInstance), action, subject, subjectId)

        return nil
    }

    journalService := service.MustGetCatalogJournalService(runtimeInstance.Context(), runtimeInstance.Scope())

    return journalService.Record(runtimeInstance, action, subject, subjectId)
}
