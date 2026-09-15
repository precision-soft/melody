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

    catalogNotificationTopic = "default"

    catalogNotificationEvent = "catalog"
)

type catalogNotification struct {
    Action    string `json:"action"`
    Subject   string `json:"subject"`
    SubjectId string `json:"subjectId"`
}

/* CatalogNotificationHubFromRuntime resolves the registered hub so its provider installs logging and registers container teardown ownership. */
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
