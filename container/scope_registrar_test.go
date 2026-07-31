package container

import (
    "testing"

    containercontract "github.com/precision-soft/melody/container/contract"
)

type scopeRegistrarProbe struct {
    value string
}

/* @info a registration on a live scope is the same substitution one request wide, and the protected "service." namespace holds there too — with or without Replacing. */
func TestScopeRegisterScoped_ProtectedNameRefused(t *testing.T) {
    serviceContainer := NewContainer()

    scopeInstance := serviceContainer.NewScope()

    plainErr := scopeInstance.RegisterScoped(
        "service.protected.probe",
        func(resolver containercontract.Resolver) (*scopeRegistrarProbe, error) {
            return &scopeRegistrarProbe{value: "scoped"}, nil
        },
    )
    if nil == plainErr {
        t.Fatalf("expected the protected name to be refused on a live scope")
    }

    replacingErr := scopeInstance.RegisterScoped(
        "service.protected.probe",
        func(resolver containercontract.Resolver) (*scopeRegistrarProbe, error) {
            return &scopeRegistrarProbe{value: "scoped"}, nil
        },
        Replacing(),
    )
    if nil == replacingErr {
        t.Fatalf("expected the protected name to be refused on a live scope even with Replacing declared")
    }
}
