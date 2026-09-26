package event

import (
    "github.com/precision-soft/melody/v3/.example/entity"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodysecurity "github.com/precision-soft/melody/v3/security"
)

/* CatalogTopic is the topic the catalog subscriber broadcasts every product and user write onto. */
const CatalogTopic = "default"

/* topicRoleRequirement names the extra role a subscriber must hold to read a privileged topic, one carrying content the application produces behind an access rule. The catalog topic carries every product and user write made behind RoleEditor and RoleAdmin. */
var topicRoleRequirement = map[string]string{
    CatalogTopic: entity.RoleEditor,
}

/* topicIsReadableBy reports whether the caller may subscribe to the topic the client chose in the query string. A topic this application publishes onto is declared above with the role its content is written behind; any other topic carries only what the publish route, itself behind RoleEditor, put there, and is readable by any caller the stream route's access rule authenticated. */
func topicIsReadableBy(runtimeInstance melodyruntimecontract.Runtime, topic string) bool {
    requiredRole, isPrivileged := topicRoleRequirement[topic]
    if false == isPrivileged {
        return true
    }

    return melodysecurity.IsGranted(runtimeInstance, requiredRole)
}
