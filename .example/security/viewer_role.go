package security

import (
    "github.com/precision-soft/melody/.example/entity"
    melodyruntimecontract "github.com/precision-soft/melody/runtime/contract"
    melodysecurity "github.com/precision-soft/melody/security"
    melodysecuritycontract "github.com/precision-soft/melody/security/contract"
)

/* NewRoleHierarchy answers the one statement of the example's role hierarchy: the access control decides with it, and the login door and the pages read the roles it expands to, so what the browser offers and what the server allows are one rule rather than two that drift. */
func NewRoleHierarchy() *melodysecurity.RoleHierarchy {
    return melodysecurity.NewRoleHierarchy(
        map[string][]string{
            entity.RoleAdmin:  {entity.RoleEditor, entity.RoleUser},
            entity.RoleEditor: {entity.RoleUser},
        },
    )
}

/* ViewerRoleList answers the roles the request's token holds, expanded through the hierarchy, and an empty list for a caller without one. It decides what a page offers, never what the server allows: every door it hides keeps its own rule. */
func ViewerRoleList(runtimeInstance melodyruntimecontract.Runtime) []string {
    securityContext, exists := melodysecurity.SecurityContextFromRuntime(runtimeInstance)
    if false == exists {
        return []string{}
    }

    return viewerRoleListOfToken(securityContext.Token())
}

func viewerRoleListOfToken(token melodysecuritycontract.Token) []string {
    if nil == token || false == token.IsAuthenticated() {
        return []string{}
    }

    return NewRoleHierarchy().ExpandRoles(token.Roles())
}

/* HoldsRole answers whether the role list carries the role, compared by its exact spelling as the role voter compares it. */
func HoldsRole(roleList []string, role string) bool {
    for _, held := range roleList {
        if role == held {
            return true
        }
    }

    return false
}
