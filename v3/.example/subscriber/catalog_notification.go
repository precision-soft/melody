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

    /* the topic every browser watching the nomenclature is subscribed to. The websocket handler sets no topic resolver, so a connection joins this one and a broadcast here reaches every open page. */
    catalogNotificationTopic = "default"

    /* the event name the page listens for. It is not "message", because a page that also received the message bus notifications on the same socket would have no way to tell the two apart. */
    catalogNotificationEvent = "catalog"
)

/* catalogNotification is what a browser is told when the nomenclature changes. It carries what happened rather than the new record: a page that wants the record fetches it, and a notification that shipped one would be a second, unversioned copy of the read endpoint. */
type catalogNotification struct {
    Action    string `json:"action"`
    Subject   string `json:"subject"`
    SubjectId string `json:"subjectId"`
}

/* notifyCatalogChange tells every open page that the nomenclature has changed.

It runs from the event listeners, beside the cache invalidation and the journal entry, because those three are the same thing said three ways: everything that must follow a write. A page holding a stale list is exactly the case the invalidation two lines above exists for, and this is what lets the page find out.

Broadcasting itself cannot fail — the hub drops an event nobody is listening for, and the payload is a struct of strings — so the only thing this reports is not being able to reach the hub at all, which is a wiring mistake rather than a runtime condition. It is reported rather than swallowed for exactly that reason: a resolution that quietly returned would leave the notifications never firing, with every write still answering 201 and nothing anywhere saying why the pages went silent. */
func notifyCatalogChange(
    runtimeInstance melodyruntimecontract.Runtime,
    action string,
    subject string,
    subjectId string,
) error {
    hub, hubErr := melodycontainer.FromResolver[*melodyhttp.ServerSentEventHub](
        runtimeInstance.Container(),
        ServiceCatalogNotificationHub,
    )
    if nil != hubErr {
        return hubErr
    }

    if nil == hub {
        return exception.NewError("the catalog notification hub resolved to nothing", nil, nil)
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
