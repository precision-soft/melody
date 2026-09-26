package subscriber

import (
    "encoding/json"

    melodycontainer "github.com/precision-soft/melody/v3/container"
    "github.com/precision-soft/melody/v3/exception"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

const (
    ServiceCatalogNotificationHub = "service.example.catalog.notification.hub"

    /* the topic every browser watching the nomenclature joins: the websocket handler sets no topic resolver, so a broadcast here reaches every open page */
    catalogNotificationTopic = "default"

    /* the event name the page listens for, not "message", so the page tells it apart from the message bus notifications on the same socket */
    catalogNotificationEvent = "catalog"
)

/* catalogNotification is what a browser is told when the nomenclature changes: what happened, not the new record, which the page fetches from the read endpoint. */
type catalogNotification struct {
    Action    string `json:"action"`
    Subject   string `json:"subject"`
    SubjectId string `json:"subjectId"`
}

/* CatalogNotificationHubFromRuntime resolves the hub through the container, the single door that does so. Resolving runs the provider, which moves the hub's reporting off the emergency logger and tells the container the hub must be closed in order. */
func CatalogNotificationHubFromRuntime(runtimeInstance melodyruntimecontract.Runtime) (*melodyhttp.ServerSentEventHub, error) {
    hub, hubErr := melodycontainer.FromResolver[*melodyhttp.ServerSentEventHub](
        runtimeInstance.Container(),
        ServiceCatalogNotificationHub,
    )
    if nil != hubErr {
        return nil, hubErr
    }

    if nil == hub {
        return nil, exception.NewError("the catalog notification hub resolved to nothing", nil, nil)
    }

    return hub, nil
}

/* notifyCatalogChange tells every open page that the nomenclature changed, from the event listeners beside the cache invalidation and the journal entry. A broadcast cannot fail, so the only error is an unreachable hub, a wiring mistake, which is reported so the pages do not go silent unexplained. */
func notifyCatalogChange(
    runtimeInstance melodyruntimecontract.Runtime,
    action string,
    subject string,
    subjectId string,
) error {
    hub, hubErr := CatalogNotificationHubFromRuntime(runtimeInstance)
    if nil != hubErr {
        return hubErr
    }

    payload, encodeErr := json.Marshal(catalogNotification{
        Action:    action,
        Subject:   subject,
        SubjectId: subjectId,
    })
    if nil != encodeErr {
        return encodeErr
    }

    hub.Broadcast(catalogNotificationTopic, melodyhttp.ServerSentEvent{
        Event: catalogNotificationEvent,
        Data:  string(payload),
    })

    return nil
}
