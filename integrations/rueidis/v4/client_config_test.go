package rueidis

import (
    "crypto/tls"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
)

func TestResolveClient_TheZeroValueIsTheDefault(t *testing.T) {
    resolved, resolveErr := resolveClient(ClientConfig{})
    if nil != resolveErr {
        t.Fatalf("expected the zero client config to resolve, got %v", resolveErr)
    }

    if true == resolved.bootPingDisabled {
        t.Fatal("expected the zero client config to ping on start")
    }

    if true == resolved.clientSideCacheEnabled {
        t.Fatal("expected the zero client config to leave the client-side cache off")
    }

    if 5*time.Second != resolved.dialTimeout || 5*time.Second != resolved.connectionWriteTimeout {
        t.Fatalf("expected the default 5s dial and write deadlines, got %+v", resolved)
    }

    if 128*(1<<20) != resolved.clientSideCacheBytes {
        t.Fatalf("expected the default 128 MiB cache budget, got %d", resolved.clientSideCacheBytes)
    }
}

func TestResolveClient_KeepsTheConfiguredValues(t *testing.T) {
    tlsConfig := &tls.Config{}

    resolved, resolveErr := resolveClient(ClientConfig{
        ClientName:             "melody",
        SelectDatabase:         3,
        TlsConfig:              tlsConfig,
        DialTimeout:            bound.Of(time.Millisecond),
        ConnectionWriteTimeout: bound.Of(2 * time.Second),
        BootPingDisabled:       true,
        ClientSideCacheEnabled: true,
        ClientSideCacheBytes:   bound.Of(1 << 20),
    })
    if nil != resolveErr {
        t.Fatalf("expected the client config to resolve, got %v", resolveErr)
    }

    if "melody" != resolved.clientName || 3 != resolved.selectDatabase || tlsConfig != resolved.tlsConfig {
        t.Fatalf("expected the plain fields kept, got %+v", resolved)
    }

    if time.Millisecond != resolved.dialTimeout || 2*time.Second != resolved.connectionWriteTimeout {
        t.Fatalf("expected 1ms / 2s, got %+v", resolved)
    }

    if false == resolved.bootPingDisabled || false == resolved.clientSideCacheEnabled || 1<<20 != resolved.clientSideCacheBytes {
        t.Fatalf("expected the ping off and a 1 MiB cache, got %+v", resolved)
    }
}

func TestResolveClient_RefusesAFieldOutsideItsRuleNamingIt(t *testing.T) {
    for _, testCase := range []struct {
        field  string
        config ClientConfig
    }{
        {field: "ClientConfig.DialTimeout", config: ClientConfig{DialTimeout: bound.UnlimitedDuration()}},
        {field: "ClientConfig.DialTimeout", config: ClientConfig{DialTimeout: bound.Of(time.Microsecond)}},
        {field: "ClientConfig.ConnectionWriteTimeout", config: ClientConfig{ConnectionWriteTimeout: bound.UnlimitedDuration()}},
        {field: "ClientConfig.ConnectionWriteTimeout", config: ClientConfig{ConnectionWriteTimeout: bound.Of(-time.Second)}},
        {field: "ClientConfig.ClientSideCacheBytes", config: ClientConfig{ClientSideCacheEnabled: true, ClientSideCacheBytes: bound.Of(0)}},
        {field: "ClientConfig.ClientSideCacheBytes", config: ClientConfig{ClientSideCacheEnabled: true, ClientSideCacheBytes: bound.UnlimitedCount()}},
    } {
        _, resolveErr := resolveClient(testCase.config)
        expectErrorNamingField(t, resolveErr, testCase.field)
    }
}

func TestResolveClient_RefusesACacheBudgetWhileTheCacheIsNotEnabled(t *testing.T) {
    _, resolveErr := resolveClient(ClientConfig{ClientSideCacheBytes: bound.Of(1 << 20)})
    expectErrorNamingField(t, resolveErr, "ClientConfig.ClientSideCacheBytes")

    if "ClientConfig.ClientSideCacheEnabled" != exceptionContextOf(resolveErr)["enabledBy"] {
        t.Fatalf("expected the refusal to name the enabling field, got %v", exceptionContextOf(resolveErr))
    }
}
