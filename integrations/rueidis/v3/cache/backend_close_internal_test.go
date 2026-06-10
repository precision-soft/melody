package cache

import (
    "testing"

    "github.com/redis/rueidis"
)

type closeTrackingClient struct {
    rueidis.Client
    closed bool
}

func (instance *closeTrackingClient) Close() {
    instance.closed = true
}

func TestBackendCloseDoesNotCloseCallerOwnedClient(t *testing.T) {
    client := &closeTrackingClient{}

    backend, backendErr := NewBackend(client, nil, "", 0, 0)
    if nil != backendErr {
        t.Fatalf("NewBackend returned an error: %v", backendErr)
    }

    if closeErr := backend.Close(); nil != closeErr {
        t.Fatalf("Backend.Close returned an error: %v", closeErr)
    }

    if true == client.closed {
        t.Fatalf("Backend.Close closed the caller-owned rueidis client; the client lifecycle is owned by the application and is shared with the locker, token store, and server-sent-event backplane")
    }
}
