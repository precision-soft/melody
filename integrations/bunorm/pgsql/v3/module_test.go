package pgsql

import (
    "testing"

    melodylock "github.com/precision-soft/melody/v3/lock"
)

func TestModule_NameAndDescription(t *testing.T) {
    module := NewModule(ModuleConfig{})

    if "bunorm.pgsql" != module.Name() {
        t.Fatalf("Name() = %q, want %q", module.Name(), "bunorm.pgsql")
    }

    if "" == module.Description() {
        t.Fatal("Description() must not be empty")
    }
}

func TestModule_RegisterServices(t *testing.T) {
    registrar := &spyServiceRegistrar{}
    NewModule(ModuleConfig{Database: newUndialedDatabase()}).RegisterServices(registrar)
    if 0 != len(registrar.names) {
        t.Fatalf("expected no service unless AsLocker is set, got %v", registrar.names)
    }

    registrar = &spyServiceRegistrar{}
    NewModule(ModuleConfig{AsLocker: true}).RegisterServices(registrar)
    if 0 != len(registrar.names) {
        t.Fatalf("expected no service without a database, got %v", registrar.names)
    }

    registrar = &spyServiceRegistrar{}
    NewModule(ModuleConfig{Database: newUndialedDatabase(), AsLocker: true}).RegisterServices(registrar)
    if 1 != len(registrar.names) || melodylock.ServiceLocker != registrar.names[0] {
        t.Fatalf("expected the locker service, got %v", registrar.names)
    }
}
