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

func TestDefaultTransportConfigPerHostIdlePoolFollowsMaxIdleConns(t *testing.T) {
    config := DefaultTransportConfig()

    if config.MaxIdleConns != config.MaxIdleConnsPerHost {
        t.Fatalf(
            "expected the per-host idle pool to default to MaxIdleConns %d, got %d",
            config.MaxIdleConns,
            config.MaxIdleConnsPerHost,
        )
    }
}

func TestResolveTransportConfigPerHostIdlePoolFollowsAnOverriddenTotal(t *testing.T) {
    resolved := resolveTransportConfig(&TransportConfig{MaxIdleConns: 7})

    if 7 != resolved.MaxIdleConnsPerHost {
        t.Fatalf("expected an overridden MaxIdleConns to carry the per-host idle pool with it, got %d", resolved.MaxIdleConnsPerHost)
    }

    pinned := resolveTransportConfig(&TransportConfig{MaxIdleConns: 7, MaxIdleConnsPerHost: 3})

    if 3 != pinned.MaxIdleConnsPerHost {
        t.Fatalf("expected an explicit MaxIdleConnsPerHost 3 to win, got %d", pinned.MaxIdleConnsPerHost)
    }

    inherited := resolveTransportConfig(&TransportConfig{MaxIdleConnsPerHost: 5})

    if 5 != inherited.MaxIdleConnsPerHost || 100 != inherited.MaxIdleConns {
        t.Fatalf("expected a per-host override alone to leave the total at 100, got %+v", inherited)
    }
}

func TestHttpClientConfigWithTransportRoundTrips(t *testing.T) {
    transport := &TransportConfig{DialTimeout: 3 * time.Second}
    config := NewHttpClientConfig("", 0, nil).WithTransport(transport)

    if transport != config.Transport() {
        t.Fatalf("expected WithTransport to store the transport config")
    }
}

func TestResolveTransportConfig_TheRemainingFourOverridesAreApplied(t *testing.T) {
    resolved := resolveTransportConfig(&TransportConfig{
        IdleConnTimeout:       11 * time.Second,
        TlsHandshakeTimeout:   12 * time.Second,
        ExpectContinueTimeout: 13 * time.Second,
        ResponseHeaderTimeout: 14 * time.Second,
    })

    if 11*time.Second != resolved.IdleConnTimeout {
        t.Fatalf("unexpected idle connection timeout: %v", resolved.IdleConnTimeout)
    }

    if 12*time.Second != resolved.TlsHandshakeTimeout {
        t.Fatalf("unexpected tls handshake timeout: %v", resolved.TlsHandshakeTimeout)
    }

    if 13*time.Second != resolved.ExpectContinueTimeout {
        t.Fatalf("unexpected expect continue timeout: %v", resolved.ExpectContinueTimeout)
    }

    if 14*time.Second != resolved.ResponseHeaderTimeout {
        t.Fatalf("unexpected response header timeout: %v", resolved.ResponseHeaderTimeout)
    }

    defaults := DefaultTransportConfig()

    if defaults.DialTimeout != resolved.DialTimeout || defaults.KeepAlive != resolved.KeepAlive {
        t.Fatalf("expected the fields the caller did not name to keep the defaults, got %v and %v", resolved.DialTimeout, resolved.KeepAlive)
    }
}
