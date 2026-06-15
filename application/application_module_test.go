package application

import (
    "testing"

    applicationcontract "github.com/precision-soft/melody/application/contract"
)

type fakeModule struct {
    name string
}

func (instance fakeModule) Name() string {
    return instance.name
}

func (instance fakeModule) Description() string {
    return instance.name
}

type fakeModuleProvider struct {
    fakeModule
    children []applicationcontract.Module
}

func (instance fakeModuleProvider) Modules() []applicationcontract.Module {
    return instance.children
}

type selfReferencingModuleProvider struct {
    fakeModule
}

func (instance selfReferencingModuleProvider) Modules() []applicationcontract.Module {
    return []applicationcontract.Module{instance}
}

func assertModuleNames(t *testing.T, modules []applicationcontract.Module, expected []string) {
    t.Helper()

    if len(expected) != len(modules) {
        t.Fatalf("expected %d modules, got %d", len(expected), len(modules))
    }

    for index := range expected {
        if expected[index] != modules[index].Name() {
            t.Fatalf("expected module %d to be %q, got %q", index, expected[index], modules[index].Name())
        }
    }
}

func TestRegisterModule_ExpandsModuleProvider(t *testing.T) {
    instance := &Application{}

    provider := fakeModuleProvider{
        fakeModule: fakeModule{name: "provider"},
        children:   []applicationcontract.Module{fakeModule{name: "child-a"}, fakeModule{name: "child-b"}},
    }

    instance.RegisterModule(provider)

    assertModuleNames(t, instance.modules, []string{"provider", "child-a", "child-b"})
}

func TestRegisterModule_PlainModuleIsNotExpanded(t *testing.T) {
    instance := &Application{}

    instance.RegisterModule(fakeModule{name: "plain"})

    assertModuleNames(t, instance.modules, []string{"plain"})
}

func TestRegisterModule_ExpandsNestedProviders(t *testing.T) {
    instance := &Application{}

    inner := fakeModuleProvider{
        fakeModule: fakeModule{name: "inner"},
        children:   []applicationcontract.Module{fakeModule{name: "leaf"}},
    }
    outer := fakeModuleProvider{
        fakeModule: fakeModule{name: "outer"},
        children:   []applicationcontract.Module{inner},
    }

    instance.RegisterModule(outer)

    assertModuleNames(t, instance.modules, []string{"outer", "inner", "leaf"})
}

func TestRegisterModule_PanicsOnProviderCycleInsteadOfStackOverflow(t *testing.T) {
    instance := &Application{}

    defer func() {
        recovered := recover()
        if nil == recovered {
            t.Fatal("expected a panic on a cyclic module provider, got none")
        }
    }()

    instance.RegisterModule(selfReferencingModuleProvider{fakeModule: fakeModule{name: "cyclic"}})

    t.Fatal("RegisterModule returned without guarding a module provider cycle")
}

func TestRegisterModuleProvider_RegistersChildrenWithoutProvider(t *testing.T) {
    instance := &Application{}

    provider := fakeModuleProvider{
        fakeModule: fakeModule{name: "provider"},
        children:   []applicationcontract.Module{fakeModule{name: "child-a"}, fakeModule{name: "child-b"}},
    }

    instance.RegisterModuleProvider(provider)

    assertModuleNames(t, instance.modules, []string{"child-a", "child-b"})
}
