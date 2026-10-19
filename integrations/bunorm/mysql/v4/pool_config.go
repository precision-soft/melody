package mysql

import "time"

func DefaultPoolConfig() *PoolConfig {
    return &PoolConfig{
        MaxOpenConnections:    25,
        MaxIdleConnections:    5,
        ConnectionMaxLifetime: 5 * time.Minute,
        ConnectionMaxIdleTime: 1 * time.Minute,
    }
}

func NewPoolConfig(
    MaxOpenConnections int,
    MaxIdleConnections int,
    ConnectionMaxLifetime time.Duration,
    ConnectionMaxIdleTime time.Duration,
) *PoolConfig {
    return &PoolConfig{
        MaxOpenConnections:    MaxOpenConnections,
        MaxIdleConnections:    MaxIdleConnections,
        ConnectionMaxLifetime: ConnectionMaxLifetime,
        ConnectionMaxIdleTime: ConnectionMaxIdleTime,
    }
}

type PoolConfig struct {
    MaxOpenConnections    int
    MaxIdleConnections    int
    ConnectionMaxLifetime time.Duration
    ConnectionMaxIdleTime time.Duration
}
