package event

import (
    "github.com/precision-soft/melody/v3/.example/entity"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodysecurity "github.com/precision-soft/melody/v3/security"
)

/* CatalogTopic is the topic the catalog subscriber broadcasts every product and user write onto. */
const CatalogTopic = "default"

/* topicRoleRequirement names the extra role a subscriber must hold to read a PRIVILEGED topic — one carrying content the application itself produces from behind an access rule. The catalog topic is such a topic: every product and user write made behind RoleEditor and RoleAdmin is broadcast onto it, so an authenticated reader holding neither used to watch them go by in real time. */
var topicRoleRequirement = map[string]string{
    CatalogTopic: entity.RoleEditor,
}

/* topicIsReadableBy reports whether the caller may subscribe to the topic.

   The topic is chosen by the client, in the query string, so being allowed to open a stream is not the same as being allowed to read the stream asked for — which is the whole distinction the public "^/events" rule used to collapse. A topic this application publishes onto itself is declared above with the role its content was written behind; any other topic carries only what someone put there through the publish route, which is itself behind RoleEditor, and is therefore readable by any authenticated caller — the access rule on the stream route is what guarantees there is one. */
func topicIsReadableBy(runtimeInstance melodyruntimecontract.Runtime, topic string) bool {
    requiredRole, isPrivileged := topicRoleRequirement[topic]
    if false == isPrivileged {
        return true
    }

    return melodysecurity.IsGranted(runtimeInstance, requiredRole)
}
