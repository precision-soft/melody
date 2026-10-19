package pgsql

import (
    "context"
    "database/sql"
    "database/sql/driver"
    "errors"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/pgdialect"
)

/* the shared material of this package's tests. A connector that never connects is what lets the registration tests hand the locker a real *bun.DB without a server: the doors under test decide what to register, and none of them issues a query, so a handle that would fail on the first one is the honest double. */

type neverConnectingConnector struct{}

func (instance neverConnectingConnector) Connect(context.Context) (driver.Conn, error) {
    return nil, errors.New("the test connector never connects")
}

func (instance neverConnectingConnector) Driver() driver.Driver {
    return neverOpeningDriver{}
}

type neverOpeningDriver struct{}

func (instance neverOpeningDriver) Open(string) (driver.Conn, error) {
    return nil, errors.New("the test driver never opens")
}

func newUndialedDatabase() *bun.DB {
    return bun.NewDB(sql.OpenDB(neverConnectingConnector{}), pgdialect.New())
}

/* spyServiceRegistrar records the names a registration door claims, which is the whole observable of both doors: what they register, and under which name. It satisfies the module's registrar contract as well as this package's narrower one, so one double serves both tests. */
type spyServiceRegistrar struct {
    names []string
}

func (instance *spyServiceRegistrar) RegisterService(serviceName string, provider any, options ...containercontract.RegisterOption) {
    instance.names = append(instance.names, serviceName)
}

func (instance *spyServiceRegistrar) Register(serviceName string, provider any, options ...containercontract.RegisterOption) error {
    instance.RegisterService(serviceName, provider, options...)

    return nil
}

func (instance *spyServiceRegistrar) MustRegister(serviceName string, provider any, options ...containercontract.RegisterOption) {
    instance.RegisterService(serviceName, provider, options...)
}
