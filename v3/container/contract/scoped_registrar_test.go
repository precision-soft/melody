package contract

import (
    "reflect"
    "testing"
)

/* @info The whole discrimination between the two lifetimes rests on the two method sets never overlapping. Go interfaces are structural: the moment ScopedRegistrar carries Register — by embedding Registrar, or by anyone spelling the method — a container registrar satisfies it and a scoped registrar satisfies a Registrar parameter, both without a conversion and both silently. The hooks that hand these registrars out would then accept each other's, and a container provider registered as scoped would be rebuilt and closed once per request without ever failing. */
func TestRegistrarInterfaces_HaveDisjointMethodSets(t *testing.T) {
    registrarType := reflect.TypeOf((*Registrar)(nil)).Elem()
    scopedRegistrarType := reflect.TypeOf((*ScopedRegistrar)(nil)).Elem()

    if true == registrarType.Implements(scopedRegistrarType) {
        t.Fatalf("expected a Registrar not to satisfy ScopedRegistrar, so a container registrar cannot reach a scoped registration hook")
    }

    if true == scopedRegistrarType.Implements(registrarType) {
        t.Fatalf("expected a ScopedRegistrar not to satisfy Registrar, so a scoped registrar cannot reach a container registration hook")
    }

    registrarMethodNames := make(map[string]struct{}, registrarType.NumMethod())
    for index := 0; index < registrarType.NumMethod(); index = index + 1 {
        registrarMethodNames[registrarType.Method(index).Name] = struct{}{}
    }

    for index := 0; index < scopedRegistrarType.NumMethod(); index = index + 1 {
        methodName := scopedRegistrarType.Method(index).Name

        if _, shared := registrarMethodNames[methodName]; true == shared {
            t.Fatalf("expected no method name to be shared by the two registrars, found %q", methodName)
        }
    }
}
