package httpclient

import (
    "math"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
)

func mustResolveTransportConfig(t *testing.T, config TransportConfig) resolvedTransportConfig {
    t.Helper()

    resolved, err := resolveTransportConfig(config)
    if nil != err {
        t.Fatalf("unexpected refusal: %v", err)
    }

    return resolved
}

func TestResolveTransportConfig_AnEmptyConfigTakesEveryDefault(t *testing.T) {
    expected := resolvedTransportConfig{
        DialTimeout:           10 * time.Second,
        KeepAlive:             30 * time.Second,
        MaxIdleConns:          100,
        MaxIdleConnsPerHost:   100,
        IdleConnTimeout:       90 * time.Second,
        TlsHandshakeTimeout:   10 * time.Second,
        ExpectContinueTimeout: 1 * time.Second,
        ResponseHeaderTimeout: 15 * time.Second,
    }

    if resolved := mustResolveTransportConfig(t, TransportConfig{}); expected != resolved {
        t.Fatalf("expected the documented defaults, got %+v", resolved)
    }
}

func TestResolveTransportConfig_EverySetValueIsApplied(t *testing.T) {
    resolved := mustResolveTransportConfig(t, TransportConfig{
        DialTimeout:           bound.Of(2 * time.Second),
        KeepAlive:             bound.Of(3 * time.Second),
        MaxIdleConns:          bound.Of(7),
        MaxIdleConnsPerHost:   bound.Of(4),
        IdleConnTimeout:       bound.Of(11 * time.Second),
        TlsHandshakeTimeout:   bound.Of(12 * time.Second),
        ExpectContinueTimeout: bound.Of(13 * time.Second),
        ResponseHeaderTimeout: bound.Of(14 * time.Second),
    })

    expected := resolvedTransportConfig{
        DialTimeout:           2 * time.Second,
        KeepAlive:             3 * time.Second,
        MaxIdleConns:          7,
        MaxIdleConnsPerHost:   4,
        IdleConnTimeout:       11 * time.Second,
        TlsHandshakeTimeout:   12 * time.Second,
        ExpectContinueTimeout: 13 * time.Second,
        ResponseHeaderTimeout: 14 * time.Second,
    }

    if expected != resolved {
        t.Fatalf("expected every set value applied, got %+v", resolved)
    }
}

func TestResolveTransportConfig_AnOverrideLeavesTheOtherFieldsAtTheirDefaults(t *testing.T) {
    resolved := mustResolveTransportConfig(t, TransportConfig{DialTimeout: bound.Of(2 * time.Second)})

    if 2*time.Second != resolved.DialTimeout {
        t.Fatalf("expected overridden dial timeout 2s, got %v", resolved.DialTimeout)
    }

    if 30*time.Second != resolved.KeepAlive || 15*time.Second != resolved.ResponseHeaderTimeout {
        t.Fatalf("expected the fields left out to keep their defaults, got %+v", resolved)
    }
}

