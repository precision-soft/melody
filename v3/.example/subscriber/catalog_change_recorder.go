package subscriber

import (
    "github.com/precision-soft/melody/v3/.example/reporting"
    "github.com/precision-soft/melody/v3/.example/service"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

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
