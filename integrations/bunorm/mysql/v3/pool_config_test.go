package mysql

import (
    "testing"
    "time"
)

func TestDefaultPoolConfig(t *testing.T) {
    poolConfig := DefaultPoolConfig()

    if 25 != poolConfig.MaxOpenConnections {
        t.Fatalf("expected the default of 25 max open connections, got %d", poolConfig.MaxOpenConnections)
    }
    if 5 != poolConfig.MaxIdleConnections {
        t.Fatalf("expected the default of 5 max idle connections, got %d", poolConfig.MaxIdleConnections)
    }
    if 5*time.Minute != poolConfig.ConnectionMaxLifetime {
        t.Fatalf("expected the default connection max lifetime of 5m, got %s", poolConfig.ConnectionMaxLifetime)
    }
    if 1*time.Minute != poolConfig.ConnectionMaxIdleTime {
        t.Fatalf("expected the default connection max idle time of 1m, got %s", poolConfig.ConnectionMaxIdleTime)
    }
}

func TestNewPoolConfigStoresValues(t *testing.T) {
    poolConfig := NewPoolConfig(10, 2, time.Minute, time.Second)

    if 10 != poolConfig.MaxOpenConnections {
        t.Fatalf("expected 10 max open connections, got %d", poolConfig.MaxOpenConnections)
    }
    if 2 != poolConfig.MaxIdleConnections {
        t.Fatalf("expected 2 max idle connections, got %d", poolConfig.MaxIdleConnections)
    }
    if time.Minute != poolConfig.ConnectionMaxLifetime {
        t.Fatalf("expected connection max lifetime 1m, got %s", poolConfig.ConnectionMaxLifetime)
    }
    if time.Second != poolConfig.ConnectionMaxIdleTime {
        t.Fatalf("expected connection max idle time 1s, got %s", poolConfig.ConnectionMaxIdleTime)
    }
}
