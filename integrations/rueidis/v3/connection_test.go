package rueidis

import (
    "context"
    "os"
    "testing"

    "github.com/redis/rueidis"
)

func TestNewConnection_ServesTheWrappedClient(t *testing.T) {
    connection := NewConnection(nil)

    if nil != connection.Client() {
        t.Fatal("expected the nil client to be served as handed")
    }

    if closeErr := connection.Close(); nil != closeErr {
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

    if closeErr := connection.Close(); nil != closeErr {
        t.Fatalf("first close: %v", closeErr)
    }

    if closeErr := connection.Close(); nil != closeErr {
        t.Fatalf("second close must stay nil, got %v", closeErr)
    }

    pingErr := client.Do(context.Background(), client.B().Ping().Build()).Error()
    if nil == pingErr {
        t.Fatal("expected the wrapped client to be closed")
    }
}
