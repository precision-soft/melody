package main

import (
    "bytes"
    "context"
    "time"

    melodyrueidis "github.com/precision-soft/melody/integrations/rueidis/v3"
    rueidiscache "github.com/precision-soft/melody/integrations/rueidis/v3/cache"
)

/* runCacheCheck exercises the rueidis cache backend against a live redis: a set/get round-trip, a
short time-to-live that actually expires the key, a miss on an absent key, and the atomic increment
counter — the behaviours a unit test with a fake backend cannot prove. */
func runCacheCheck(address string) {
    ctx := context.Background()

    provider := melodyrueidis.NewProvider()

    client, openErr := provider.Open(melodyrueidis.NewConnectionParameters(address, "", ""))
    if nil != openErr {
        fail("cache: open redis: %v", openErr)
    }
    defer client.Close()

    backend, backendErr := rueidiscache.NewBackend(client, ctx, "melody-e2e:cache:", 0, 0)
    if nil != backendErr {
        fail("cache: new backend: %v", backendErr)
    }

    key := "greeting"
    payload := []byte("hello from the cache e2e")

    /* set/get round-trip */
    if setErr := backend.SetCtx(ctx, key, payload, time.Minute); nil != setErr {
        fail("cache: set: %v", setErr)
    }

    stored, found, getErr := backend.GetCtx(ctx, key)
    if nil != getErr {
        fail("cache: get: %v", getErr)
    }
    if false == found {
        fail("cache: expected a hit for %q, got a miss", key)
    }
    if false == bytes.Equal(payload, stored) {
        fail("cache: hit payload mismatch — wanted %q, got %q", payload, stored)
    }
    pass("cache set/get round-trip on live redis (%d bytes)", len(stored))

    /* miss on an absent key */
    _, foundMissing, missErr := backend.GetCtx(ctx, "never-written")
    if nil != missErr {
        fail("cache: miss lookup: %v", missErr)
    }
    if true == foundMissing {
        fail("cache: expected a miss for an unwritten key, got a hit")
    }
    pass("cache miss reported for an absent key")

    /* a short time-to-live actually expires the key */
    expiringKey := "expiring"
    if setErr := backend.SetCtx(ctx, expiringKey, []byte("transient"), 250*time.Millisecond); nil != setErr {
        fail("cache: set expiring: %v", setErr)
    }

    time.Sleep(700 * time.Millisecond)

    _, stillThere, expiredErr := backend.GetCtx(ctx, expiringKey)
    if nil != expiredErr {
        fail("cache: expiry lookup: %v", expiredErr)
    }
    if true == stillThere {
        fail("cache: key %q survived its time-to-live", expiringKey)
    }
    pass("cache honoured the time-to-live (key expired)")

    /* atomic increment counter */
    counterKey := "counter"
    if deleteErr := backend.DeleteCtx(ctx, counterKey); nil != deleteErr {
        fail("cache: reset counter: %v", deleteErr)
    }

    first, firstErr := backend.IncrementCtx(ctx, counterKey, 3)
    if nil != firstErr {
        fail("cache: increment: %v", firstErr)
    }
    second, secondErr := backend.IncrementCtx(ctx, counterKey, 4)
    if nil != secondErr {
        fail("cache: increment: %v", secondErr)
    }
    if 3 != first || 7 != second {
        fail("cache: increment counter wrong — wanted 3 then 7, got %d then %d", first, second)
    }
    pass("cache atomic increment counter (3 → 7)")
}
