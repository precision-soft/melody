package mysql

import (
    "context"
    "database/sql"
    "database/sql/driver"
    "errors"
    "testing"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
    melodylock "github.com/precision-soft/melody/v3/lock"
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/mysqldialect"
)

/* @info fakes */

type fakeConnector struct{}

func (fakeConnector) Connect(context.Context) (driver.Conn, error) {
    return nil, errors.New("fake connector never connects")
}

func (fakeConnector) Driver() driver.Driver {
    return fakeDriver{}
}

type fakeDriver struct{}

func (fakeDriver) Open(string) (driver.Conn, error) {
    return nil, errors.New("fake driver never opens")
}

func newMysqlDatabase() *bun.DB {
    return bun.NewDB(sql.OpenDB(fakeConnector{}), mysqldialect.New())
}

type spyServiceRegistrar struct {
    names []string
}

func (instance *spyServiceRegistrar) RegisterService(serviceName string, provider any, options ...containercontract.RegisterOption) {
    instance.names = append(instance.names, serviceName)
}

/* @info tests */

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
    NewModule(ModuleConfig{AsLocker: true}).RegisterServices(registrar)
    if 0 != len(registrar.names) {
        t.Fatalf("expected no service without a database, got %v", registrar.names)
    }

    registrar = &spyServiceRegistrar{}
    NewModule(ModuleConfig{Database: newMysqlDatabase(), AsLocker: true}).RegisterServices(registrar)
    if 1 != len(registrar.names) || melodylock.ServiceLocker != registrar.names[0] {
        t.Fatalf("expected the locker service, got %v", registrar.names)
    }
}
