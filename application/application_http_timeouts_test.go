package application

import (
    nethttp "net/http"
    "testing"
)

func TestApplyHttpServerTimeoutsDefaults(t *testing.T) {
    server := &nethttp.Server{}

    applyHttpServerTimeouts(server)

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
