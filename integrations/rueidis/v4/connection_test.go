package rueidis

import (
    "context"
    "errors"
    "os"
    "strings"
    "testing"
    "time"

    "github.com/redis/rueidis"
)

func TestNewConnection_ServesTheWrappedClient(t *testing.T) {
    connection := NewConnection(nil)

    if nil != connection.Client() {
        t.Fatal("expected the nil client to be served as handed")
    }

    if closeErr := connection.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("expected the nil client close to answer nil, got %v", closeErr)
    }
}

func TestConnection_ClosesTheClientOnceAndStaysIdempotent(t *testing.T) {
    address := os.Getenv("REDIS_ADDRESS")
    if "" == address {
        t.Skip("REDIS_ADDRESS is not set")
    }

    client, clientErr := rueidis.NewClient(rueidis.ClientOption{
        InitAddress:  []string{address},
        DisableCache: true,
    })
    if nil != clientErr {
        t.Fatalf("client: %v", clientErr)
    }

    connection := NewConnection(client)

    if client != connection.Client() {
        t.Fatal("expected the wrapped client to be served")
    }

    if closeErr := connection.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("first close: %v", closeErr)
    }

    if closeErr := connection.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("second close must stay nil, got %v", closeErr)
    }

    pingErr := client.Do(context.Background(), client.B().Ping().Build()).Error()
    if nil == pingErr {
        t.Fatal("expected the wrapped client to be closed")
    }
}

type blockingCloseClient struct {
    rueidis.Client
    closeEntered chan struct{}
    releaseClose chan struct{}
}

func (instance *blockingCloseClient) Close() {
    close(instance.closeEntered)
    <-instance.releaseClose
}

func TestConnection_AClientCloseOutlivingTheDeadlineAnswersTheDeadlineAndFinishesInTheBackground(t *testing.T) {
    client := &blockingCloseClient{closeEntered: make(chan struct{}), releaseClose: make(chan struct{})}
    connection := NewConnection(client)

    closeContext, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
    defer cancel()

    closeErr := connection.CloseWithContext(closeContext)
    if false == errors.Is(closeErr, context.DeadlineExceeded) {
        t.Fatalf("expected the deadline's error over a close still running, got %v", closeErr)
    }

    <-client.closeEntered
    close(client.releaseClose)

    if laterErr := connection.CloseWithContext(t.Context()); nil != laterErr {
        t.Fatalf("expected the finished close to answer nil, got %v", laterErr)
    }
}

type panickingCloseClient struct {
    rueidis.Client
}

func (instance *panickingCloseClient) Close() {
    panic(errors.New("the driver tore its own state down"))
}

func TestConnection_APanickingClientCloseIsAnsweredAsTheClosesErrorEveryTime(t *testing.T) {
    connection := NewConnection(&panickingCloseClient{})

    for attempt := 0; attempt < 2; attempt++ {
        closeErr := connection.CloseWithContext(t.Context())
        if nil == closeErr || false == strings.Contains(closeErr.Error(), "panicked") {
            t.Fatalf("attempt %d: expected the panic answered as the close's error, got %v", attempt, closeErr)
        }
    }
}
