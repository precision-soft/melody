package outbox

import (
    "context"
    "io"
    "errors"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"

    outboxintegration "github.com/precision-soft/melody/integrations/outbox/v3"
    "github.com/precision-soft/melody/v3/.example/message"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyexception "github.com/precision-soft/melody/v3/exception"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
)

func relayStatusOver(t *testing.T, relayErr error) (int, string) {
    t.Helper()

    containerInstance := melodycontainer.NewContainer()
    t.Cleanup(func() { _ = containerInstance.Close() })
    melodycontainer.MustRegister(containerInstance, outboxintegration.ServiceRelay, func(resolver melodycontainercontract.Resolver) (*outboxintegration.Relay, error) {
        return nil, relayErr
    })
    runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)

    request := melodyhttp.NewRequest(httptest.NewRequest(nethttp.MethodPost, "/outbox/relay", nil), nil, runtimeInstance, melodyhttp.NewRequestContext("relay-test", time.Now()))
    relay := melodycontainer.Lazy[*outboxintegration.Relay](containerInstance, outboxintegration.ServiceRelay)

    response, handlerErr := RelayHandler(relay)(runtimeInstance, httptest.NewRecorder(), request)
    if nil != handlerErr {
        t.Fatalf("expected a response, got %v", handlerErr)
    }

    return response.StatusCode(), bodyOf(t, response.BodyReader())
}

func TestRelayHandler_AnswersWithoutATransport503NamingTheKey(t *testing.T) {
    status, body := relayStatusOver(t, melodyexception.NewError("the outbox has no transport to publish to", nil, message.ErrTransportNotConfigured))
    if nethttp.StatusServiceUnavailable != status || false == strings.Contains(body, "AMQP_DSN") {
        t.Fatalf("expected 503 naming AMQP_DSN, got %d %s", status, body)
    }

    if status, _ = relayStatusOver(t, errors.New("the outbox store refused")); nethttp.StatusInternalServerError != status {
        t.Fatalf("expected any other resolution failure answered 500, got %d", status)
    }
}

func bodyOf(t *testing.T, reader io.Reader) string {
    t.Helper()

    if nil == reader {
        return ""
    }

    body, readErr := io.ReadAll(reader)
    if nil != readErr {
        t.Fatalf("read body: %v", readErr)
    }

    return string(body)
}
