package security

import (
    "github.com/precision-soft/melody/v3/exception"
)

/* HmacAppRegistry maps a calling application name (as carried by the envelope) to the roles its service principal is granted once the envelope is verified. An app absent from the registry is rejected, so only known callers obtain a token. */
type HmacAppRegistry interface {
    RolesForApp(app string) ([]string, bool)
}

func NewStaticHmacAppRegistry(rolesByApp map[string][]string) *StaticHmacAppRegistry {
    if 0 == len(rolesByApp) {
        exception.Panic(exception.NewError("hmac app registry is empty", nil, nil))
    }

    copied := make(map[string][]string, len(rolesByApp))
    for app, roles := range rolesByApp {
        if "" == app {
            exception.Panic(exception.NewError("hmac app name is empty", nil, nil))
        }

        copied[app] = append([]string{}, roles...)
    }

    return &StaticHmacAppRegistry{rolesByApp: copied}
}

type StaticHmacAppRegistry struct {
    rolesByApp map[string][]string
}

func (instance *StaticHmacAppRegistry) RolesForApp(app string) ([]string, bool) {
    roles, exists := instance.rolesByApp[app]
    if false == exists {
        return nil, false
    }

    return append([]string{}, roles...), true
}

var _ HmacAppRegistry = (*StaticHmacAppRegistry)(nil)
