package pgsql

import (
    "math"
    "time"

    "github.com/precision-soft/melody/v4/bound"
)

/* PoolConfig sizes the pool the provider builds the database over; its zero value is the default. */
type PoolConfig struct {
    /* MaxOpenConnections caps the open connections: 50 by default, at least 1, unlimited admitted as database/sql's no cap. */
    MaxOpenConnections bound.Count

    /* MaxIdleConnections caps the idle connections kept for reuse: 25 by default, 0 keeping no idle connection, unlimited admitted and clamped by database/sql to the open cap. */
    MaxIdleConnections bound.Count

    /* ConnectionMaxLifetime recycles a connection after that long open: 5m by default, at least 1s, unlimited admitted as never recycled. */
    ConnectionMaxLifetime bound.Duration

    /* ConnectionMaxIdleTime recycles a connection after that long idle: 1m by default, at least 1s, unlimited admitted as never recycled. */
    ConnectionMaxIdleTime bound.Duration
}

var poolMaxOpenConnectionsRule = bound.Rule[int]{
    Name:              "PoolConfig.MaxOpenConnections",
    Default:           50,
    Minimum:           1,
    UnlimitedAdmitted: true,
}

var poolMaxIdleConnectionsRule = bound.Rule[int]{
    Name:              "PoolConfig.MaxIdleConnections",
    Default:           25,
    Minimum:           0,
    UnlimitedAdmitted: true,
}

var poolConnectionMaxLifetimeRule = bound.Rule[time.Duration]{
    Name:              "PoolConfig.ConnectionMaxLifetime",
    Default:           5 * time.Minute,
    Minimum:           bound.TtlMinimum,
    UnlimitedAdmitted: true,
}

var poolConnectionMaxIdleTimeRule = bound.Rule[time.Duration]{
    Name:              "PoolConfig.ConnectionMaxIdleTime",
    Default:           1 * time.Minute,
    Minimum:           bound.TtlMinimum,
    UnlimitedAdmitted: true,
}

type resolvedPool struct {
    maxOpenConnections    bound.Resolved[int]
    maxIdleConnections    bound.Resolved[int]
    connectionMaxLifetime bound.Resolved[time.Duration]
    connectionMaxIdleTime bound.Resolved[time.Duration]
}

func resolvePool(config PoolConfig) (resolvedPool, error) {
    maxOpenConnections, maxOpenConnectionsErr := bound.Resolve(config.MaxOpenConnections, poolMaxOpenConnectionsRule)
    if nil != maxOpenConnectionsErr {
        return resolvedPool{}, maxOpenConnectionsErr
    }

    maxIdleConnections, maxIdleConnectionsErr := bound.Resolve(config.MaxIdleConnections, poolMaxIdleConnectionsRule)
    if nil != maxIdleConnectionsErr {
        return resolvedPool{}, maxIdleConnectionsErr
    }

    connectionMaxLifetime, connectionMaxLifetimeErr := bound.Resolve(config.ConnectionMaxLifetime, poolConnectionMaxLifetimeRule)
    if nil != connectionMaxLifetimeErr {
        return resolvedPool{}, connectionMaxLifetimeErr
    }

    connectionMaxIdleTime, connectionMaxIdleTimeErr := bound.Resolve(config.ConnectionMaxIdleTime, poolConnectionMaxIdleTimeRule)
    if nil != connectionMaxIdleTimeErr {
        return resolvedPool{}, connectionMaxIdleTimeErr
    }

    return resolvedPool{
        maxOpenConnections:    maxOpenConnections,
        maxIdleConnections:    maxIdleConnections,
        connectionMaxLifetime: connectionMaxLifetime,
        connectionMaxIdleTime: connectionMaxIdleTime,
    }, nil
}

/* migrationPool is the pool OpenForMigration dials with: the two connections a sequential migration run needs, and no connection recycled mid-run. */
func migrationPool() resolvedPool {
    return resolvedPool{
        maxOpenConnections:    bound.Resolved[int]{Value: 2},
        maxIdleConnections:    bound.Resolved[int]{Value: 1},
        connectionMaxLifetime: bound.Resolved[time.Duration]{Unlimited: true},
        connectionMaxIdleTime: bound.Resolved[time.Duration]{Unlimited: true},
    }
}

/* openConnectionCount answers the open-connection cap as database/sql reads it, where zero is no cap. */
func (instance resolvedPool) openConnectionCount() int {
    if true == instance.maxOpenConnections.Unlimited {
        return 0
    }

    return instance.maxOpenConnections.Value
}

/* idleConnectionCount answers the idle-connection cap as database/sql reads it: zero keeps no idle connection and a cap above the open one is clamped to it, so no cap is the largest count. */
func (instance resolvedPool) idleConnectionCount() int {
    if true == instance.maxIdleConnections.Unlimited {
        return math.MaxInt
    }

    return instance.maxIdleConnections.Value
}

func (instance resolvedPool) context() map[string]any {
    return map[string]any{
        "maxOpenConnections":    instance.maxOpenConnections.Limit().String(),
        "maxIdleConnections":    instance.maxIdleConnections.Limit().String(),
        "connectionMaxLifetime": instance.connectionMaxLifetime.Limit().String(),
        "connectionMaxIdleTime": instance.connectionMaxIdleTime.Limit().String(),
    }
}

/* driverDuration answers a resolved duration as database/sql and the drivers read it, where zero is no bound. */
func driverDuration(resolved bound.Resolved[time.Duration]) time.Duration {
    if true == resolved.Unlimited {
        return 0
    }

    return resolved.Value
}
