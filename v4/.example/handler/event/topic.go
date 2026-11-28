package event

import (
    nethttp "net/http"

    "github.com/precision-soft/melody/v4/.example/entity"
    "github.com/precision-soft/melody/v4/.example/presenter"
    "github.com/precision-soft/melody/v4/.example/subscriber"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyhttpcontract "github.com/precision-soft/melody/v4/http/contract"
    melodysecurity "github.com/precision-soft/melody/v4/security"
)

const (
    /* CatalogTopic is the topic the subscribers broadcast every product, category and currency write onto. */
    CatalogTopic = subscriber.CatalogNotificationTopic

    /* UserDirectoryTopic is the topic the subscribers broadcast every account write onto. */
    UserDirectoryTopic = subscriber.UserDirectoryNotificationTopic

    /* TopicQueryName is the query value a client names its topic in, on the stream, the websocket and the publish doors. */
    TopicQueryName = "topic"
)

/* topicRoleRequirement names the extra role a subscriber must hold to read a privileged topic, one carrying content the application produces behind an access rule, and the role a publisher must hold to write onto it. */
var topicRoleRequirement = map[string]string{
    CatalogTopic:       entity.RoleEditor,
    UserDirectoryTopic: entity.RoleAdmin,
}

/* TopicOf is the one reading of the topic a client chose, the catalogue's when it names none. */
func TopicOf(request melodyhttpcontract.Request) string {
    return queryStringOr(request, TopicQueryName, CatalogTopic)
}

/* topicIsReadableBy reports whether the caller may subscribe to, or publish onto, the topic the client chose in the query string. A topic this application publishes onto is declared above with the role its content is written behind; any other topic carries only what the publish route, itself behind RoleEditor, put there, and is readable by any caller the stream route's access rule authenticated. */
func topicIsReadableBy(runtimeInstance melodycontainercontract.Runtime, topic string) bool {
    requiredRole, isPrivileged := topicRoleRequirement[topic]
    if false == isPrivileged {
        return true
    }

    return melodysecurity.IsGranted(runtimeInstance, requiredRole)
}

/* StreamTopicGateMiddleware refuses, on the route named, a topic whose role the caller lacks, before the websocket integration's handler accepts the upgrade, past which a refusal would be discarded. It stands ahead of the slot count, so a refused topic takes no slot. Every other route passes through untouched. */
func StreamTopicGateMiddleware(routeName string) melodyhttpcontract.Middleware {
    return func(next melodyhttpcontract.Handler) melodyhttpcontract.Handler {
        return func(runtimeInstance melodycontainercontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
            if routeName != request.RouteName() {
                return next(runtimeInstance, writer, request)
            }

            if false == topicIsReadableBy(runtimeInstance, TopicOf(request)) {
                return presenter.ApiError(runtimeInstance, request, nethttp.StatusForbidden, "not allowed to subscribe to this topic"), nil
            }

            return next(runtimeInstance, writer, request)
        }
    }
}
