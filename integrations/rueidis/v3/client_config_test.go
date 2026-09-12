package rueidis

import (
    "testing"
    "time"
)

func TestDefaultClientConfig_Values(t *testing.T) {
    clientConfig := DefaultClientConfig()

    if "" != clientConfig.ClientName {
        t.Fatalf("default client name must be empty, got %q", clientConfig.ClientName)
    }

    if 0 != clientConfig.SelectDb {
        t.Fatalf("default select db must be 0, got %d", clientConfig.SelectDb)
    }

    if true != clientConfig.DisableCache {
        t.Fatalf("default client config must disable the client-side cache")
    }

    if nil != clientConfig.TlsConfig {
        t.Fatalf("default tls config must be nil")
    }

    if true != clientConfig.PingOnStart {
        t.Fatalf("default client config must ping on start")
    }

    if 5*time.Second != clientConfig.DialTimeout {
        t.Fatalf("default dial timeout must be 5s, got %s", clientConfig.DialTimeout)
    }

    if 5*time.Second != clientConfig.ConnWriteTimeout {
        t.Fatalf("default connection write timeout must be 5s, got %s", clientConfig.ConnWriteTimeout)
    }
}
