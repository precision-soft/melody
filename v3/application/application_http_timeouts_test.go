package application

import (
    nethttp "net/http"
    "testing"
    "time"
)

type timeoutOverridingConfig struct {
    readTimeout       time.Duration
    readHeaderTimeout time.Duration
    writeTimeout      time.Duration
    idleTimeout       time.Duration
    maxHeaderBytes    int
}

func (instance *timeoutOverridingConfig) GetReadTimeout() time.Duration {
    return instance.readTimeout
}

func (instance *timeoutOverridingConfig) GetReadHeaderTimeout() time.Duration {
    return instance.readHeaderTimeout
}

func (instance *timeoutOverridingConfig) GetWriteTimeout() time.Duration {
    return instance.writeTimeout
}

func (instance *timeoutOverridingConfig) GetIdleTimeout() time.Duration {
    return instance.idleTimeout
}

func (instance *timeoutOverridingConfig) GetMaxHeaderBytes() int {
    return instance.maxHeaderBytes
}

type timeoutNonImplementingConfig struct{}

func TestApplyHttpServerTimeoutsDefaults(t *testing.T) {
    server := &nethttp.Server{}

    applyHttpServerTimeouts(server, &timeoutNonImplementingConfig{})

    if defaultHttpReadTimeout != server.ReadTimeout {
        t.Fatalf("expected default ReadTimeout %v, got %v", defaultHttpReadTimeout, server.ReadTimeout)
    }
    if defaultHttpReadHeaderTimeout != server.ReadHeaderTimeout {
        t.Fatalf("expected default ReadHeaderTimeout %v, got %v", defaultHttpReadHeaderTimeout, server.ReadHeaderTimeout)
    }
    if defaultHttpWriteTimeout != server.WriteTimeout {
        t.Fatalf("expected default WriteTimeout %v, got %v", defaultHttpWriteTimeout, server.WriteTimeout)
    }
    if defaultHttpIdleTimeout != server.IdleTimeout {
        t.Fatalf("expected default IdleTimeout %v, got %v", defaultHttpIdleTimeout, server.IdleTimeout)
    }
    if defaultHttpMaxHeaderBytes != server.MaxHeaderBytes {
        t.Fatalf("expected default MaxHeaderBytes %v, got %v", defaultHttpMaxHeaderBytes, server.MaxHeaderBytes)
    }
}

func TestApplyHttpServerTimeoutsOverrides(t *testing.T) {
    overrides := &timeoutOverridingConfig{
        readTimeout:       1 * time.Second,
        readHeaderTimeout: 2 * time.Second,
        writeTimeout:      3 * time.Second,
        idleTimeout:       4 * time.Second,
        maxHeaderBytes:    1234,
    }

    server := &nethttp.Server{}

    applyHttpServerTimeouts(server, overrides)

    if server.ReadTimeout != overrides.readTimeout {
        t.Fatalf("expected override ReadTimeout %v, got %v", overrides.readTimeout, server.ReadTimeout)
    }
    if server.ReadHeaderTimeout != overrides.readHeaderTimeout {
        t.Fatalf("expected override ReadHeaderTimeout %v, got %v", overrides.readHeaderTimeout, server.ReadHeaderTimeout)
    }
    if server.WriteTimeout != overrides.writeTimeout {
        t.Fatalf("expected override WriteTimeout %v, got %v", overrides.writeTimeout, server.WriteTimeout)
    }
    if server.IdleTimeout != overrides.idleTimeout {
        t.Fatalf("expected override IdleTimeout %v, got %v", overrides.idleTimeout, server.IdleTimeout)
    }
    if server.MaxHeaderBytes != overrides.maxHeaderBytes {
        t.Fatalf("expected override MaxHeaderBytes %v, got %v", overrides.maxHeaderBytes, server.MaxHeaderBytes)
    }
}
