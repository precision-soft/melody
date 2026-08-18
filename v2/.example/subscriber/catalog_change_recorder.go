package subscriber

import (
    "github.com/precision-soft/melody/v2/.example/service"
    melodyruntimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

/* recordCatalogChange writes one journal entry for a change the nomenclature has just accepted. It is called from the event listeners rather than from the services, because the listeners are already the place where everything that must follow a write is gathered — the cache invalidation lives beside it, and both would otherwise have to be repeated at every call site that changes a record.

The journal service is resolved per call rather than held by the subscriber: a subscriber is built while the container is still being filled, and a service captured then would be the one from before the environment had finished deciding what it could reach. */
func recordCatalogChange(
    runtimeInstance melodyruntimecontract.Runtime,
    action string,
    subject string,
    subjectId string,
) error {
    journalService := service.MustGetCatalogJournalService(runtimeInstance.Container())

    return journalService.Record(runtimeInstance, action, subject, subjectId)
}
