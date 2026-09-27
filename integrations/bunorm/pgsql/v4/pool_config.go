package pgsql

import "time"

func DefaultPoolConfig() *PoolConfig {
    return &PoolConfig{
        MaxOpenConnections:    50,
        MaxIdleConnections:    25,
        ConnectionMaxLifetime: 5 * time.Minute,
        ConnectionMaxIdleTime: 1 * time.Minute,
    }
}

func NewPoolConfig(
    maxOpenConnections int,
    maxIdleConnections int,
    connectionMaxLifetime time.Duration,
    connectionMaxIdleTime time.Duration,
) *PoolConfig {
    return &PoolConfig{
        MaxOpenConnections:    maxOpenConnections,
        MaxIdleConnections:    maxIdleConnections,
        ConnectionMaxLifetime: connectionMaxLifetime,
        ConnectionMaxIdleTime: connectionMaxIdleTime,
    }
}

type PoolConfig struct {
    MaxOpenConnections    int
    MaxIdleConnections    int
    ConnectionMaxLifetime time.Duration
    ConnectionMaxIdleTime time.Duration
}
