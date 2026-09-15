package event

import (
    "github.com/precision-soft/melody/v3/.example/entity"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodysecurity "github.com/precision-soft/melody/v3/security"
)

/* CatalogTopic is the topic the catalog subscriber broadcasts every product and user write onto. */
const CatalogTopic = "default"

var topicRoleRequirement = map[string]string{
    CatalogTopic: entity.RoleEditor,
}

func topicIsReadableBy(runtimeInstance melodyruntimecontract.Runtime, topic string) bool {
    requiredRole, isPrivileged := topicRoleRequirement[topic]
    if false == isPrivileged {
        return true
    }

    return melodysecurity.IsGranted(runtimeInstance, requiredRole)
}
