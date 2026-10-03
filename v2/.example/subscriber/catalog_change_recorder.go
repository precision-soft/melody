package subscriber

import (
    "github.com/precision-soft/melody/v2/.example/service"
    melodyruntimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

/* recordCatalogChange writes one journal entry for a change the nomenclature has just accepted, from the event listeners, where everything that follows a write is gathered beside the cache invalidation. The journal service is resolved per call, since a subscriber is built while the container is still being filled. */
func recordCatalogChange(
    runtimeInstance melodyruntimecontract.Runtime,
    action string,
    subject string,
    subjectId string,
) error {
    journalService := service.MustGetCatalogJournalService(runtimeInstance.Container())

    return journalService.Record(runtimeInstance, action, subject, subjectId)
}
