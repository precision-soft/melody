package mysql

import (
    "math"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
)

func TestResolvePool_AnAbsentFieldTakesItsDefault(t *testing.T) {
    resolved, resolveErr := resolvePool(PoolConfig{})
    if nil != resolveErr {
        t.Fatalf("expected the zero pool to resolve, got %v", resolveErr)
    }

    if 25 != resolved.openConnectionCount() || 5 != resolved.idleConnectionCount() {
        t.Fatalf("expected the default caps 25 / 5, got %d / %d", resolved.openConnectionCount(), resolved.idleConnectionCount())
    }

    if 5*time.Minute != driverDuration(resolved.connectionMaxLifetime) || time.Minute != driverDuration(resolved.connectionMaxIdleTime) {
        t.Fatalf("expected the default recycling 5m / 1m, got %v / %v", resolved.connectionMaxLifetime, resolved.connectionMaxIdleTime)
    }
}

func TestResolvePool_KeepsTheConfiguredValues(t *testing.T) {
    resolved, resolveErr := resolvePool(PoolConfig{
        MaxOpenConnections:    bound.Of(10),
        MaxIdleConnections:    bound.Of(2),
        ConnectionMaxLifetime: bound.Of(time.Minute),
        ConnectionMaxIdleTime: bound.Of(time.Second),
    })
    if nil != resolveErr {
        t.Fatalf("expected the pool to resolve, got %v", resolveErr)
    }

    if 10 != resolved.openConnectionCount() || 2 != resolved.idleConnectionCount() ||
        time.Minute != driverDuration(resolved.connectionMaxLifetime) || time.Second != driverDuration(resolved.connectionMaxIdleTime) {
        t.Fatalf("expected the configured pool, got %+v", resolved)
    }
}

func TestResolvePool_UnlimitedReachesDatabaseSqlAsNoBound(t *testing.T) {
    resolved, resolveErr := resolvePool(PoolConfig{
        MaxOpenConnections:    bound.UnlimitedCount(),
        MaxIdleConnections:    bound.UnlimitedCount(),
        ConnectionMaxLifetime: bound.UnlimitedDuration(),
        ConnectionMaxIdleTime: bound.UnlimitedDuration(),
    })
    if nil != resolveErr {
        t.Fatalf("expected an unlimited pool to resolve, got %v", resolveErr)
    }

    if 0 != resolved.openConnectionCount() || math.MaxInt != resolved.idleConnectionCount() ||
        0 != driverDuration(resolved.connectionMaxLifetime) || 0 != driverDuration(resolved.connectionMaxIdleTime) {
        t.Fatalf("expected every bound lifted, got %+v", resolved)
    }
}

func TestResolvePool_AZeroIdleCapKeepsNoIdleConnection(t *testing.T) {
    resolved, resolveErr := resolvePool(PoolConfig{MaxIdleConnections: bound.Of(0)})
    if nil != resolveErr {
        t.Fatalf("expected a zero idle cap to resolve, got %v", resolveErr)
    }

    if 0 != resolved.idleConnectionCount() {
        t.Fatalf("expected no idle connection kept, got %d", resolved.idleConnectionCount())
    }
}

func TestResolvePool_RefusesAFieldBelowItsMinimumNamingIt(t *testing.T) {
    for _, testCase := range []struct {
        field  string
        config PoolConfig
    }{
        {field: "PoolConfig.MaxOpenConnections", config: PoolConfig{MaxOpenConnections: bound.Of(0)}},
        {field: "PoolConfig.MaxIdleConnections", config: PoolConfig{MaxIdleConnections: bound.Of(-1)}},
        {field: "PoolConfig.ConnectionMaxLifetime", config: PoolConfig{ConnectionMaxLifetime: bound.Of(999 * time.Millisecond)}},
        {field: "PoolConfig.ConnectionMaxIdleTime", config: PoolConfig{ConnectionMaxIdleTime: bound.Of(time.Duration(0))}},
    } {
        _, resolveErr := resolvePool(testCase.config)
        expectErrorNamingField(t, resolveErr, testCase.field)
    }
}

func TestMigrationPool_KeepsTwoConnectionsAndNeverRecyclesMidRun(t *testing.T) {
    pool := migrationPool()

    if 2 != pool.openConnectionCount() || 1 != pool.idleConnectionCount() {
        t.Fatalf("expected the two connections a sequential migration run needs, got %d / %d", pool.openConnectionCount(), pool.idleConnectionCount())
    }

    if 0 != driverDuration(pool.connectionMaxLifetime) || 0 != driverDuration(pool.connectionMaxIdleTime) {
        t.Fatalf("expected no connection recycling for migrations, got %+v", pool)
    }
}
