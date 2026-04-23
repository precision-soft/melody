package pgsql

import (
    "crypto/tls"
    "testing"
)

func TestProviderDefaultsInsecureFalse(t *testing.T) {
    provider := NewProvider()

    if true == provider.insecure {
        t.Fatalf("expected default insecure=false (secure-by-default), got %v", provider.insecure)
    }
    if nil != provider.tlsConfig {
        t.Fatalf("expected default tlsConfig=nil, got %v", provider.tlsConfig)
    }
}

func TestProviderWithInsecureOverrides(t *testing.T) {
    provider := NewProvider(WithInsecure(true))

    if false == provider.insecure {
        t.Fatalf("expected insecure=true after WithInsecure(true), got %v", provider.insecure)
    }
}

func TestProviderWithTlsConfig(t *testing.T) {
    tlsConfig := &tls.Config{
        ServerName: "db.example.com",
    }

    provider := NewProvider(WithTlsConfig(tlsConfig))

    if provider.tlsConfig != tlsConfig {
        t.Fatalf("expected tlsConfig set to the provided value, got %v", provider.tlsConfig)
    }
}
