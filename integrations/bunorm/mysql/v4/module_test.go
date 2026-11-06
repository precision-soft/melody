package mysql

import (
    "context"
    "database/sql"
    "database/sql/driver"
    "errors"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    melodylock "github.com/precision-soft/melody/v4/lock"
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/mysqldialect"
)

type fakeConnector struct{}

func (instance fakeConnector) Connect(context.Context) (driver.Conn, error) {
    return nil, errors.New("fake connector never connects")
}

func (instance fakeConnector) Driver() driver.Driver {
    return fakeDriver{}
}

type fakeDriver struct{}

func (instance fakeDriver) Open(string) (driver.Conn, error) {
    return nil, errors.New("fake driver never opens")
}

func newMysqlDatabase() *bun.DB {
    return bun.NewDB(sql.OpenDB(fakeConnector{}), mysqldialect.New())
}

type spyServiceRegistrar struct {
    names []string
}

func (instance *spyServiceRegistrar) RegisterService(serviceName string, provider any, options containercontract.RegisterOptions) {
    instance.names = append(instance.names, serviceName)
}

func (instance *spyServiceRegistrar) Register(serviceName string, provider any, options containercontract.RegisterOptions) error {
    instance.RegisterService(serviceName, provider, options)

    return nil
}

func (instance *spyServiceRegistrar) MustRegister(serviceName string, provider any, options containercontract.RegisterOptions) {
    instance.RegisterService(serviceName, provider, options)
}

func TestModule_NameAndDescription(t *testing.T) {
    module := NewModule(ModuleConfig{})

    if "bunorm.mysql" != module.Name() {
        t.Fatalf("Name() = %q, want %q", module.Name(), "bunorm.mysql")
    }

    if "" == module.Description() {
        t.Fatal("Description() must not be empty")
    }
}

func TestModule_RegisterServices(t *testing.T) {
    registrar := &spyServiceRegistrar{}
    NewModule(ModuleConfig{Database: newMysqlDatabase()}).RegisterServices(registrar)
    if 0 != len(registrar.names) {
        t.Fatalf("expected no service unless AsLocker is set, got %v", registrar.names)
    }

    registrar = &spyServiceRegistrar{}
    NewModule(ModuleConfig{Database: newMysqlDatabase(), AsLocker: true}).RegisterServices(registrar)
    if 1 != len(registrar.names) || melodylock.ServiceLocker != registrar.names[0] {
        t.Fatalf("expected the locker service, got %v", registrar.names)
    }
}

func TestModule_RegisterServicesHandsTheLockerConfiguration(t *testing.T) {
    expectPanicNamingField(t, "LockerConfig.ReleaseTimeout", func() {
        NewModule(ModuleConfig{Database: newMysqlDatabase(), AsLocker: true, Locker: LockerConfig{ReleaseTimeout: bound.Of(time.Duration(0))}}).RegisterServices(&spyServiceRegistrar{})
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
