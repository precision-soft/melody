package httpclient

import (
    "math"
    "net"
    "net/http"
    "net/http/httptest"
    "sync"
    "testing"
    "time"
)

func TestDefaultTransportConfig(t *testing.T) {
    config := DefaultTransportConfig()

    if 10*time.Second != *config.DialTimeout || 30*time.Second != *config.KeepAlive || 100 != *config.MaxIdleConns {
        t.Fatalf("unexpected default transport config: %+v", config)
    }

    if 90*time.Second != *config.IdleConnTimeout || 10*time.Second != *config.TlsHandshakeTimeout {
        t.Fatalf("unexpected default transport config: %+v", config)
    }

    if 1*time.Second != *config.ExpectContinueTimeout || 15*time.Second != *config.ResponseHeaderTimeout {
        t.Fatalf("unexpected default transport config: %+v", config)
    }
}

func TestResolveTransportConfigNilFallsBackToDefault(t *testing.T) {
    resolved := resolveTransportConfig(nil)
    defaults := DefaultTransportConfig()

    if *defaults.DialTimeout != resolved.DialTimeout || *defaults.MaxIdleConns != resolved.MaxIdleConns {
        t.Fatalf("expected the default transport config, got %+v", resolved)
    }
}

func TestResolveTransportConfigOverrideWinsPerField(t *testing.T) {
    resolved := resolveTransportConfig(&TransportConfig{
        DialTimeout:  TransportDuration(2 * time.Second),
        MaxIdleConns: TransportCount(7),
    })

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

/* a set zero and a set negative are statements, not "unset", and resolve verbatim: MaxIdleConns zero is net/http's unbounded pool, a negative KeepAlive disables the probes, IdleConnTimeout zero waits forever */
func TestResolveTransportConfig_ZeroAndNegativeAreResolvedVerbatim(t *testing.T) {
    resolved := resolveTransportConfig(&TransportConfig{
        KeepAlive:       TransportDuration(-1),
        MaxIdleConns:    TransportCount(0),
        IdleConnTimeout: TransportDuration(0),
    })

    if -1 != resolved.KeepAlive {
        t.Fatalf("expected the negative keep-alive to travel verbatim, got %v", resolved.KeepAlive)
    }

    if 0 != resolved.MaxIdleConns {
        t.Fatalf("expected the zero MaxIdleConns to travel verbatim, got %d", resolved.MaxIdleConns)
    }

    if 0 != resolved.IdleConnTimeout {
        t.Fatalf("expected the zero idle connection timeout to travel verbatim, got %v", resolved.IdleConnTimeout)
    }
}

/* a MaxIdleConns SET to zero must also carry the per-host pool with it: the follow rule reads "pinned or not", not "positive or not" — and it carries the total's MEANING, an unbounded pool, spelled as the largest count, because a per-host zero is net/http's default of two, the cap the follow rule exists to avoid. */
func TestResolveTransportConfig_AZeroTotalCarriesThePerHostPoolWithItAsUnbounded(t *testing.T) {
    resolved := resolveTransportConfig(&TransportConfig{MaxIdleConns: TransportCount(0)})

    if 0 != resolved.MaxIdleConns || math.MaxInt != resolved.MaxIdleConnsPerHost {
        t.Fatalf("expected the zero total to carry an unbounded per-host pool, got %+v", resolved)
    }

    pinned := resolveTransportConfig(&TransportConfig{MaxIdleConns: TransportCount(0), MaxIdleConnsPerHost: TransportCount(3)})
    if 3 != pinned.MaxIdleConnsPerHost {
        t.Fatalf("expected an explicit per-host pool to win over the unbounded total, got %d", pinned.MaxIdleConnsPerHost)
    }
}

/* two waves of six concurrent requests against one host, each wave held until all six arrived: under a per-host pool of two the second wave dials anew, under an unbounded pool it reuses all six */
func TestNewHttpClient_AnUnboundedTotalKeepsEveryIdleConnectionOfTheHost(t *testing.T) {
    var mutex sync.Mutex
    newConnections := 0
    var arrived sync.WaitGroup
    var release sync.WaitGroup

    server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        arrived.Done()
        release.Wait()
    }))
    server.Config.ConnState = func(connection net.Conn, state http.ConnState) {
        if http.StateNew == state {
            mutex.Lock()
            newConnections++
            mutex.Unlock()
        }
    }
    server.Start()
    defer server.Close()

    client := NewHttpClient(
        NewHttpClientConfig("", 0, nil).WithTransport(&TransportConfig{MaxIdleConns: TransportCount(0)}),
    )
    defer client.Close()

    wave := func() {
        arrived.Add(6)
        release.Add(1)

        var finished sync.WaitGroup
        for index := 0; index < 6; index++ {
            finished.Add(1)
            go func() {
                defer finished.Done()
                if _, err := client.Get(server.URL); nil != err {
                    t.Errorf("unexpected error: %v", err)
                }
            }()
        }

        arrived.Wait()
        release.Done()
        finished.Wait()
    }

    wave()
    time.Sleep(200 * time.Millisecond)
    wave()

    mutex.Lock()
    defer mutex.Unlock()
    if 6 != newConnections {
        t.Fatalf("expected the second wave to reuse the six idle connections of the first, got %d new connections", newConnections)
    }
}

/* the helpers exist so a TransportConfig literal stays a literal; each hands back a pointer to its own copy of the value. */
func TestTransportDurationAndTransportCount_HandBackPointersToTheValue(t *testing.T) {
    duration := TransportDuration(5 * time.Second)
    if nil == duration || 5*time.Second != *duration {
        t.Fatalf("unexpected transport duration pointer: %v", duration)
    }

    count := TransportCount(7)
    if nil == count || 7 != *count {
        t.Fatalf("unexpected transport count pointer: %v", count)
    }
}
