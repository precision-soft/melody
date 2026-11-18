package security

import (
    "github.com/precision-soft/melody/v4/exception"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
)

/* HmacApplicationRegistry maps a calling application name (as carried by the envelope) to the roles its service principal is granted once the envelope is verified. An application absent from the registry, an answer of false, is rejected, so only known callers obtain a token; an error says the registry could not answer, and the verifier answers it as unavailable. */
type HmacApplicationRegistry interface {
    RolesForApplication(runtimeInstance runtimecontract.Runtime, application string) ([]string, bool, error)
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

func (instance *StaticHmacApplicationRegistry) RolesForApplication(runtimeInstance runtimecontract.Runtime, application string) ([]string, bool, error) {
    if runtimeErr := runtimecontract.RefuseNilRuntime(runtimeInstance); nil != runtimeErr {
        return nil, false, runtimeErr
    }

    roles, exists := instance.rolesByApplication[application]
    if false == exists {
        return nil, false, nil
    }

    return append([]string{}, roles...), true, nil
}

var _ HmacApplicationRegistry = (*StaticHmacApplicationRegistry)(nil)
