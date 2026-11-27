package event

import (
    nethttp "net/http"
    "testing"

    "github.com/precision-soft/melody/v4/.example/entity"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyhttpcontract "github.com/precision-soft/melody/v4/http/contract"
)

/* the user directory topic carries the account writes made behind the administrator's role, so an editor, who reads the catalogue topic, does not read it */
func TestTopicIsReadableBy_TheUserDirectoryTopicRequiresTheAdministrator(t *testing.T) {
    _, editorRuntime := streamRequestAs(t, "/events/stream/", []string{entity.RoleUser, entity.RoleEditor})
    if true == topicIsReadableBy(editorRuntime, UserDirectoryTopic) {
        t.Fatal("expected the user directory topic refused to an editor")
    }

    if false == topicIsReadableBy(editorRuntime, CatalogTopic) {
        t.Fatal("expected the catalogue topic readable by an editor")
    }

    _, administratorRuntime := streamRequestAs(t, "/events/stream/", []string{entity.RoleUser, entity.RoleEditor, entity.RoleAdmin})
    if false == topicIsReadableBy(administratorRuntime, UserDirectoryTopic) {
        t.Fatal("expected the user directory topic readable by an administrator")
    }
}

/* the one reading of the topic: the query's value, the catalogue's when the query names none or names it empty */
func TestTopicOf_ReadsTheQueryAndFallsBackOnTheCatalogueTopic(t *testing.T) {
    for target, expected := range map[string]string{
        "/ws?topic=user-directory": UserDirectoryTopic,
        "/ws?topic=":               CatalogTopic,
        "/ws":                      CatalogTopic,
    } {
        request, _ := streamRequest(t, target, true)
        if topic := TopicOf(request); expected != topic {
            t.Fatalf("%s: expected the topic %q, got %q", target, expected, topic)
        }
    }
}

func gatedHandler(handlerRan *bool) melodyhttpcontract.Handler {
    return func(runtimeInstance melodycontainercontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        *handlerRan = true

        return nil, nil
    }
}

/* an editor asking the websocket route for the user directory topic is refused 403 before the integration's handler would accept the upgrade */
func TestStreamTopicGateMiddleware_RefusesTheUserDirectoryTopicToAnEditor(t *testing.T) {
    request, runtimeInstance := routedRequestAs(t, countedRouteName, "/ws?topic=user-directory", []string{entity.RoleUser, entity.RoleEditor})

    handlerRan := false
    response, handlerErr := StreamTopicGateMiddleware(countedRouteName)(gatedHandler(&handlerRan))(runtimeInstance, &recordingResponseWriter{}, request)
    if nil != handlerErr || nil == response || nethttp.StatusForbidden != response.StatusCode() {
        t.Fatalf("expected the editor refused with 403, got %v, %v", response, handlerErr)
    }

    if true == handlerRan {
        t.Fatal("expected the handler never reached for a refused topic")
    }
}

func TestStreamTopicGateMiddleware_AdmitsAnAdministrator(t *testing.T) {
    request, runtimeInstance := routedRequestAs(t, countedRouteName, "/ws?topic=user-directory", []string{entity.RoleUser, entity.RoleEditor, entity.RoleAdmin})

    handlerRan := false
    if _, handlerErr := StreamTopicGateMiddleware(countedRouteName)(gatedHandler(&handlerRan))(runtimeInstance, &recordingResponseWriter{}, request); nil != handlerErr || false == handlerRan {
        t.Fatalf("expected the administrator to reach the handler, ran=%v err=%v", handlerRan, handlerErr)
    }
}

/* the gate reads the topic of the route named alone: another route naming a topic in its query is not this gate's to refuse */
func TestStreamTopicGateMiddleware_PassesEveryOtherRouteUntouched(t *testing.T) {
    request, runtimeInstance := routedRequestAs(t, "example.products.list", "/products/?topic=user-directory", []string{entity.RoleUser})

    handlerRan := false
    if _, handlerErr := StreamTopicGateMiddleware(countedRouteName)(gatedHandler(&handlerRan))(runtimeInstance, &recordingResponseWriter{}, request); nil != handlerErr || false == handlerRan {
        t.Fatalf("expected another route to reach its handler, ran=%v err=%v", handlerRan, handlerErr)
    }
}
