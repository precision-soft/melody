package httpclient

import (
    "testing"
    "time"
)

func TestDefaultTransportConfig(t *testing.T) {
    config := DefaultTransportConfig()

    if 10*time.Second != config.DialTimeout || 30*time.Second != config.KeepAlive || 100 != config.MaxIdleConns {
        t.Fatalf("unexpected default transport config: %+v", config)
    }

    if 90*time.Second != config.IdleConnTimeout || 10*time.Second != config.TlsHandshakeTimeout {
        t.Fatalf("unexpected default transport config: %+v", config)
    }

    if 1*time.Second != config.ExpectContinueTimeout || 15*time.Second != config.ResponseHeaderTimeout {
        t.Fatalf("unexpected default transport config: %+v", config)
    }
}

func TestResolveTransportConfigNilFallsBackToDefault(t *testing.T) {
    resolved := resolveTransportConfig(nil)
    defaults := DefaultTransportConfig()

    if defaults.DialTimeout != resolved.DialTimeout || defaults.MaxIdleConns != resolved.MaxIdleConns {
        t.Fatalf("expected the default transport config, got %+v", resolved)
    }
}

func TestResolveTransportConfigOverrideWinsPerField(t *testing.T) {
    resolved := resolveTransportConfig(&TransportConfig{DialTimeout: 2 * time.Second, MaxIdleConns: 7})

    if 2*time.Second != resolved.DialTimeout {
        t.Fatalf("expected overridden dial timeout 2s, got %v", resolved.DialTimeout)
    }

    if 7 != resolved.MaxIdleConns {
        t.Fatalf("expected overridden MaxIdleConns 7, got %d", resolved.MaxIdleConns)
    }

    if 30*time.Second != resolved.KeepAlive {
        t.Fatalf("expected inherited keep-alive 30s, got %v", resolved.KeepAlive)
    }

    if 15*time.Second != resolved.ResponseHeaderTimeout {
        t.Fatalf("expected inherited response-header timeout 15s, got %v", resolved.ResponseHeaderTimeout)
    }
}

func TestHttpClientConfigWithTransportRoundTrips(t *testing.T) {
    transport := &TransportConfig{DialTimeout: 3 * time.Second}
    config := NewHttpClientConfig("", 0, nil).WithTransport(transport)

    if transport != config.Transport() {
        t.Fatalf("expected WithTransport to store the transport config")
    }
}
