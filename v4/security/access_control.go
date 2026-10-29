package security

import (
    "github.com/precision-soft/melody/v4/security/accesscontrol"
)

/* AccessControlRule is an alias for accesscontrol.Rule. */
type AccessControlRule = accesscontrol.Rule

/* AccessControl is an alias for accesscontrol.Control. */
type AccessControl = accesscontrol.Control

/* NewAccessControl collects the rules a request is resolved against; nil is the empty control, which claims no path. */
func NewAccessControl(rules []AccessControlRule) *AccessControl {
    return accesscontrol.NewControl(rules)
}
