package cache

import (
    "context"
    "os"
    "testing"

    "github.com/redis/rueidis"
)

/* liveBackend answers a backend over the redis the validation lane starts, under a prefix unique to the calling test so parallel suites never read one another's keys. The suite skips where no address is exported, the way the rate limiter suite of the parent package does. */
func liveBackend(t *testing.T) (*Backend, rueidis.Client) {
    t.Helper()

    address := os.Getenv("REDIS_ADDRESS")
    if "" == address {
        t.Skip("REDIS_ADDRESS not set; skipping the live redis suite")
    }

    client, clientErr := rueidis.NewClient(rueidis.ClientOption{
        InitAddress:  []string{address},
        DisableCache: true,
    })
    if nil != clientErr {
        t.Fatalf("could not reach redis at %s: %v", address, clientErr)
    }

    t.Cleanup(client.Close)

    prefix := "melody:test:" + t.Name() + ":"

    backend, backendErr := NewBackend(client, context.Background(), prefix, 0, 0)
    if nil != backendErr {
        t.Fatalf("could not build the backend: %v", backendErr)
    }

    t.Cleanup(func() {
        /* the cleanup clears through its own backend: a test that closed the one under test would otherwise leave its keys behind, since a closed backend refuses Clear like every other operation */
        cleaner, cleanerErr := NewBackend(client, context.Background(), prefix, 0, 0)
        if nil != cleanerErr {
            t.Logf("could not build the cleanup backend: %v", cleanerErr)

            return
        }

        if clearErr := cleaner.Clear(); nil != clearErr {
            t.Logf("could not clear the test prefix: %v", clearErr)
        }
    })

    return backend, client
}

/* rawTtl reads the expiry redis actually holds, which is the only way to tell a key written without expiry from one written with a long one. */
func rawTtl(t *testing.T, client rueidis.Client, fullKey string) int64 {
    t.Helper()

    response := client.Do(context.Background(), client.B().Pttl().Key(fullKey).Build())
    remaining, remainingErr := response.AsInt64()
    if nil != remainingErr {
        t.Fatalf("could not read the expiry of %s: %v", fullKey, remainingErr)
    }

    return remaining
}