func TestResolveTransportConfig_AValueBelowTheMinimumIsRefusedNamingTheField(t *testing.T) {
    durationCases := []struct {
        field   string
        minimum time.Duration
        config  func(value bound.Duration) TransportConfig
    }{
        {"TransportConfig.DialTimeout", time.Millisecond, func(value bound.Duration) TransportConfig { return TransportConfig{DialTimeout: value} }},
        {"TransportConfig.KeepAlive", time.Second, func(value bound.Duration) TransportConfig { return TransportConfig{KeepAlive: value} }},
        {"TransportConfig.IdleConnTimeout", time.Second, func(value bound.Duration) TransportConfig { return TransportConfig{IdleConnTimeout: value} }},
        {"TransportConfig.TlsHandshakeTimeout", time.Millisecond, func(value bound.Duration) TransportConfig { return TransportConfig{TlsHandshakeTimeout: value} }},
        {"TransportConfig.ResponseHeaderTimeout", time.Millisecond, func(value bound.Duration) TransportConfig { return TransportConfig{ResponseHeaderTimeout: value} }},
    }

    for _, currentCase := range durationCases {
        for _, value := range []time.Duration{0, currentCase.minimum - 1} {
            _, err := resolveTransportConfig(currentCase.config(bound.Of(value)))
            assertRefusedField(t, err, currentCase.field, "the limit is below its minimum")
        }

        mustResolveTransportConfig(t, currentCase.config(bound.Of(currentCase.minimum)))
    }

    _, err := resolveTransportConfig(TransportConfig{ExpectContinueTimeout: bound.Of(-1 * time.Nanosecond)})
    assertRefusedField(t, err, "TransportConfig.ExpectContinueTimeout", "the limit is below its minimum")

    _, err = resolveTransportConfig(TransportConfig{MaxIdleConns: bound.Of(0)})
    assertRefusedField(t, err, "TransportConfig.MaxIdleConns", "the limit is below its minimum")

    _, err = resolveTransportConfig(TransportConfig{MaxIdleConnsPerHost: bound.Of(0)})
    assertRefusedField(t, err, "TransportConfig.MaxIdleConnsPerHost", "the limit is below its minimum")
}

func TestResolveTransportConfig_AZeroExpectContinueTimeoutIsAValue(t *testing.T) {
    resolved := mustResolveTransportConfig(t, TransportConfig{ExpectContinueTimeout: bound.Of(time.Duration(0))})

    if 0 != resolved.ExpectContinueTimeout {
        t.Fatalf("expected the zero wait kept, got %v", resolved.ExpectContinueTimeout)
    }
}

func TestResolveTransportConfig_UnlimitedIsRefusedWhereTheFieldDoesNotAdmitIt(t *testing.T) {
    unlimited := bound.UnlimitedDuration()

    for field, config := range map[string]TransportConfig{
        "TransportConfig.DialTimeout":           {DialTimeout: unlimited},
        "TransportConfig.KeepAlive":             {KeepAlive: unlimited},
        "TransportConfig.TlsHandshakeTimeout":   {TlsHandshakeTimeout: unlimited},
        "TransportConfig.ExpectContinueTimeout": {ExpectContinueTimeout: unlimited},
        "TransportConfig.ResponseHeaderTimeout": {ResponseHeaderTimeout: unlimited},
    } {
        _, err := resolveTransportConfig(config)
        assertRefusedField(t, err, field, "the limit does not admit unlimited")
    }
}

func TestResolveTransportConfig_UnlimitedMapsToNetHttpTerms(t *testing.T) {
    resolved := mustResolveTransportConfig(t, TransportConfig{
        MaxIdleConns:        bound.UnlimitedCount(),
        MaxIdleConnsPerHost: bound.UnlimitedCount(),
        IdleConnTimeout:     bound.UnlimitedDuration(),
    })

    if 0 != resolved.MaxIdleConns {
        t.Fatalf("expected an unlimited total to reach net/http as zero, got %d", resolved.MaxIdleConns)
    }

    if math.MaxInt != resolved.MaxIdleConnsPerHost {
        t.Fatalf("expected an unlimited host pool to reach net/http as the largest int, got %d", resolved.MaxIdleConnsPerHost)
    }

    if 0 != resolved.IdleConnTimeout {
        t.Fatalf("expected an unlimited idle timeout to reach net/http as zero, got %v", resolved.IdleConnTimeout)
    }
}

func TestResolveTransportConfig_KeepAliveDisabledTurnsTheProbesOff(t *testing.T) {
    resolved := mustResolveTransportConfig(t, TransportConfig{KeepAliveDisabled: true})

    if 0 <= resolved.KeepAlive {
        t.Fatalf("expected a negative interval, net.Dialer's keep-alive off, got %v", resolved.KeepAlive)
    }

    enabled := mustResolveTransportConfig(t, TransportConfig{KeepAlive: bound.Of(5 * time.Second)})

    if 5*time.Second != enabled.KeepAlive {
        t.Fatalf("expected the set interval with keep-alive enabled, got %v", enabled.KeepAlive)
    }
}

