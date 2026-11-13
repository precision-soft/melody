package security

import (
    "github.com/precision-soft/melody/v4/exception"
)

/* HmacApplicationRegistry maps a calling application name (as carried by the envelope) to the roles its service principal is granted once the envelope is verified. An application absent from the registry is rejected, so only known callers obtain a token. */
type HmacApplicationRegistry interface {
    RolesForApplication(application string) ([]string, bool)
}

func NewStaticHmacApplicationRegistry(rolesByApplication map[string][]string) *StaticHmacApplicationRegistry {
    if 0 == len(rolesByApplication) {
        exception.Panic(exception.NewError("hmac application registry is empty", nil, nil))
    }

    copied := make(map[string][]string, len(rolesByApplication))
    for application, roles := range rolesByApplication {
        if "" == application {
            exception.Panic(exception.NewError("hmac application name is empty", nil, nil))
        }

        copied[application] = append([]string{}, roles...)
    }

    return &StaticHmacApplicationRegistry{rolesByApplication: copied}
}

type StaticHmacApplicationRegistry struct {
    rolesByApplication map[string][]string
}

func (instance *StaticHmacApplicationRegistry) RolesForApplication(application string) ([]string, bool) {
    roles, exists := instance.rolesByApplication[application]
    if false == exists {
        return nil, false
    }

    return append([]string{}, roles...), true
}

var _ HmacApplicationRegistry = (*StaticHmacApplicationRegistry)(nil)
