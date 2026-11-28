package event

import (
    nethttp "net/http"
    "testing"

    "github.com/precision-soft/melody/v4/.example/entity"
    "github.com/precision-soft/melody/v4/.example/message"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodymessagebuscontract "github.com/precision-soft/melody/v4/messagebus/contract"
)

type recordingBus struct {
    dispatched []any
}

func (instance *recordingBus) Dispatch(runtimeInstance melodycontainercontract.Runtime, messageValue any, options melodymessagebuscontract.DispatchOptions) (melodymessagebuscontract.Envelope, error) {
    instance.dispatched = append(instance.dispatched, messageValue)

    return nil, nil
}

func TestPublishHandler_RefusesAnEditorOnTheUserDirectoryTopic(t *testing.T) {
    request, runtimeInstance := streamRequestAs(t, "/events/publish/?topic=user-directory&text=forged", []string{entity.RoleUser, entity.RoleEditor})
    bus := &recordingBus{}

    response, handlerErr := PublishHandler(bus)(runtimeInstance, &recordingResponseWriter{}, request)
    if nil != handlerErr || nil == response || nethttp.StatusForbidden != response.StatusCode() {
        t.Fatalf("expected the editor refused with 403, got %v, %v", response, handlerErr)
    }

    if 0 != len(bus.dispatched) {
        t.Fatalf("expected nothing dispatched for a refused topic, got %+v", bus.dispatched)
    }
}

func TestPublishHandler_PublishesOntoATopicTheCallerMayRead(t *testing.T) {
    for _, publication := range []struct {
        target string
        roles  []string
        topic  string
    }{
        {target: "/events/publish/?topic=user-directory&text=notice", roles: []string{entity.RoleUser, entity.RoleEditor, entity.RoleAdmin}, topic: UserDirectoryTopic},
        {target: "/events/publish/?text=notice", roles: []string{entity.RoleUser, entity.RoleEditor}, topic: CatalogTopic},
    } {
        request, runtimeInstance := streamRequestAs(t, publication.target, publication.roles)
        bus := &recordingBus{}

        response, handlerErr := PublishHandler(bus)(runtimeInstance, &recordingResponseWriter{}, request)
        if nil != handlerErr || nil == response || nethttp.StatusAccepted != response.StatusCode() {
            t.Fatalf("%s: expected the notice accepted, got %v, %v", publication.target, response, handlerErr)
        }

        if 1 != len(bus.dispatched) || (message.Notification{Topic: publication.topic, Text: "notice"}) != bus.dispatched[0] {
            t.Fatalf("%s: expected one notice on %s, got %+v", publication.target, publication.topic, bus.dispatched)
        }
    }
}
