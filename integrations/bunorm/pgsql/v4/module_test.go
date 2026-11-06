package pgsql

import (
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    melodylock "github.com/precision-soft/melody/v4/lock"
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
    NewModule(ModuleConfig{Database: newUndialedDatabase(), AsLocker: true}).RegisterServices(registrar)
    if 1 != len(registrar.names) || melodylock.ServiceLocker != registrar.names[0] {
        t.Fatalf("expected the locker service, got %v", registrar.names)
    }
}

func TestModule_RegisterServicesHandsTheLockerConfiguration(t *testing.T) {
    expectPanicNamingField(t, "LockerConfig.ReleaseTimeout", func() {
        NewModule(ModuleConfig{Database: newUndialedDatabase(), AsLocker: true, Locker: LockerConfig{ReleaseTimeout: bound.Of(time.Duration(0))}}).RegisterServices(&spyServiceRegistrar{})
    })
}

func expectModuleRefusalNaming(t *testing.T, field string, config ModuleConfig) {
    t.Helper()

    defer func() {
        recoveredErr, isError := recover().(error)
        if false == isError {
            t.Fatalf("expected a refusal naming %s", field)
        }

        if field != exception.LogContext(recoveredErr, exception.LogContextConfig{})["field"] {
            t.Fatalf("expected the refusal to name %s, got %v", field, exception.LogContext(recoveredErr, exception.LogContextConfig{}))
        }
    }()

    NewModule(config).RegisterServices(&spyServiceRegistrar{})
}

func TestModule_TheLockerAskedWithNoDatabaseIsRefusedNamingIt(t *testing.T) {
    expectModuleRefusalNaming(t, "ModuleConfig.AsLocker", ModuleConfig{AsLocker: true})
}

func TestModule_TheLockersConfigurationWithTheLockerOffIsRefusedNamingIt(t *testing.T) {
    expectModuleRefusalNaming(t, "ModuleConfig.Locker", ModuleConfig{Locker: LockerConfig{ReleaseTimeout: bound.Of(time.Second)}})
}

func TestModule_AnEmptyConfigRegistersNothing(t *testing.T) {
    registrar := &spyServiceRegistrar{}
    NewModule(ModuleConfig{}).RegisterServices(registrar)
    if 0 != len(registrar.names) {
        t.Fatalf("expected no service from an empty module, got %v", registrar.names)
    }
}
