package application

import (
    nethttp "net/http"
    "testing"

    "github.com/precision-soft/melody/logging"
    loggingcontract "github.com/precision-soft/melody/logging/contract"
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

type errorLogCapturingLogger struct {
    loggingcontract.Logger

    warnings []loggingcontract.Context
    messages []string
}

func (instance *errorLogCapturingLogger) Warning(message string, context loggingcontract.Context) {
    instance.messages = append(instance.messages, message)
    instance.warnings = append(instance.warnings, context)
}

func TestApplyHttpServerErrorLog_WiresTheApplicationLogger(t *testing.T) {
    server := &nethttp.Server{}
    capture := &errorLogCapturingLogger{Logger: logging.NewNopLogger()}

    applyHttpServerErrorLog(server, capture)

    if nil == server.ErrorLog {
        t.Fatalf("expected net/http to report through the application logger")
    }

    server.ErrorLog.Printf("http: TLS handshake error from 10.0.0.1:52000: EOF")

    if 1 != len(capture.messages) {
        t.Fatalf("expected one record, got %v", capture.messages)
    }

    if "http server error" != capture.messages[0] {
        t.Fatalf("expected the groupable message, got %q", capture.messages[0])
    }

    if "http: TLS handshake error from 10.0.0.1:52000: EOF" != capture.warnings[0]["line"] {
        t.Fatalf("expected the line to travel in the context, got %v", capture.warnings[0]["line"])
    }
}
