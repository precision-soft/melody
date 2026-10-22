package httpclient

import (
    "testing"
    "time"
)

func TestDefaultTransportConfig(t *testing.T) {
    config := DefaultTransportConfig()

    if 10*time.Second != *config.DialTimeout || 30*time.Second != *config.KeepAlive || 100 != *config.MaxIdleConns {
        t.Fatalf("unexpected default transport config: %+v", resolveTransportConfig(config))
    }

    if 90*time.Second != *config.IdleConnTimeout || 10*time.Second != *config.TlsHandshakeTimeout {
        t.Fatalf("unexpected default transport config: %+v", resolveTransportConfig(config))
    }

    if 1*time.Second != *config.ExpectContinueTimeout || 15*time.Second != *config.ResponseHeaderTimeout {
        t.Fatalf("unexpected default transport config: %+v", resolveTransportConfig(config))
    }
}

func TestTransportDurationAndCount_AnswerAFreshPointerToTheValue(t *testing.T) {
    first := TransportDuration(time.Second)
    second := TransportDuration(time.Second)

    if time.Second != *first || first == second {
        t.Fatalf("expected a fresh pointer to 1s per call, got %p and %p", first, second)
    }

    count := TransportCount(0)

    if nil == count || 0 != *count {
        t.Fatalf("expected a pointer to zero, got %v", count)
    }
}

func TestResolveTransportConfigNilFallsBackToDefault(t *testing.T) {
    resolved := resolveTransportConfig(nil)

    if resolveTransportConfig(DefaultTransportConfig()) != resolved {
        t.Fatalf("expected the default transport config, got %+v", resolved)
    }

    if resolveTransportConfig(&TransportConfig{}) != resolved {
        t.Fatalf("expected a config with no field set to resolve to the defaults, got %+v", resolveTransportConfig(&TransportConfig{}))
    }
}

func TestResolveTransportConfigOverrideWinsPerField(t *testing.T) {
    resolved := resolveTransportConfig(&TransportConfig{DialTimeout: TransportDuration(2 * time.Second), MaxIdleConns: TransportCount(7)})

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

    if *config.MaxIdleConns != *config.MaxIdleConnsPerHost {
        t.Fatalf(
            "expected the per-host idle pool to default to MaxIdleConns %d, got %d",
            *config.MaxIdleConns,
            *config.MaxIdleConnsPerHost,
        )
    }
}

func TestResolveTransportConfigPerHostIdlePoolFollowsAnOverriddenTotal(t *testing.T) {
    resolved := resolveTransportConfig(&TransportConfig{MaxIdleConns: TransportCount(7)})

    if 7 != resolved.MaxIdleConnsPerHost {
        t.Fatalf("expected an overridden MaxIdleConns to carry the per-host idle pool with it, got %d", resolved.MaxIdleConnsPerHost)
    }

    pinned := resolveTransportConfig(&TransportConfig{MaxIdleConns: TransportCount(7), MaxIdleConnsPerHost: TransportCount(3)})

    if 3 != pinned.MaxIdleConnsPerHost {
        t.Fatalf("expected an explicit MaxIdleConnsPerHost 3 to win, got %d", pinned.MaxIdleConnsPerHost)
    }

    inherited := resolveTransportConfig(&TransportConfig{MaxIdleConnsPerHost: TransportCount(5)})

    if 5 != inherited.MaxIdleConnsPerHost || 100 != inherited.MaxIdleConns {
        t.Fatalf("expected a per-host override alone to leave the total at 100, got %+v", inherited)
    }

    /* an unbounded total leaves the host at its default: following it, the host pool would be zero, which net/http reads as two */
    unbounded := resolveTransportConfig(&TransportConfig{MaxIdleConns: TransportCount(0)})

    if 0 != unbounded.MaxIdleConns || 100 != unbounded.MaxIdleConnsPerHost {
        t.Fatalf("expected a set zero total kept and the host pool left at 100, got %+v", unbounded)
    }

    noIdle := resolveTransportConfig(&TransportConfig{MaxIdleConns: TransportCount(-1)})

    if -1 != noIdle.MaxIdleConns || 100 != noIdle.MaxIdleConnsPerHost {
        t.Fatalf("expected a negative total kept and the host pool left at 100, got %+v", noIdle)
    }

    hostZero := resolveTransportConfig(&TransportConfig{MaxIdleConnsPerHost: TransportCount(0)})

    if 0 != hostZero.MaxIdleConnsPerHost {
        t.Fatalf("expected a host pool set to zero to reach net/http as zero, got %d", hostZero.MaxIdleConnsPerHost)
    }
}

func TestHttpClientConfigWithTransportRoundTrips(t *testing.T) {
    transport := &TransportConfig{DialTimeout: TransportDuration(3 * time.Second)}
    config := NewHttpClientConfig("", 0, nil).WithTransport(transport)

    if transport != config.Transport() {
        t.Fatalf("expected WithTransport to store the transport config")
    }
}

func TestResolveTransportConfig_TheRemainingFourOverridesAreApplied(t *testing.T) {
    resolved := resolveTransportConfig(&TransportConfig{
        IdleConnTimeout:       TransportDuration(11 * time.Second),
        TlsHandshakeTimeout:   TransportDuration(12 * time.Second),
        ExpectContinueTimeout: TransportDuration(13 * time.Second),
        ResponseHeaderTimeout: TransportDuration(14 * time.Second),
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

    if *defaults.DialTimeout != resolved.DialTimeout || *defaults.KeepAlive != resolved.KeepAlive {
        t.Fatalf("expected the fields the caller did not name to keep the defaults, got %v and %v", resolved.DialTimeout, resolved.KeepAlive)
    }
}

/* a set field reaches net/http as it is, zero and negative alike, so net/http's meanings for zero and net.Dialer's negative KeepAlive are reachable */
func TestResolveTransportConfig_ASetZeroOrNegativeFieldIsKept(t *testing.T) {
    for _, value := range []int{0, -1} {
        duration := time.Duration(value)

        resolved := resolveTransportConfig(&TransportConfig{
            DialTimeout:           TransportDuration(duration),
            KeepAlive:             TransportDuration(duration),
            MaxIdleConns:          TransportCount(value),
            MaxIdleConnsPerHost:   TransportCount(value),
            IdleConnTimeout:       TransportDuration(duration),
            TlsHandshakeTimeout:   TransportDuration(duration),
            ExpectContinueTimeout: TransportDuration(duration),
            ResponseHeaderTimeout: TransportDuration(duration),
        })

        expected := resolvedTransportConfig{
            DialTimeout:           duration,
            KeepAlive:             duration,
            MaxIdleConns:          value,
            MaxIdleConnsPerHost:   value,
            IdleConnTimeout:       duration,
            TlsHandshakeTimeout:   duration,
            ExpectContinueTimeout: duration,
            ResponseHeaderTimeout: duration,
        }

        if expected != resolved {
            t.Fatalf("expected every field set to %d to be kept, got %+v", value, resolved)
        }
    }
}