func TestResolveTransportConfig_AKeepAliveSetBesideKeepAliveDisabledIsRefused(t *testing.T) {
    _, err := resolveTransportConfig(TransportConfig{KeepAliveDisabled: true, KeepAlive: bound.Of(5 * time.Second)})

    assertRefusedField(t, err, "TransportConfig.KeepAlive", "the keep-alive interval is set on a transport whose keep-alive is disabled")
}

func TestResolveTransportConfig_EachIdlePoolIsDecidedOnItsOwn(t *testing.T) {
    total := mustResolveTransportConfig(t, TransportConfig{MaxIdleConns: bound.Of(500)})

    if 500 != total.MaxIdleConns || 100 != total.MaxIdleConnsPerHost {
        t.Fatalf("expected a set total to leave the host pool at its default of 100, got %+v", total)
    }

    unlimitedTotal := mustResolveTransportConfig(t, TransportConfig{MaxIdleConns: bound.UnlimitedCount()})

    if 0 != unlimitedTotal.MaxIdleConns || 100 != unlimitedTotal.MaxIdleConnsPerHost {
        t.Fatalf("expected an unlimited total and the host pool left at 100, got %+v", unlimitedTotal)
    }

    perHost := mustResolveTransportConfig(t, TransportConfig{MaxIdleConnsPerHost: bound.Of(5)})

    if 5 != perHost.MaxIdleConnsPerHost || 100 != perHost.MaxIdleConns {
        t.Fatalf("expected a set host pool to leave the total at its default of 100, got %+v", perHost)
    }

    aboveTotal := mustResolveTransportConfig(t, TransportConfig{MaxIdleConns: bound.Of(50), MaxIdleConnsPerHost: bound.Of(200)})

    if 50 != aboveTotal.MaxIdleConns || 200 != aboveTotal.MaxIdleConnsPerHost {
        t.Fatalf("expected a host pool above the total kept as set, got %+v", aboveTotal)
    }
}

func TestResolveTransportConfig_IdleConnectionsDisabledKeepsNoIdleConnection(t *testing.T) {
    resolved := mustResolveTransportConfig(t, TransportConfig{IdleConnectionsDisabled: true})

    if false == resolved.DisableKeepAlives {
        t.Fatalf("expected DisableKeepAlives, the switch both the HTTP/1 and the HTTP/2 transport read, got %+v", resolved)
    }

    enabled := mustResolveTransportConfig(t, TransportConfig{})

    if true == enabled.DisableKeepAlives {
        t.Fatalf("expected connection reuse by default, got %+v", enabled)
    }
}

func TestResolveTransportConfig_AnIdlePoolLimitSetBesideIdleConnectionsDisabledIsRefused(t *testing.T) {
    _, err := resolveTransportConfig(TransportConfig{IdleConnectionsDisabled: true, MaxIdleConns: bound.Of(5)})
    assertRefusedField(t, err, "TransportConfig.MaxIdleConns", "an idle pool limit is set on a transport that keeps no idle connection")

    _, err = resolveTransportConfig(TransportConfig{IdleConnectionsDisabled: true, MaxIdleConnsPerHost: bound.UnlimitedCount()})
    assertRefusedField(t, err, "TransportConfig.MaxIdleConnsPerHost", "an idle pool limit is set on a transport that keeps no idle connection")

    _, err = resolveTransportConfig(TransportConfig{IdleConnectionsDisabled: true, IdleConnTimeout: bound.Of(time.Minute)})
    assertRefusedField(t, err, "TransportConfig.IdleConnTimeout", "an idle pool limit is set on a transport that keeps no idle connection")
}
